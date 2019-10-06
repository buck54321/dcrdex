package comms

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrdex/server/comms/rpc"
	// "github.com/decred/slog"
	"github.com/gorilla/websocket"
)

var testCtx context.Context

func newServer() *RPCServer {
	return &RPCServer{
		clients:    make(map[uint64]*RPCClient),
		quarantine: make(map[string]time.Time),
	}
}

type wsConnStub struct {
	msg     chan []byte
	quit    chan struct{}
	write   int
	read    int
	close   int
	lastMsg []byte
}

func newWsStub() *wsConnStub {
	return &wsConnStub{
		msg:  make(chan []byte),
		quit: make(chan struct{}),
	}
}

var testMtx sync.RWMutex

// check results under lock.
func lockedExe(f func()) {
	testMtx.Lock()
	defer testMtx.Unlock()
	f()
}

// nonEOF can specify a particular error should be returned through ReadMessage.
var nonEOF = make(chan struct{})
var pongTrigger = []byte("pong")

func (conn *wsConnStub) ReadMessage() (int, []byte, error) {
	var b []byte
	select {
	case b = <-conn.msg:
		if bytes.Equal(b, pongTrigger) {
			return websocket.PongMessage, []byte{}, nil
		}
	case <-conn.quit:
		return 0, nil, io.EOF
	case <-testCtx.Done():
		return 0, nil, io.EOF
	case <-nonEOF:
		return 0, nil, fmt.Errorf("test nonEOF error")
	}
	conn.read++
	return 0, b, nil
}

var writeErr = ""

func (conn *wsConnStub) WriteMessage(msgType int, msg []byte) error {
	testMtx.Lock()
	defer testMtx.Unlock()
	if msgType == websocket.PingMessage {
		select {
		case conn.msg <- pongTrigger:
		default:
		}
		return nil
	}
	conn.lastMsg = msg
	conn.write++
	if writeErr == "" {
		return nil
	}
	err := fmt.Errorf(writeErr)
	writeErr = ""
	return err
}

func (conn *wsConnStub) Close() error {
	select {
	case <-conn.quit:
	default:
		close(conn.quit)
	}
	conn.close++
	return nil
}

func dummyRPCMethod(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
	return nil
}

var reqID int

func makeReq(method, msg string) *rpc.Request {
	reqID++
	return &rpc.Request{
		Jsonrpc: rpc.JSONRPCVersion,
		Method:  method,
		Params:  []byte(msg),
		ID:      reqID,
	}
}

func makeReqMsg(method, msg string) *rpc.Message {
	reqBytes, _ := json.Marshal(makeReq(method, msg))
	return makeMsg(rpc.RequestMessage, reqBytes)
}

func makeResp(id interface{}, msg string) *rpc.Response {
	return &rpc.Response{
		Jsonrpc: rpc.JSONRPCVersion,
		Result:  []byte(msg),
		Error:   nil,
		ID:      &id,
	}
}

func makeRespMsg(id interface{}, msg string) *rpc.Message {
	respBytes, _ := json.Marshal(makeResp(id, msg))
	return makeMsg(rpc.ResponseMessage, respBytes)
}

func makeMsg(msgType uint8, b []byte) *rpc.Message {
	return &rpc.Message{
		Type:    msgType,
		Payload: json.RawMessage(b),
	}
}

func decodeRespMsg(msgBytes []byte) *rpc.Response {
	msg := new(rpc.Message)
	err := json.Unmarshal(msgBytes, &msg)
	if err != nil {
		fmt.Printf("decodeRespMsg error decoding message: %v\n", err)
	}
	if msg.Type != rpc.ResponseMessage {
		fmt.Printf("wrong message type for decodeRespMsg. exptected %d, got %d\n", rpc.ResponseMessage, msg.Type)
	}
	resp := new(rpc.Response)
	err = json.Unmarshal(msg.Payload, &resp)
	if err != nil {
		fmt.Printf("decodeRespMsg error decoding message: %v\n", err)
	}
	return resp
}

func sendToConn(t *testing.T, conn *wsConnStub, method, msg string) {
	encMsg, err := json.Marshal(makeReqMsg(method, msg))
	if err != nil {
		t.Fatalf("error encoding %s request: %v", method, err)
	}
	conn.msg <- encMsg
	time.Sleep(time.Millisecond * 10)
}

func sendReplace(t *testing.T, conn *wsConnStub, thing interface{}, old, new string) {
	enc, err := json.Marshal(thing)
	if err != nil {
		t.Fatalf("error encoding thing for sendReplace: %v", err)
	}
	s := string(enc)
	s = strings.ReplaceAll(s, old, new)
	conn.msg <- []byte(s)
	time.Sleep(time.Millisecond)
}

func newTestDEXClient(addr string, rootCAs *x509.CertPool) (*websocket.Conn, error) {
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment, // Same as DefaultDialer.
		HandshakeTimeout: 10 * time.Second,          // DefaultDialer is 45 seconds.
		TLSClientConfig: &tls.Config{
			RootCAs:            rootCAs,
			InsecureSkipVerify: true,
		},
	}

	conn, _, err := dialer.Dial(addr, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func TestMain(m *testing.M) {
	var shutdown func()
	testCtx, shutdown = context.WithCancel(context.Background())
	defer shutdown()
	// UseLogger(slog.NewBackend(os.Stdout).Logger("COMMSTEST"))
	os.Exit(m.Run())
}

// method strings cannot be empty.
func TestRegisterMethod_PanicsEmtpyString(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("no panic on registering empty string method")
		}
	}()
	RegisterMethod("", dummyRPCMethod)
}

// methods cannot be registered more than once.
func TestRegisterMethod_PanicsDoubleRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("no panic on registering empty string method")
		}
	}()
	RegisterMethod("somemethod", dummyRPCMethod)
	RegisterMethod("somemethod", dummyRPCMethod)
}

// Test the server with a stub for the client connections.
func TestClientRequests(t *testing.T) {
	server := newServer()
	var client *RPCClient
	var conn *wsConnStub
	stubAddr := "testaddr"
	sendToServer := func(method, msg string) { sendToConn(t, conn, method, msg) }

	clientOn := func() bool {
		var on bool
		lockedExe(func() {
			client.quitMtx.RLock()
			defer client.quitMtx.RUnlock()
			on = client.on
		})
		return on
	}

	// Register all methods before sending any requests.
	// 'getclient' grabs the server's RPCClient.
	RegisterMethod("getclient", func(c *RPCClient, _ *rpc.Request) *rpc.RPCError {
		testMtx.Lock()
		defer testMtx.Unlock()
		client = c
		return nil
	})
	// Check request parses the request to a map of strings.
	var parsedParams map[string]string
	RegisterMethod("checkrequest", func(c *RPCClient, req *rpc.Request) *rpc.RPCError {
		testMtx.Lock()
		defer testMtx.Unlock()
		parsedParams = make(map[string]string)
		err := json.Unmarshal(req.Params, &parsedParams)
		if err != nil {
			t.Fatalf("request parse error: %v", err)
		}
		if client.id != c.id {
			t.Fatalf("client ID mismatch. %d != %d", client.id, c.id)
		}
		return nil
	})
	// 'checkinvalid' should never be run, since the request has invalid
	// formatting.
	passed := false
	RegisterMethod("checkinvalid", func(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
		testMtx.Lock()
		defer testMtx.Unlock()
		passed = true
		return nil
	})
	// 'error' returns an RPCError.
	RegisterMethod("error", func(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
		return rpc.NewRPCError(550, "somemessage")
	})
	// 'ban' quarantines the user using the RPCQuarantineClient error code.
	RegisterMethod("ban", func(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
		return rpc.NewRPCError(rpc.RPCQuarantineClient, "user quarantined")
	})

	// A helper function to reconnect to the server and grab the server's
	// RPCClient.
	reconnect := func() {
		conn = newWsStub()
		go server.websocketHandler(conn, stubAddr)
		time.Sleep(time.Millisecond * 10)
		sendToServer("getclient", `{}`)
	}
	reconnect()

	sendToServer("getclient", `{}`)
	lockedExe(func() {
		if client == nil {
			t.Fatalf("'getclient' failed")
		}
		if server.clientCount() != 1 {
			t.Fatalf("clientCount != 1")
		}
	})

	// Check that the request is parsed as expected.
	sendToServer("checkrequest", `{"key":"value"}`)
	lockedExe(func() {
		v, found := parsedParams["key"]
		if !found {
			t.Fatalf("RegisterMethod key not found")
		}
		if v != "value" {
			t.Fatalf(`expected "value", got %s`, v)
		}
	})

	// Send invalid params, and make sure the server doesn't pass the message. The
	// server will not disconnect the client.
	checkReplacePass := func(old, new string) {
		sendReplace(t, conn, makeReqMsg("checkinvalid", old), old, new)
		if passed {
			t.Fatalf("invalid request passed to handler")
		}
	}
	checkReplacePass(`{"a":"b"}`, "?")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after invalid message")
	}

	// Send the invalid message again, but error out on the server's WriteMessage
	// attempt. The server should disconnect the client in this case.
	writeErr = "basic error"
	checkReplacePass(`{"a":"b"}`, "?")
	if clientOn() {
		t.Fatalf("client connected after WriteMessage error")
	}
	if server.clientCount() != 0 {
		t.Fatalf("clientCount != 0")
	}

	// Send incorrect RPC version
	reconnect()
	checkReplacePass(`"2.0"`, `"3.0"`)
	if clientOn() {
		t.Fatalf("client connected after invalid json version")
	}

	// Shut the client down. Check the on flag.
	reconnect()
	lockedExe(func() { client.disconnect() })
	time.Sleep(time.Millisecond)
	if clientOn() {
		t.Fatalf("shutdown client has on flag set")
	}
	// Shut down again.
	lockedExe(func() { client.disconnect() })

	// Reconnect and try shutting down with non-EOF error.
	reconnect()
	nonEOF <- struct{}{}
	time.Sleep(time.Millisecond)
	if clientOn() {
		t.Fatalf("failed to shutdown on non-EOF error")
	}

	// Try a non-existent handler. This should not result in a disconnect.
	reconnect()
	sendToServer("nonexistent", "{}")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after invalid method")
	}

	// Again, but with an WriteMessage error when sending error to client. This
	// should result in a disconnection.
	writeErr = "basic error"
	sendToServer("nonexistent", "{}")
	if clientOn() {
		t.Fatalf("client still connected after WriteMessage error for invalid method")
	}

	// An RPC error. No disconnect.
	reconnect()
	sendToServer("error", "{}")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after rpc error")
	}

	// Return a user quarantine error.
	sendToServer("ban", "{}")
	if clientOn() {
		t.Fatalf("client still connected after quarantine error")
	}
	if !server.isQuarantined(stubAddr) {
		t.Fatalf("server has not marked client as quarantined")
	}

	checkParseError := func() {
		lockedExe(func() {
			resp := decodeRespMsg(conn.lastMsg)
			conn.lastMsg = nil
			if resp.Error == nil || resp.Error.Code != rpc.RPCParseError {
				t.Fatalf("no error after invalid id")
			}
		})
	}

	// Test an invalid ID.
	reconnect()
	req := makeReq("getclient", `{}`)
	req.ID = 555
	msg, _ := req.Message()
	sendReplace(t, conn, msg, "555", "{}")
	checkParseError()

	// Test null ID
	sendReplace(t, conn, msg, "555", "null")
	checkParseError()

}

func TestClientResponses(t *testing.T) {
	server := newServer()
	var client *RPCClient
	var conn *wsConnStub
	stubAddr := "testaddr"

	// Register all methods before sending any requests.
	// 'getclient' grabs the server's RPCClient.
	RegisterMethod("grabclient", func(c *RPCClient, _ *rpc.Request) *rpc.RPCError {
		testMtx.Lock()
		defer testMtx.Unlock()
		client = c
		return nil
	})

	getClient := func() {
		encReq, _ := json.Marshal(makeReqMsg("grabclient", `{}`))
		conn.msg <- encReq
		time.Sleep(time.Millisecond * 10)
	}

	sendToClient := func(method, params string, f func(*rpc.Response)) interface{} {
		req := makeReq(method, params)
		err := client.Request(req, f)
		if err != nil {
			t.Logf("sendToClient error: %v", err)
		}
		time.Sleep(time.Millisecond * 10)
		return req.ID
	}

	respondToServer := func(id interface{}, msg string) {
		encResp, err := json.Marshal(makeRespMsg(id, msg))
		if err != nil {
			t.Fatalf("error encoding %v (%T) request: %v", id, id, err)
		}
		conn.msg <- encResp
		time.Sleep(time.Millisecond * 10)
	}

	reconnect := func() {
		conn = newWsStub()
		go server.websocketHandler(conn, stubAddr)
		time.Sleep(time.Millisecond * 10)
		getClient()
	}
	reconnect()

	// Send a request from the server to the client, setting a flag when the
	// client responds.
	responded := false
	id := sendToClient("looptest", `{}`, func(resp *rpc.Response) {
		responded = true
	})
	// Respond to the server
	respondToServer(id, `{}`)
	lockedExe(func() {
		if !responded {
			t.Fatalf("no response for looptest")
		}
	})

	checkParseError := func(tag string) {
		lockedExe(func() {
			resp := decodeRespMsg(conn.lastMsg)
			conn.lastMsg = nil
			if resp.Error == nil || resp.Error.Code != rpc.RPCParseError {
				t.Fatalf("no error after %s id", tag)
			}
		})
	}

	// Test an invalid id type.
	id = struct{}{}
	respondToServer(id, `{}`)
	checkParseError("invalid id")

	// Test a negative id
	id = -1
	respondToServer(id, `{}`)
	checkParseError("negative id")

	// Test a nil id
	id = nil
	respondToServer(id, `{}`)
	checkParseError("null id")

	// Send an invalid payload.
	respondToServer(5, `?`)
	old := `{"a":"b"}`
	sendReplace(t, conn, makeRespMsg(id, old), old, `?`)
	checkParseError("invalid payload")

	// Send a null payload.
	sendReplace(t, conn, makeRespMsg(id, old), old, "null")
	checkParseError("null payload")
}

func TestOnline(t *testing.T) {
	portAddr := ":57623"
	address := "wss://127.0.0.1:57623/ws"
	tempDir, err := ioutil.TempDir("", "example")
	if err != nil {
		t.Fatalf("TempDir error: %v", err)
	}
	defer os.RemoveAll(tempDir) // clean up

	keyPath := filepath.Join(tempDir, "rpc.key")
	certPath := filepath.Join(tempDir, "rpc.cert")
	pongWait = 50 * time.Millisecond
	pingPeriod = (pongWait * 9) / 10
	server, err := NewRPCServer(&RPCConfig{
		ListenAddrs: []string{portAddr},
		RPCKey:      keyPath,
		RPCCert:     certPath,
	})
	if err != nil {
		t.Fatalf("server constructor error: %v", err)
	}
	defer server.Stop()

	// Register methods before starting server.
	// No response simulates a method that returns no response.
	RegisterMethod("noresponse", func(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
		return nil
	})
	// The 'ok' method returns an affirmative response.
	type okresult struct {
		OK bool `json:"ok"`
	}
	RegisterMethod("ok", func(c *RPCClient, req *rpc.Request) *rpc.RPCError {
		resp, err := rpc.NewResponse(req.ID, &okresult{OK: true}, nil)
		if err != nil {
			return rpc.NewRPCError(500, err.Error())
		}
		msg, err := resp.Message()
		if err != nil {
			t.Fatalf("failed to get message from response")
		}
		err = c.sendMessage(msg)
		if err != nil {
			return rpc.NewRPCError(500, err.Error())
		}
		return nil
	})
	// The 'banuser' method quarantines the user.
	RegisterMethod("banuser", func(c *RPCClient, req *rpc.Request) *rpc.RPCError {
		return rpc.NewRPCError(rpc.RPCQuarantineClient, "test quarantine")
	})

	server.Start()

	// Get the SystemCertPool, continue with an empty pool on error
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	// Read in the cert file
	certs, err := ioutil.ReadFile(certPath)
	if err != nil {
		t.Fatalf("Failed to append %q to RootCAs: %v", certPath, err)
	}

	// Append our cert to the system pool
	if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
		t.Fatalf("No certs appended, using system certs only")
	}

	remoteClient, err := newTestDEXClient(address, rootCAs)
	if err != nil {
		t.Fatalf("remoteClient constructor error: %v", err)
	}

	var response, r []byte
	// Check if the response is nil.
	nilResponse := func() bool {
		var isNil bool
		lockedExe(func() { isNil = response == nil })
		return isNil
	}

	// A loop to grab responses from the server.
	var readErr, re error
	go func() {
		for {
			_, r, re = remoteClient.ReadMessage()
			lockedExe(func() {
				response = r
				readErr = re
			})
			if re != nil {
				break
			}
		}
	}()

	sendToDEX := func(method, msg string) error {
		b, err := json.Marshal(makeReqMsg(method, msg))
		if err != nil {
			t.Fatalf("error encoding %s request: %v", method, err)
		}
		err = remoteClient.WriteMessage(websocket.TextMessage, b)
		time.Sleep(time.Millisecond * 10)
		return err
	}

	// Sleep for a few  pongs to make sure the client doesn't disconnect.
	time.Sleep(pongWait * 3)

	err = sendToDEX("noresponse", "{}")
	if err != nil {
		t.Fatalf("noresponse send error: %v", err)
	}
	if !nilResponse() {
		t.Fatalf("response set for 'noresponse' request")
	}

	err = sendToDEX("ok", "{}")
	if err != nil {
		t.Fatalf("noresponse send error: %v", err)
	}
	if nilResponse() {
		t.Fatalf("no response set for 'ok' request")
	}
	var resp *rpc.Response
	lockedExe(func() { resp = decodeRespMsg(response) })
	ok := new(okresult)
	err = json.Unmarshal(resp.Result, &ok)
	if err != nil {
		t.Fatalf("'ok' response unmarshal error: %v", err)
	}
	if !ok.OK {
		t.Fatalf("ok.OK false")
	}

	// Ban the client using the special RPCError code.
	err = sendToDEX("banuser", "{}")
	if err != nil {
		t.Fatalf("banuser send error: %v", err)
	}
	lockedExe(func() {
		if readErr == nil {
			t.Fatalf("no read error after ban")
		}
	})
	// Try connecting, and make sure there is an error.
	_, err = newTestDEXClient(address, rootCAs)
	if err == nil {
		t.Fatalf("no websocket connection error after ban")
	}
	// Manually set the ban time.
	func() {
		server.banMtx.Lock()
		defer server.banMtx.Unlock()
		if len(server.quarantine) != 1 {
			t.Fatalf("unexpected number of quarantined IPs")
		}
		for ip := range server.quarantine {
			server.quarantine[ip] = time.Now()
		}
	}()
	// Now try again. Should connect.
	conn, err := newTestDEXClient(address, rootCAs)
	if err != nil {
		t.Fatalf("error connecting on expired ban")
	}
	time.Sleep(time.Millisecond * 10)
	clientCount := server.clientCount()
	if clientCount != 1 {
		t.Fatalf("server claiming %d clients. Expected 1", clientCount)
	}
	conn.Close()
}

func TestParseListeners(t *testing.T) {
	ipv6wPort := "[fdc5:f621:d3b4:923f::]:80"
	ipv6wZonePort := "[a:b:c:d::%123]:45"
	// Invalid because capital letter O.
	ipv6Invalid := "[1200:0000:AB00:1234:O000:2552:7777:1313]:1234"
	ipv4wPort := "36.182.54.55:80"

	ips := []string{
		ipv6wPort,
		ipv6wZonePort,
		ipv4wPort,
	}

	out4, out6, hasWildcard, err := parseListeners(ips)
	if err != nil {
		t.Fatalf("error parsing listeners: %v", err)
	}
	if len(out4) != 1 {
		t.Fatalf("expected 1 ipv4 addresses. found %d", len(out4))
	}
	if len(out6) != 2 {
		t.Fatalf("expected 2 ipv6 addresses. found %d", len(out6))
	}
	if hasWildcard {
		t.Fatal("hasWildcard true. should be false.")
	}

	// Port-only address goes in both.
	ips = append(ips, ":1234")
	out4, out6, hasWildcard, err = parseListeners(ips)
	if err != nil {
		t.Fatalf("error parsing listeners with wildcard: %v", err)
	}
	if len(out4) != 2 {
		t.Fatalf("expected 2 ipv4 addresses. found %d", len(out4))
	}
	if len(out6) != 3 {
		t.Fatalf("expected 3 ipv6 addresses. found %d", len(out6))
	}
	if !hasWildcard {
		t.Fatal("hasWildcard false with port-only address")
	}

	// No port is invalid
	ips = append(ips, "localhost")
	_, _, _, err = parseListeners(ips)
	if err == nil {
		t.Fatal("no error when no IP specified")
	}

	// Pass invalid address
	_, _, _, err = parseListeners([]string{ipv6Invalid})
	if err == nil {
		t.Fatal("no error with invalid address")
	}
}
