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
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/ws"
	"github.com/decred/dcrd/certgen"
)

const (
	// PongWait is the time allowed to read the next pong message from the peer.
	PongWait = 60 * time.Second
	// PingPeriod sets the frequency on which websocket clients should ping.
	PingPeriod = 30 * time.Second
	// connectTimeoutSeconds is used to ensure timely connections.
	connectTimeoutSeconds = 10
	// expireTime is the time after sending a request to the source node before
	// we consider the request failed.
	expireTime = time.Second * 30
)

// sourceNode represents a connected source node.
type sourceNode struct {
	relayID string
	addr    string
	cl      *ws.WSLink

	reqMtx       sync.Mutex
	respHandlers map[uint64]*responseHandler
}

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (n *sourceNode) logReq(reqID uint64, respHandler func([]byte, map[string][]string), expire func()) {
	n.reqMtx.Lock()
	defer n.reqMtx.Unlock()
	doExpire := func() {
		// Delete the response handler, and call the provided expire function if
		// *Nexus has not already retrieved the handler function for execution.
		if n.expireRequest(reqID) {
			expire()
		}
	}
	n.respHandlers[reqID] = &responseHandler{
		f:      respHandler,
		expire: time.AfterFunc(expireTime, doExpire),
	}
}

// expireRequest expires the pending request.
func (n *sourceNode) expireRequest(reqID uint64) bool {
	n.reqMtx.Lock()
	defer n.reqMtx.Unlock()
	_, removed := n.respHandlers[reqID]
	delete(n.respHandlers, reqID)
	return removed
}

// respHandler gets the stored responseHandler, if it exists, else nil.
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

// responseHandler is a handler for the response from a sent WebSockets request.
type responseHandler struct {
	f      func([]byte, map[string][]string)
	expire *time.Timer
}

// nodeRelay manages source nodes.
type nodeRelay struct {
	sync.RWMutex
	sources map[string]*sourceNode
}

type NexusConfig struct {
	// ExternalAddr is the external IP:port address or host(:port) of the
	// Nexus relay manager. The operator must configure this independently
	// so that the External address is routed to the specified Port listening
	// on all loopback interfaces.
	ExternalAddr string
	// Port is the port that Nexus will listen on.
	Port string
	// Dir is a directory to output relayfiles and generated TLS key-cert pairs.
	Dir string
	// Key is the path to a TLS key. If Key == "", a new key and certificate
	// will be created in the Dir. The ExternalAddr will be added to the
	// certificate as a host. If the ExternalAddr changes, a new certificate
	// can be generated by deleting the old key-cert pair and restarting.
	// Changing the ExternalAddr renders any previously generated relayfiles
	// void.
	Key string
	// Cert is the path to a TLS certificate. See docs for Key.
	Cert   string
	Logger dex.Logger
	// RelayIDs are the relay IDs for which to start node relays. These can be
	// any string the caller chooses. These relay IDs are given to source node
	// operators (generally as part of a relayfile) and are used to configure
	// their source nodes. The channel returned from WaitForSourceNodes will
	// not close until there is at least one source node connected for every
	// ID in RelayIDs.
	RelayIDs []string
}

// normalize checks sanity and sets defaults for the NexusConfig.
func (cfg *NexusConfig) normalize() error {
	const (
		defaultNexusPort = "17537"
		keyFilename      = "relay.key"
		certFilename     = "relay.cert"
	)
	if len(cfg.RelayIDs) == 0 {
		return errors.New("no relays specified")
	}
	re := regexp.MustCompile(`\s`)
	for _, relayID := range cfg.RelayIDs {
		if re.MatchString(relayID) {
			return fmt.Errorf("relay ID %q contains whitespace", relayID)
		}
	}
	if cfg.Port == "" {
		cfg.Port = defaultNexusPort
	}
	if cfg.Key == "" {
		cfg.Key = filepath.Join(cfg.Dir, keyFilename)
	}
	if cfg.Cert == "" {
		cfg.Cert = filepath.Join(cfg.Dir, certFilename)
	}
	return nil
}

// prepareKeys loads the TLS certificate, creating a key-cert pair if necessary.
func (cfg *NexusConfig) prepareKeys() (*tls.Config, []byte, error) {
	keyExists := dex.FileExists(cfg.Key)
	certExists := dex.FileExists(cfg.Cert)
	if certExists == !keyExists {
		return nil, nil, fmt.Errorf("missing cert pair file")
	}
	if !keyExists {
		// certgen will actually ignore the port, but we'll remove it for good
		// measure.
		var dnsNames []string
		if cfg.ExternalAddr != "" {
			host, _, err := net.SplitHostPort(cfg.ExternalAddr)
			if err != nil {
				return nil, nil, fmt.Errorf("error parsing public address: %v", err)
			}
			dnsNames = []string{host}
		}
		err := genCertPair(cfg.Cert, cfg.Key, dnsNames, cfg.Logger)
		if err != nil {
			return nil, nil, err
		}
	}
	keypair, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, nil, err
	}

	certB, err := os.ReadFile(cfg.Cert)
	if err != nil {
		return nil, nil, fmt.Errorf("error loading certificate file contents: %v", err)
	}

	// Prepare the TLS configuration.
	return &tls.Config{
		Certificates: []tls.Certificate{keypair},
		MinVersion:   tls.VersionTLS12,
	}, certB, nil
}

// Nexus is run on the server and manages a series of node relays. A source node
// will connect to the Nexus, making their services available for a local
// consumer.
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
	relays            map[string]*nodeRelay
}

// NewNexus is the constructor for a Nexus.
func NewNexus(cfg *NexusConfig) (*Nexus, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	relayfileDir := filepath.Join(cfg.Dir, "relay-files")
	if err := os.MkdirAll(relayfileDir, 0700); err != nil {
		return nil, fmt.Errorf("error creating relay file directory: %w", err)
	}
	tlsConfig, certB, err := cfg.prepareKeys()
	if err != nil {
		return nil, err
	}

	relays := make(map[string]*nodeRelay, len(cfg.RelayIDs))
	for _, relayID := range cfg.RelayIDs {
		relays[relayID] = &nodeRelay{
			sources: make(map[string]*sourceNode),
		}
	}

	return &Nexus{
		cfg:               cfg,
		tlsConfig:         tlsConfig,
		relayAddrs:        make(map[string]string),
		log:               cfg.Logger,
		relays:            relays,
		certB:             certB,
		relayfileDir:      relayfileDir,
		allNodesConnected: make(chan struct{}),
	}, nil
}

// RelayAddr returns the local address for relay, or an error if there is no
// server running for the given relay ID.
func (n *Nexus) RelayAddr(relayID string) (string, error) {
	relayAddr, found := n.relayAddrs[relayID]
	if !found {
		return "", fmt.Errorf("no relay node found for ID %q", relayID)
	}
	return relayAddr, nil
}

// monitorNodeConnections checks the status of relays once per second, and
// closes the allNodesConnected channel when every relay has at least one
// source node.
func (n *Nexus) monitorNodeConnections() {
	nodeReport := func() (registered, unregistered []string) {
		for relayID, relay := range n.relays {
			relay.RLock()
			if len(relay.sources) > 0 {
				registered = append(registered, relayID)
			} else {
				unregistered = append(unregistered, relayID)
			}
			relay.RUnlock()
		}
		return
	}

	n.log.Infof("Node relay waiting on %d source nodes to connect", len(n.relays))
	lastLog := time.Time{}
	for {
		if r, u := nodeReport(); len(u) == 0 {
			close(n.allNodesConnected)
			return
		} else if time.Since(lastLog) > time.Minute {
			lastLog = time.Now()
			n.log.Infof("Node relay waiting on sources. %d / %d connected. Missing sources for relays %+v", len(r), len(r)+len(u), u)
		}
		select {
		case <-time.After(time.Second):
		case <-n.ctx.Done():
			return
		}
	}
}

// WaitForSourceNodes returns a channel that will be closed when a source node
// has connected for all relays.
func (n *Nexus) WaitForSourceNodes() <-chan struct{} {
	return n.allNodesConnected
}

// RelayFile is used for encoding JSON relayfiles. A relayfile is a file that
// contains all the relevant connection information for a source node
// configuration. Nexus will generate a relayfile for each relay ID on startup.
type RelayFile struct {
	RelayID string    `json:"relayID"`
	Cert    dex.Bytes `json:"cert"`
	Addr    string    `json:"addr"`
}

// Connect starts the Nexus, creating a relay node for every relay ID.
func (n *Nexus) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	n.ctx = ctx

	log, wg := n.cfg.Logger, &n.wg

	inAddr := "0.0.0.0:" + n.cfg.Port

	// Create listener.
	listener, err := tls.Listen("tcp", inAddr, n.tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("can't listen on %s. nexus server quitting: %w", inAddr, err)
	}
	// Update the listening address in case a :0 was provided.
	addr := listener.Addr().String()

	for _, relayID := range n.cfg.RelayIDs {
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

	srv := &http.Server{
		Handler:      http.HandlerFunc(n.handleSourceConnect),
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
	log.Infof("Noderelay server listening on %s", addr)

	go n.monitorNodeConnections()

	return &n.wg, nil
}

// handleSourceConnect handles a connection from a source node, upgrading the
// connection to a websocket connection and adding the sourceNode to the
// relayNode.sources.
func (n *Nexus) handleSourceConnect(w http.ResponseWriter, r *http.Request) {
	wg, log, ctx := &n.wg, n.log, n.ctx
	conn, err := ws.NewConnection(w, r, PongWait)
	if err != nil {
		log.Errorf("ws connection error: %v", err)
		return
	}
	ip := dex.NewIPKey(r.RemoteAddr)

	wg.Add(1)
	go func() {
		defer wg.Done()

		cl := ws.NewWSLink(ip.String(), conn, PingPeriod, nil, log)
		defer cl.Disconnect()

		node := &sourceNode{
			cl:           cl,
			addr:         r.RemoteAddr,
			respHandlers: make(map[uint64]*responseHandler),
		}

		registered := make(chan error)
		cl.RawHandler = func(b []byte) {
			var resp RelayedMessage
			if err := json.Unmarshal(b, &resp); err != nil {
				n.log.Errorf("error unmarshalling connect message: %v", err)
				return
			}

			if resp.MessageID == 0 {
				node.relayID = string(resp.Body)
				select {
				case registered <- nil:
				default:
					log.Debugf("blocking node id channel")
				}
				return
			}
			if node.relayID == "" {
				registered <- fmt.Errorf("received numbered request from %s before node ID", ip)
				cl.Disconnect()
				return
			}
			respHandler := node.respHandler(resp.MessageID)
			if respHandler == nil {
				n.log.Errorf("no handler for response from %s", ip)
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

		const readLimit = 2_097_152 // 2 MiB
		cl.SetReadLimit(readLimit)

		select {
		case err := <-registered:
			if err != nil {
				log.Error(err)
				return
			}
		case <-time.After(connectTimeoutSeconds * time.Second):
			log.Errorf("connected nexus source failed to ID")
			return
		case <-ctx.Done():
			return
		}

		if _, found := n.relayAddrs[node.relayID]; !found {
			log.Warnf("source node trying to register with unknown relay ID %s", node.relayID)
			return
		}

		relay, exists := n.relays[node.relayID]
		if !exists {
			log.Errorf("no relay with ID %s for source node connecting from %s", node.relayID, node.addr)
			return
		}
		relay.Lock()
		if oldNode, exists := relay.sources[node.addr]; exists {
			oldNode.cl.Disconnect()
		}
		relay.sources[node.addr] = node
		nodeCount := len(relay.sources)
		relay.Unlock()

		log.Infof("Source node for relay %q has connected from IP %s. %d sources now serving this relay", node.relayID, node.addr, nodeCount)

		defer func() {
			relay.Lock()
			delete(relay.sources, node.addr)
			nodeCount := len(relay.sources)
			relay.Unlock()
			log.Infof("Source node %s has disconnected from relay %s. %d sources now serving this relay", node.addr, node.relayID, nodeCount)

		}()

		cm.Wait()
	}()
}

// RelayedMessage is the format with which HTTP requests are routed over the
// source nodes' WebSocket connections.
type RelayedMessage struct {
	MessageID uint64              `json:"messageID"`
	Method    string              `json:"method,omitempty"`
	Body      dex.Bytes           `json:"body"`
	Headers   map[string][]string `json:"headers,omitempty"`
}

var messageIDCounter uint64

// runRelayServer runs a relayNode server. This server accepts requests from
// local consumers and routes them to a waiting source node connection.
func (n *Nexus) runRelayServer(relayID string) (string, error) {
	log, wg := n.cfg.Logger, &n.wg

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("error getting nexus listener for relay ID %s: %w", relayID, err)
	}

	relayAddr := l.Addr().String()
	mgr := n.relays[relayID]

	// This is a request coming from a local dcrdex backend. Send it to any
	// waiting sourceNode and handle the response.
	handleRequest := func(w http.ResponseWriter, r *http.Request) {
		// Parse the request.
		b, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			n.log.Errorf("Error reading request for relay ID %s: %v", relayID, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		// Format for WebSockets.
		reqID := atomic.AddUint64(&messageIDCounter, 1)
		reqB, err := json.Marshal(&RelayedMessage{
			MessageID: reqID,
			Method:    r.Method,
			Body:      b,
			Headers:   r.Header,
		})
		if err != nil {
			log.Errorf("Error marshaling RelayedMessage: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		// Prepare a list of sources.
		mgr.RLock()
		nodeList := make([]*sourceNode, 0, len(mgr.sources))
		for _, n := range mgr.sources {
			nodeList = append(nodeList, n)
		}
		mgr.RUnlock()
		if len(nodeList) == 0 {
			http.Error(w, fmt.Sprintf("No nodes connected for relay %s", relayID), http.StatusServiceUnavailable)
			return
		}

		// Randomly shuffle the list.
		rand.Shuffle(len(nodeList), func(i, j int) {
			nodeList[i], nodeList[j] = nodeList[j], nodeList[i]
		})

		// result is used to track the best result from the nodeList. The first
		// non-error result is used to respond to the consumer.
		type result struct {
			body []byte
			hdrs map[string][]string
			err  error
		}

		var res *result

	out:
		for i, node := range nodeList {
			resultC := make(chan *result)
			node.logReq(reqID, func(body []byte, hdrs map[string][]string) {
				resultC <- &result{body: body, hdrs: hdrs}
			}, func() {
				resultC <- &result{err: fmt.Errorf("request expired")}
			})

			node.cl.SendRaw(reqB)
			select {
			case res = <-resultC:
				if res.err == nil {
					break out
				}
				log.Errorf("Error requesting data from %s node at %s: %v", node.relayID, node.addr, res.err)
				if i < len(nodeList)-1 {
					log.Infof("Trying another source node")
				}
			case <-n.ctx.Done():
				return
			}
		}
		if res == nil || res.err != nil {
			http.Error(w, "all source nodes errored", http.StatusTeapot)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		for k, vs := range res.hdrs {
			for _, v := range vs {
				w.Header().Set(k, v)
			}
		}
		w.WriteHeader(http.StatusOK)
		if _, err = w.Write(res.body); err != nil {
			log.Errorf("Write error: %v", err)
		}
	}

	// Start the server.
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
		log.Infof("Nexus no longer serving relay %s: err = %v", relayID, err)
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
