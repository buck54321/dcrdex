package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/websocket"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/ws"
)

// https://testnet.binance.vision/

const (
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

var (
	log         = dex.StdOutLogger("TBNC", dex.LevelDebug)
	ctx, cancel = context.WithCancel(context.Background())
	httpAddr, _ = url.Parse("https://testnet.binance.vision")
)

func main() {
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		for range killChan {
			log.Infof("Shutdown signal received")
			cancel()
		}
	}()

	if err := mainErr(ctx); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr(ctx context.Context) error {
	f := &FakeBinance{
		wsServer: websocket.New(nil, log.SubLogger("WS")),
		balances: map[string]*balance{
			"dcr": {
				free:   1000,
				locked: 0,
			},
			"btc": {
				free:   1000,
				locked: 0,
			},
		},
	}
	http.HandleFunc("/", f.handleRequest)
	http.HandleFunc("/ws", f.handleWebsocket)

	http.ListenAndServe(":37346", nil)
	return nil
}

type balance struct {
	free   float64
	locked float64
}

type FakeBinance struct {
	wsServer *websocket.Server

	balanceMtx sync.RWMutex
	balances   map[string]*balance
}

func (f *FakeBinance) handleRequest(w http.ResponseWriter, r *http.Request) {
	r.URL.Host = httpAddr.Host
	r.Host = httpAddr.Host

	path := strings.TrimLeft(r.URL.Path, "/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) == 0 {
		log.Errorf("invalid path: %v", r.URL.Path)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	root := pathParts[0]
	path = "/" + path // to match docs and client

	if root == "sapi" {
		f.handleSapi(path, w, r)
	}

	log.Infof("%q request received", path)

	switch path {
	default:
	}

	// Forward the request
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		log.Errorf("error forwarding request: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("error reading response: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hdr := w.Header()
	for k, vals := range resp.Header {
		for _, v := range vals {
			hdr.Add(k, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	w.Write(append(b, byte('\n')))
}

func (f *FakeBinance) handleSapi(path string, w http.ResponseWriter, r *http.Request) {
	switch path {
	case "/sapi/v1/capital/config/getall":
		f.handleWalletCoinsReq(w, r)
	default:
		log.Errorf("unknown sapi path: %q", path)
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
	}
}

func (f *FakeBinance) handleWalletCoinsReq(w http.ResponseWriter, r *http.Request) {
	ci := f.coinInfo()
	writeJSONWithStatus(w, ci, http.StatusOK)
}

type fakeBinanceNetworkInfo struct {
	MinConfirm              int    `json:"minConfirm"`
	Network                 string `json:"network"`
	UnLockConfirm           int    `json:"unLockConfirm"`
	WithdrawEnable          bool   `json:"withdrawEnable"`
	WithdrawFee             string `json:"withdrawFee"`
	WithdrawIntegerMultiple string `json:"withdrawIntegerMultiple"`
	WithdrawMax             string `json:"withdrawMax"`
	WithdrawMin             string `json:"withdrawMin"`
}

type fakeBinanceCoinInfo struct {
	Coin        string                    `json:"coin"`
	Free        string                    `json:"free"`
	Locked      string                    `json:"locked"`
	Withdrawing string                    `json:"withdrawing"`
	NetworkList []*fakeBinanceNetworkInfo `json:"networkList"`
}

func (f *FakeBinance) coinInfo() (coins []*fakeBinanceCoinInfo) {
	f.balanceMtx.Lock()
	for symbol, bal := range f.balances {
		bigSymbol := strings.ToUpper(symbol)
		coins = append(coins, &fakeBinanceCoinInfo{
			Coin:   bigSymbol,
			Free:   strconv.FormatFloat(bal.free, 'f', 8, 64),
			Locked: strconv.FormatFloat(bal.locked, 'f', 8, 64),
			NetworkList: []*fakeBinanceNetworkInfo{
				{
					Network:                 bigSymbol,
					MinConfirm:              1,
					WithdrawEnable:          true,
					WithdrawFee:             strconv.FormatFloat(0.00000800, 'f', 8, 64),
					WithdrawIntegerMultiple: strconv.FormatFloat(0.00000001, 'f', 8, 64),
					WithdrawMax:             strconv.FormatFloat(1000, 'f', 8, 64),
					WithdrawMin:             strconv.FormatFloat(0.01, 'f', 8, 64),
				},
			},
		})
	}
	f.balanceMtx.Unlock()
	return
}

func (f *FakeBinance) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wsConn, err := ws.NewConnection(w, r, pongWait)
	if err != nil {
		log.Errorf("ws connection error: %v", err)
		return
	}
	var remoteLink comms.WsConn
	localLink := ws.NewWSLink(r.RemoteAddr, wsConn, pingPeriod, nil, log.SubLogger(r.RemoteAddr))
	localLink.UseRawHandler(func(b []byte) {
		if err := remoteLink.SendRaw(b); err != nil {
			log.Errorf("remote SendRaw error: %v", err)
			cancel()
		}
	})

	remoteLink, err = comms.NewWsConn(&comms.WsCfg{
		URL:      "wss://testnet.binance.vision/ws",
		PingWait: pingPeriod,
		// Cert: ,
		ReconnectSync:    func() {},
		ConnectEventFunc: func(cs comms.ConnectionStatus) {},
		Logger:           log.SubLogger(r.RemoteAddr),
		RawHandler: func(b []byte) {
			if err := localLink.SendRaw(b); err != nil {
				log.Errorf("local SendRaw error: %v", err)
				cancel()
			}
		},
	})
	if err != nil {
		log.Errorf("NewWsConn error: %v", err)
		return
	}

	remoteCM := dex.NewConnectionMaster(remoteLink)
	if err = remoteCM.ConnectOnce(ctx); err != nil {
		log.Errorf("websocketHandler remote connect: %v", err)
		return
	}

	localCM := dex.NewConnectionMaster(localLink)
	if err = localCM.ConnectOnce(ctx); err != nil {
		log.Errorf("websocketHandler local connect: %v", err)
		return
	}

	remoteCM.Wait()
	localCM.Wait()
}

// writeJSON writes marshals the provided interface and writes the bytes to the
// ResponseWriter with the specified response code.
func writeJSONWithStatus(w http.ResponseWriter, thing interface{}, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, err := json.Marshal(thing)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf("JSON encode error: %v", err)
		return
	}
	w.WriteHeader(code)
	_, err = w.Write(append(b, byte('\n')))
	if err != nil {
		log.Errorf("Write error: %v", err)
	}
}
