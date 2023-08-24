// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package noderelay

import (
	"context"
	"crypto/elliptic"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/ws"
	"github.com/decred/dcrd/certgen"
)

const (
	connectTimeoutSeconds = 10
	// Time allowed to read the next pong message from the peer. The
	// default is intended for production, but leaving as a var instead of const
	// to facilitate testing.
	PongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait. The
	// default is intended for production, but leaving as a var instead of const
	// to facilitate testing.
	PingPeriod       = 30 * time.Second
	defaultNexusPort = "17537"
)

type NexusConfig struct {
	ExternalAddr string
	Host         string
	Port         string
	Dir          string
	Key          string
	Cert         string
	CertHosts    []string
	Logger       dex.Logger
	Relays       []string
}

// Nexus is run on the server. A Node will connect to the Nexus, making
// their services available for a client.
type Nexus struct {
	ctx               context.Context
	cfg               *NexusConfig
	tlsConfig         *tls.Config
	relayAddrs        map[string]string
	log               dex.Logger
	wg                sync.WaitGroup
	certB             []byte
	relayfileDir      string
	allNodesConnected chan struct{}

	nodeMtx sync.RWMutex
	nodes   map[string]*sourceNode
}

func NewNexus(cfg *NexusConfig) (*Nexus, error) {
	if len(cfg.Relays) == 0 {
		return nil, errors.New("no relays specified")
	}
	re := regexp.MustCompile(`\s`)
	for _, relayID := range cfg.Relays {
		if re.MatchString(relayID) {
			return nil, fmt.Errorf("relay ID %q contains whitespace", relayID)
		}
	}
	if cfg.Port == "" {
		cfg.Port = defaultNexusPort
	}
	relayfileDir := filepath.Join(cfg.Dir, "relay-files")
	if err := os.MkdirAll(relayfileDir, 0700); err != nil {
		return nil, fmt.Errorf("error creating relay file directory: %w", err)
	}
	// Find or create the key pair.
	keyExists := dex.FileExists(cfg.Key)
	certExists := dex.FileExists(cfg.Cert)
	if certExists == !keyExists {
		return nil, fmt.Errorf("missing cert pair file")
	}
	if !keyExists && !certExists {
		err := genCertPair(cfg.Cert, cfg.Key, cfg.CertHosts, cfg.Logger)
		if err != nil {
			return nil, err
		}
	}
	keypair, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, err
	}

	certB, err := os.ReadFile(cfg.Cert)
	if err != nil {
		return nil, fmt.Errorf("error loading certificate file contents: %v", err)
	}

	// Prepare the TLS configuration.
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{keypair},
		MinVersion:   tls.VersionTLS12,
	}

	return &Nexus{
		cfg:               cfg,
		tlsConfig:         tlsConfig,
		relayAddrs:        make(map[string]string),
		log:               cfg.Logger,
		nodes:             make(map[string]*sourceNode),
		certB:             certB,
		relayfileDir:      relayfileDir,
		allNodesConnected: make(chan struct{}),
	}, nil
}

func (n *Nexus) RelayAddr(relayID string) (string, error) {
	relayAddr, found := n.relayAddrs[relayID]
	if !found {
		return "", fmt.Errorf("no relay node found for ID %q", relayID)
	}
	return relayAddr, nil
}

func (n *Nexus) monitorNodeConnections() {
	nodesConnected := func() (numRegistered, numIDs int) {
		n.nodeMtx.RLock()
		defer n.nodeMtx.RUnlock()
		numIDs = len(n.relayAddrs)
		for relayID := range n.relayAddrs {
			if _, registered := n.nodes[relayID]; registered {
				numRegistered++
			}
		}
		return
	}

	lastLog := time.Time{}
	for {
		if r, m := nodesConnected(); r == m {
			close(n.allNodesConnected)
			return
		} else if time.Since(lastLog) > time.Minute {
			lastLog = time.Now()
			n.log.Infof("Node relay waiting on sources. %d / %d connected", r, m)
		}
		select {
		case <-time.After(time.Second):
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *Nexus) WaitForSourceNodes() <-chan struct{} {
	return n.allNodesConnected
}

type RelayFile struct {
	RelayID string    `json:"relayID"`
	Cert    dex.Bytes `json:"cert"`
	Addr    string    `json:"addr"`
}

// Run is the constructor for an RPCServer.
func (n *Nexus) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	n.ctx = ctx

	log, wg := n.cfg.Logger, &n.wg

	inAddr := "localhost:" + n.cfg.Port

	// Create listener.
	listener, err := tls.Listen("tcp", inAddr, n.tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("can't listen on %s. nexus server quitting: %w", inAddr, err)
	}
	// Update the listening address in case a :0 was provided.
	addr := listener.Addr().String()

	for _, relayID := range n.cfg.Relays {
		relayAddr, err := n.runRelayServer(relayID)
		if err != nil {
			return nil, fmt.Errorf("error running node server for relay ID %s", relayID)
		}
		n.relayAddrs[relayID] = relayAddr

		relayfilePath := filepath.Join(n.relayfileDir, relayID+".relayfile")

		b, err := json.Marshal(&RelayFile{
			RelayID: relayID,
			Cert:    n.certB,
			Addr:    n.cfg.ExternalAddr,
		})
		if err != nil {
			n.log.Errorf("error encoding relay file: %v", err)
		} else if err = os.WriteFile(relayfilePath, b, 0600); err != nil {
			n.log.Errorf("error writing relay file: %v", err)
		}
	}

	handleSourceConnect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.NewConnection(w, r, PongWait)
		if err != nil {
			log.Errorf("ws connection error: %v", err)
			return
		}
		ip := dex.NewIPKey(r.RemoteAddr).String()

		wg.Add(1)
		go func() {
			defer wg.Done()

			cl := ws.NewWSLink(ip, conn, PingPeriod, nil, log)
			defer cl.Disconnect()

			node := &sourceNode{
				cl:           cl,
				respHandlers: make(map[uint64]*responseHandler),
			}

			nodeChan := make(chan string)
			cl.RawHandler = func(b []byte) {
				var resp RelayedMessage
				if err := json.Unmarshal(b, &resp); err != nil {
					n.log.Errorf("error unmarshalling connect message: %v", err)
					return
				}

				if resp.MessageID == 0 {
					select {
					case nodeChan <- string(resp.Body):
					default:
						log.Debugf("blocking node id channel")
					}
					return
				}
				if node.id == "" {
					n.log.Errorf("received numbered request from %s before node ID", ip)
					cl.Disconnect()
					return
				}
				respHandler := node.respHandler(resp.MessageID)
				if respHandler == nil {
					n.log.Errorf("no handler for response from %s %s", ip)
					return
				}
				respHandler.f(resp.Body, resp.Headers)
			}
			cm := dex.NewConnectionMaster(cl)
			err := cm.ConnectOnce(ctx) // we discard the cm anyway, but good practice
			if err != nil {
				log.Errorf("websocketHandler client connect: %v", err)
				return
			}

			select {
			case nodeID := <-nodeChan:
				node.id = nodeID
			case <-time.After(connectTimeoutSeconds * time.Second):
				log.Errorf("connected nexus source failed to ID")
				return
			case <-ctx.Done():
				return
			}

			if _, found := n.relayAddrs[node.id]; !found {
				log.Errorf("no nexus relay registered for ID %s", node.id)
				return
			}

			n.nodeMtx.Lock()
			if oldNode, exists := n.nodes[node.id]; exists {
				oldNode.cl.Disconnect()
			}
			n.nodes[node.id] = node
			n.nodeMtx.Unlock()

			defer func() {
				n.nodeMtx.Lock()
				delete(n.nodes, node.id)
				n.nodeMtx.Unlock()

			}()

			cm.Wait() // also waits for any handleMessage calls in (*WSLink).inHandler
		}()
	})

	srv := &http.Server{
		Handler:      handleSourceConnect,
		ReadTimeout:  connectTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: connectTimeoutSeconds * time.Second, // hung responses must die
	}

	// Close the listener on context cancellation.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners:
			log.Errorf("HTTP server Shutdown: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			log.Warnf("unexpected (http.Server).Serve error: %v", err)
		}
		log.Infof("Server off")
	}()
	log.Infof("Server listening on %s", addr)

	go n.monitorNodeConnections()

	return &n.wg, nil
}

type RelayedMessage struct {
	MessageID uint64              `json:"messageID"`
	Body      dex.Bytes           `json:"body"`
	Headers   map[string][]string `json:"headers"`
}

var messageIDCounter uint64

func (n *Nexus) runRelayServer(nodeID string) (string, error) {
	log, wg := n.cfg.Logger, &n.wg

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("error getting nexus listener for ID %s: %w", nodeID, err)
	}

	relayAddr := l.Addr().String()

	// This is a request coming from a local dcrdex wallet. Send it to any
	// waiting nodeSource and handle the response.
	handleRequest := func(w http.ResponseWriter, r *http.Request) {
		n.nodeMtx.RLock()
		node, found := n.nodes[nodeID]
		n.nodeMtx.RUnlock()
		if !found {
			http.Error(w, fmt.Sprintf("no node %s", nodeID), http.StatusServiceUnavailable)
			return
		}
		b, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			n.log.Errorf("error from source node with ID %s: %v", nodeID, err)
			return
		}

		reqID := atomic.AddUint64(&messageIDCounter, 1)
		reqB, err := json.Marshal(&RelayedMessage{
			MessageID: reqID,
			Body:      b,
			Headers:   r.Header,
		})

		recv := make(chan struct{})

		node.logReq(reqID, func(body []byte, hdrs map[string][]string) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			// msg := append(b, byte('\n'))
			for k, vs := range hdrs {
				for _, v := range vs {
					w.Header().Set(k, v)
				}
			}
			w.WriteHeader(http.StatusOK)
			_, err = w.Write(body)
			if err != nil {
				log.Errorf("Write error: %v", err)
			}
			close(recv)
		})

		node.cl.SendRaw(reqB)
		select {
		case <-recv:
		case <-n.ctx.Done():
		}
	}

	srv := &http.Server{
		Addr:         relayAddr,
		Handler:      http.HandlerFunc(handleRequest),
		ReadTimeout:  connectTimeoutSeconds * time.Second,
		WriteTimeout: connectTimeoutSeconds * time.Second,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Serve(l); !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("listen: %s\n", err)
		}
		log.Infof("Nexus no longer serving ID %s: %v", nodeID, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-n.ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Errorf("http.Server Shutdown errored: %v", err)
		}
	}()

	return relayAddr, nil
}

type responseHandler struct {
	f      func([]byte, map[string][]string)
	expire *time.Timer
}

type sourceNode struct {
	id string
	cl *ws.WSLink

	reqMtx       sync.Mutex
	respHandlers map[uint64]*responseHandler
}

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (n *sourceNode) logReq(reqID uint64, respHandler func([]byte, map[string][]string)) {
	n.reqMtx.Lock()
	defer n.reqMtx.Unlock()
	doExpire := func() {
		// Delete the response handler, and call the provided expire function if
		// (*wsLink).respHandler has not already retrieved the handler function
		// for execution.
		if n.expireRequest(reqID) {
			// expire()
		}
	}
	const expireTime = time.Second * 30
	n.respHandlers[reqID] = &responseHandler{
		f:      respHandler,
		expire: time.AfterFunc(expireTime, doExpire),
	}
}

func (n *sourceNode) expireRequest(reqID uint64) bool {
	n.reqMtx.Lock()
	defer n.reqMtx.Unlock()
	_, removed := n.respHandlers[reqID]
	delete(n.respHandlers, reqID)
	return removed
}

func (n *sourceNode) respHandler(reqID uint64) *responseHandler {
	n.reqMtx.Lock()
	defer n.reqMtx.Unlock()
	cb, ok := n.respHandlers[reqID]
	if ok {
		// Stop the expiration Timer. If the Timer fired after respHandler was
		// called, but we found the response handler in the map, wsLink.expire
		// is waiting for the reqMtx lock and will return false, thus preventing
		// the registered expire func from executing.
		cb.expire.Stop()
		delete(n.respHandlers, reqID)
	}
	return cb
}

func ParseRelayAddr(encAddr string) (relayID string, is bool, err error) {
	if !strings.Contains(encAddr, "noderelay") {
		return "", false, nil
	}
	parts := strings.SplitN(encAddr, ":", 2)
	if len(parts) != 2 {
		return "", false, fmt.Errorf("invalid relay ID: %s", encAddr)
	}
	return parts[1], true, nil
}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string, hosts []string, log dex.Logger) error {
	log.Infof("Generating TLS certificates...")

	org := "dcrdex nexus autogenerated cert"
	validUntil := time.Now().Add(10 * 365 * 24 * time.Hour)
	cert, key, err := certgen.NewTLSCertPair(elliptic.P521(), org,
		validUntil, hosts)
	if err != nil {
		return err
	}

	// Write cert and key files.
	if err = os.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(keyFile, key, 0600); err != nil {
		os.Remove(certFile)
		return err
	}

	log.Infof("Done generating TLS certificates")
	return nil
}
