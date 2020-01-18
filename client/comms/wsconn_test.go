package comms

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/certgen"
	"github.com/decred/slog"
	"github.com/gorilla/websocket"
)

func makeRequest(id uint64, route string, msg interface{}) *msgjson.Message {
	req, _ := msgjson.NewRequest(id, route, msg)
	return req
}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string, altDNSNames []string) error {
	log.Infof("Generating TLS certificates...")

	org := "dcrdex autogenerated cert"
	validUntil := time.Now().Add(10 * 365 * 24 * time.Hour)
	cert, key, err := certgen.NewTLSCertPair(elliptic.P521(), org,
		validUntil, altDNSNames)
	if err != nil {
		return err
	}

	// Write cert and key files.
	if err = ioutil.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = ioutil.WriteFile(keyFile, key, 0600); err != nil {
		os.Remove(certFile)
		return err
	}

	log.Infof("Done generating TLS certificates")
	return nil
}

func TestWsConn(t *testing.T) {
	upgrader := websocket.Upgrader{}

	pingCh := make(chan struct{})
	readPumpCh := make(chan interface{})
	writePumpCh := make(chan *msgjson.Message)
	shutdown := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	pingWait := time.Millisecond * 200

	var wsc *WsConn

	id := uint64(0)
	handler := func(w http.ResponseWriter, r *http.Request) {
		hCtx, hCancel := context.WithCancel(context.Background())
		atomic.AddUint64(&id, 1)

		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("unable to upgrade http connection: %s", err)
		}

		c.SetPongHandler(func(string) error {
			id := atomic.LoadUint64(&id)
			t.Logf("handler #%d: pong received", id)
			return nil
		})

		go func() {
			for {
				select {
				case <-pingCh:
					err := c.WriteControl(websocket.PingMessage, []byte{},
						time.Now().Add(writeWait))
					if err != nil {
						t.Errorf("handler #%d: ping error: %v", id, err)
						return
					}

					id := atomic.LoadUint64(&id)
					t.Logf("handler #%d: ping sent", id)

				case msg := <-readPumpCh:
					err := c.WriteJSON(msg)
					if err != nil {
						t.Errorf("handler #%d: write error: %v", id, err)
						return
					}

				case <-hCtx.Done():
					return
				}
			}
		}()

		for {
			mType, message, err := c.ReadMessage()
			if err != nil {
				c.Close()
				hCancel()

				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					// Terminate on a normal close message.
					return
				}

				// DRAFT NOTE: This was a t.Fatalf before, but it was causing an error
				// I don't yet understand. May need to ask @dnldd for advice.
				fmt.Printf("handler #%d: read error: %v\n", id, err)
				return
			}

			if mType == websocket.TextMessage {
				msg, err := msgjson.DecodeMessage(message)
				if err != nil {
					t.Errorf("handler #%d: decode error: %v", id, err)
					c.Close()
					hCancel()
					return
				}

				writePumpCh <- msg
			}
		}
	}

	certFile, err := ioutil.TempFile("", "certfile")
	if err != nil {
		t.Fatalf("unable to create temp certfile: %s", err)
	}
	certFile.Close()
	defer os.Remove(certFile.Name())

	keyFile, err := ioutil.TempFile("", "keyfile")
	if err != nil {
		t.Fatalf("unable to create temp keyfile: %s", err)
	}
	keyFile.Close()
	defer os.Remove(keyFile.Name())

	err = genCertPair(certFile.Name(), keyFile.Name(), nil)
	if err != nil {
		t.Fatal(err)
	}

	host := "127.0.0.1:6060"
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handler)

	server := &http.Server{
		WriteTimeout: time.Second * 5,
		ReadTimeout:  time.Second * 5,
		IdleTimeout:  time.Second * 5,
		Addr:         host,
		Handler:      mux,
	}

	defer server.Close()

	go func() {
		err := server.ListenAndServeTLS(certFile.Name(), keyFile.Name())
		if err != nil {
			fmt.Println(err)
		}
	}()

	cfg := &WsCfg{
		URL:      "wss://" + host + "/ws",
		PingWait: pingWait,
		RpcCert:  certFile.Name(),
		Ctx:      ctx,
	}
	wsc, err = NewWsConn(cfg)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		wsc.WaitForShutdown()
		shutdown <- struct{}{}
	}()

	reconnectAndPing := func() {
		// Drop the connection and force a reconnect by waiting.
		time.Sleep(time.Millisecond * 210)

		// Wait for a reconnection.
		for !wsc.isConnected() {
			time.Sleep(time.Millisecond * 10)
			continue
		}

		// Send a ping.
		pingCh <- struct{}{}
	}

	orderid, _ := hex.DecodeString("ceb09afa675cee31c0f858b94c81bd1a4c2af8c5947d13e544eef772381f2c8d")
	matchid, _ := hex.DecodeString("7c6b44735e303585d644c713fe0e95897e7e8ba2b9bba98d6d61b70006d3d58c")
	match := &msgjson.Match{
		OrderID:  orderid,
		MatchID:  matchid,
		Quantity: 20,
		Rate:     2,
		Address:  "DsiNAJCd2sSazZRU9ViDD334DaLgU1Kse3P",
	}

	// Ensure a malformed message to the client does not terminate
	// the connection.
	readPumpCh <- []byte("{notjson")

	// Send a message to the client.
	sent := makeRequest(1, msgjson.MatchRoute, match)
	readPumpCh <- sent

	// Fetch the read source.
	readSource := wsc.MessageSource()
	if readSource == nil {
		t.Fatal("expected a non-nil read source")
	}

	// Ensure the read source can be fetched once.
	rSource := wsc.MessageSource()
	if rSource != nil {
		t.Fatal("expected a nil read source")
	}

	// Read the message received by the client.
	received := <-readSource

	// Ensure the received message equal to the sent message.
	if received.Type != sent.Type {
		t.Fatalf("expected %v type, got %v", sent.Type, received.Type)
	}

	if received.Route != sent.Route {
		t.Fatalf("expected %v route, got %v", sent.Route, received.Route)
	}

	if received.ID != sent.ID {
		t.Fatalf("expected %v id, got %v", sent.ID, received.ID)
	}

	if !bytes.Equal(received.Payload, sent.Payload) {
		t.Fatal("sent and received payload mismatch")
	}

	reconnectAndPing()

	coinID := []byte{
		0xc3, 0x16, 0x10, 0x33, 0xde, 0x09, 0x6f, 0xd7, 0x4d, 0x90, 0x51, 0xff,
		0x0b, 0xd9, 0x9e, 0x35, 0x9d, 0xe3, 0x50, 0x80, 0xa3, 0x51, 0x10, 0x81,
		0xed, 0x03, 0x5f, 0x54, 0x1b, 0x85, 0x0d, 0x43, 0x00, 0x00, 0x00, 0x0a,
	}

	contract, _ := hex.DecodeString("caf8d277f80f71e4")
	init := &msgjson.Init{
		OrderID:  orderid,
		MatchID:  matchid,
		CoinID:   coinID,
		Time:     1570704776,
		Contract: contract,
	}

	// Send a message from the client.
	mId := wsc.NextID()
	sent = makeRequest(mId, msgjson.InitRoute, init)
	handlerRun := false
	err = wsc.Request(sent, func(*msgjson.Message) {
		handlerRun = true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read the message received by the server.
	received = <-writePumpCh

	// Ensure the received message equal to the sent message.
	if received.Type != sent.Type {
		t.Fatalf("expected %v type, got %v", sent.Type, received.Type)
	}

	if received.Route != sent.Route {
		t.Fatalf("expected %v route, got %v", sent.Route, received.Route)
	}

	if received.ID != sent.ID {
		t.Fatalf("expected %v id, got %v", sent.ID, received.ID)
	}

	if !bytes.Equal(received.Payload, sent.Payload) {
		t.Fatal("sent and received payload mismatch")
	}

	// Ensure the next id is as expected.
	next := wsc.NextID()
	if next != 2 {
		t.Fatalf("expected next id to be %d, got %d", 2, next)
	}

	// Ensure the request got logged.
	hndlr := wsc.respHandler(mId)
	if hndlr == nil {
		t.Fatalf("no handler found")
	}
	hndlr.f(nil)
	if !handlerRun {
		t.Fatalf("wrong handler retrieved")
	}

	// Lookup an unlogged request id.
	hndlr = wsc.respHandler(next)
	if hndlr != nil {
		t.Fatal("expected an error for unlogged id")
	}

	// Drop the connection and force a reconnect by waiting.
	time.Sleep(time.Millisecond * 210)

	// Ensure the connection is disconnected.
	for wsc.isConnected() {
		time.Sleep(time.Millisecond * 10)
		continue
	}

	// Try sending a message on a disconnected connection.
	err = wsc.Send(sent)
	if err == nil {
		t.Fatalf("expected a connection state error")
	}

	cancel()
	<-shutdown
}

func TestFailingConnection(t *testing.T) {
	shutdown := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	backendLogger := slog.NewBackend(os.Stdout)
	defer os.Stdout.Sync()
	log := backendLogger.Logger("Debug")
	log.SetLevel(slog.LevelTrace)
	UseLogger(log)

	pingWait := time.Millisecond * 200

	certFile, err := ioutil.TempFile("", "certfile")
	if err != nil {
		t.Fatalf("unable to create temp certfile: %s", err)
	}
	certFile.Close()
	defer os.Remove(certFile.Name())

	keyFile, err := ioutil.TempFile("", "keyfile")
	if err != nil {
		t.Fatalf("unable to create temp keyfile: %s", err)
	}
	keyFile.Close()
	defer os.Remove(keyFile.Name())

	err = genCertPair(certFile.Name(), keyFile.Name(), nil)
	if err != nil {
		t.Fatal(err)
	}

	host := "127.0.0.1:6060"
	cfg := &WsCfg{
		URL:      "wss://" + host + "/ws",
		PingWait: pingWait,
		RpcCert:  certFile.Name(),
		Ctx:      ctx,
	}
	// Initial connection will fail immediately
	wsc, err := NewWsConn(cfg)
	if err == nil {
		t.Fatal("no error for non-existent server")
	}

	go func() {
		wsc.WaitForShutdown()
		shutdown <- struct{}{}
	}()

	oldCount := atomic.LoadUint64(&wsc.reconnects)
	tick := func() { time.Sleep(time.Millisecond * 210) }
	for idx := 0; idx < 5; idx++ {
		tick()

		// Ensure the connection status is false and the number of
		// reconnect attempts have increased.
		if wsc.isConnected() {
			t.Fatalf("expected the connection to be in a disconnected state")
		}

		updatedCount := atomic.LoadUint64(&wsc.reconnects)
		if updatedCount == oldCount || updatedCount < oldCount {
			t.Fatalf("expected the connection to have "+
				"increased connection attempts, %v", updatedCount)
		}

		oldCount = updatedCount
	}

	cancel()
	<-shutdown
}
