package comms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

const (
	// bufferSize is buffer size for a websocket connection's read channel.
	readBuffSize = 128

	// The maximum time in seconds to write to a connection.
	writeWait = time.Second * 3
)

// WsConn is an interface for a websocket client.
type WsConn interface {
	NextID() uint64
	Send(msg *msgjson.Message) error
	Request(msg *msgjson.Message, f func(*msgjson.Message)) error
	Connect(ctx context.Context) (error, *sync.WaitGroup)
	MessageSource() <-chan *msgjson.Message
}

// When the DEX sends a request to the client, a responseHandler is created
// to wait for the response.
type responseHandler struct {
	expiration *time.Timer
	f          func(*msgjson.Message)
}

// WsCfg is the configuration struct for initializing a WsConn.
type WsCfg struct {
	// URL is the websocket endpoint URL.
	URL string
	// The maximum time in seconds to wait for a ping from the server.
	PingWait time.Duration
	// The rpc certificate file path.
	RpcCert string
	// ReconnectSync runs the needed reconnection synchronization after
	// a disconnect.
	ReconnectSync func()
}

// wsConn represents a client websocket connection.
type wsConn struct {
	reconnects   uint64
	rID          uint64
	cfg          *WsCfg
	ws           *websocket.Conn
	wsMtx        sync.Mutex
	tlsCfg       *tls.Config
	readCh       chan *msgjson.Message
	sendCh       chan *msgjson.Message
	reconnectCh  chan struct{}
	reqMtx       sync.RWMutex
	connected    bool
	connectedMtx sync.RWMutex
	once         sync.Once
	respHandlers map[uint64]*responseHandler
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

// NewWsConn creates a client websocket connection.
func NewWsConn(cfg *WsCfg) (WsConn, error) {
	if cfg.PingWait < 0 {
		return nil, fmt.Errorf("ping wait cannot be negative")
	}

	var tlsConfig *tls.Config
	if cfg.RpcCert != "" {
		if !fileExists(cfg.RpcCert) {
			return nil, fmt.Errorf("the rpc cert provided (%v) "+
				"does not exist", cfg.RpcCert)
		}

		rootCAs, _ := x509.SystemCertPool()
		if rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}

		certs, err := ioutil.ReadFile(cfg.RpcCert)
		if err != nil {
			return nil, fmt.Errorf("file reading error: %v", err)
		}

		if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
			return nil, fmt.Errorf("unable to append cert")
		}

		tlsConfig = &tls.Config{
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
		}
	}

	return &wsConn{
		cfg:          cfg,
		tlsCfg:       tlsConfig,
		readCh:       make(chan *msgjson.Message, readBuffSize),
		sendCh:       make(chan *msgjson.Message),
		reconnectCh:  make(chan struct{}),
		respHandlers: make(map[uint64]*responseHandler),
	}, nil
}

// isConnected returns the connection connected state.
func (conn *wsConn) isConnected() bool {
	conn.connectedMtx.RLock()
	defer conn.connectedMtx.RUnlock()
	return conn.connected
}

// setConnected updates the connection's connected state.
func (conn *wsConn) setConnected(connected bool) {
	conn.connectedMtx.Lock()
	conn.connected = connected
	conn.connectedMtx.Unlock()
}

// connect attempts to establish a websocket connection.
func (conn *wsConn) connect() error {
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  conn.tlsCfg,
	}

	ws, _, err := dialer.Dial(conn.cfg.URL, nil)
	if err != nil {
		conn.queueReconnect()
		return err
	}

	ws.SetPingHandler(func(string) error {
		conn.wsMtx.Lock()
		defer conn.wsMtx.Unlock()

		now := time.Now()
		err := ws.SetReadDeadline(now.Add(conn.cfg.PingWait))
		if err != nil {
			log.Errorf("read deadline error: %v", err)
			return err
		}

		// Respond with a pong.
		err = ws.WriteControl(websocket.PongMessage, []byte{}, now.Add(writeWait))
		if err != nil {
			log.Errorf("pong error: %v", err)
			return err
		}

		return nil
	})

	conn.wsMtx.Lock()
	conn.ws = ws
	conn.wsMtx.Unlock()

	go conn.read()
	conn.setConnected(true)

	return nil
}

// read fetches and parses incoming messages for processing. This should be
// run as a goroutine.
func (conn *wsConn) read() {
	for {
		msg := new(msgjson.Message)

		conn.wsMtx.Lock()
		ws := conn.ws
		conn.wsMtx.Unlock()

		err := ws.ReadJSON(msg)
		if err != nil {
			var mErr *json.UnmarshalTypeError
			if errors.As(err, &mErr) {
				// JSON decode errors are not fatal, log and proceed.
				log.Errorf("json decode error: %v", mErr)
				continue
			}

			if websocket.IsCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure) ||
				strings.Contains(err.Error(), "websocket: close sent") {
				return
			}

			var opErr *net.OpError
			if errors.As(err, &opErr) {
				if opErr.Op == "read" {
					if strings.Contains(opErr.Err.Error(),
						"use of closed network connection") {
						return
					}
				}
			}

			// Log all other errors and trigger a reconnection.
			log.Errorf("read error: %v", err)
			conn.reconnectCh <- struct{}{}
			return
		}

		// If the message is a response, find the handler.
		if msg.Type == msgjson.Response {
			handler := conn.respHandler(msg.ID)
			if handler == nil {
				b, _ := json.Marshal(msg)
				log.Errorf("no handler found for response", string(b))
				continue
			}
			// Run handlers in a goroutine so that other messages can be recieved.
			go handler.f(msg)
			continue
		}
		conn.readCh <- msg
	}
}

// keepAlive maintains an active websocket connection by reconnecting when
// the established connection is broken. This should be run as a goroutine.
func (conn *wsConn) keepAlive(ctx context.Context) {
	for {
		select {
		case <-conn.reconnectCh:
			conn.setConnected(false)

			reconnects := atomic.AddUint64(&conn.reconnects, 1)
			if reconnects > 1 {
				conn.close()
			}

			err := conn.connect()
			if err != nil {
				log.Errorf("connection error: %v", err)
				conn.queueReconnect()
				continue
			}

			// Synchronize after a reconnection.
			if conn.cfg.ReconnectSync != nil {
				conn.cfg.ReconnectSync()
			}

		case <-ctx.Done():
			// Terminate the keepAlive process and read process when
			/// the dex client signals a shutdown.
			conn.setConnected(false)
			conn.close()
			return
		}
	}
}

// queueReconnect queues a reconnection attempt.
func (conn *wsConn) queueReconnect() {
	conn.setConnected(false)
	time.AfterFunc(conn.cfg.PingWait, func() { conn.reconnectCh <- struct{}{} })
}

// NextID returns the next request id.
func (conn *wsConn) NextID() uint64 {
	return atomic.AddUint64(&conn.rID, 1)
}

// Connect connects the client and starts an auto-reconnect loop. Any error
// encountered during the initial connection will be returned. The reconnect
// loop will continue to try connecting, even if an error is returned. To
// shutdown auto-reconnect, use close().
func (conn *wsConn) Connect(ctx context.Context) (error, *sync.WaitGroup) {
	var wg sync.WaitGroup
	wg.Add(1)

	go conn.keepAlive(ctx)

	go func() {
		<-ctx.Done()
		conn.close()
		wg.Done()
	}()

	return conn.connect(), &wg
}

// Close terminates all websocket processes and closes the connection.
func (conn *wsConn) close() {
	conn.wsMtx.Lock()
	defer conn.wsMtx.Unlock()

	if conn.ws == nil {
		return
	}

	msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	conn.ws.WriteControl(websocket.CloseMessage, msg,
		time.Now().Add(writeWait))
	conn.ws.Close()
}

// Send pushes outgoing messages over the websocket connection.
func (conn *wsConn) Send(msg *msgjson.Message) error {
	if !conn.isConnected() {
		return fmt.Errorf("cannot send on a broken connection")
	}

	conn.wsMtx.Lock()
	conn.ws.SetWriteDeadline(time.Now().Add(writeWait))

	err := conn.ws.WriteJSON(msg)
	conn.wsMtx.Unlock()
	if err != nil {
		log.Errorf("write error: %v", err)
		return err
	}
	return nil
}

// Request sends the message with Send, but keeps a record of the callback
// function to run when a response is received.
func (conn *wsConn) Request(msg *msgjson.Message, f func(*msgjson.Message)) error {
	// Log the message sent if it is a request.
	if msg.Type == msgjson.Request {
		conn.logReq(msg.ID, f)
	}
	return conn.Send(msg)
}

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (conn *wsConn) logReq(id uint64, respHandler func(*msgjson.Message)) {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	conn.respHandlers[id] = &responseHandler{
		expiration: time.AfterFunc(time.Minute*5, func() {
			conn.reqMtx.Lock()
			delete(conn.respHandlers, id)
			conn.reqMtx.Unlock()
		}),
		f: respHandler,
	}
}

// respHandler extracts the response handler for the provided request ID if it
// exists, else nil. If the handler exists, it will be deleted from the map.
func (conn *wsConn) respHandler(id uint64) *responseHandler {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	cb, ok := conn.respHandlers[id]
	if ok {
		cb.expiration.Stop()
		delete(conn.respHandlers, id)
	}
	return cb
}

// MessageSource returns the connection's read source only once. The returned
// chan will receive requests and notifications from the server, but not
// responses, which have handlers associated with their request.
func (conn *wsConn) MessageSource() <-chan *msgjson.Message {
	var ch <-chan *msgjson.Message

	conn.once.Do(func() {
		ch = conn.readCh
	})

	return ch
}
