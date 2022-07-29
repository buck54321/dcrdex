// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
)

const (
	httpURL      = "https://api.binance.com"
	websocketURL = "wss://stream.binance.com:9443"
	// Simnet uses the harness-linked forwarder in clent/cmd/testbinance.
	simneHttpURL = "http://localhost:37346"
	simneWsURL   = "ws://localhost:37346"
)

var (
	httpClient = http.DefaultClient
)

type ExchangeBalance struct {
	Available float64
	Locked    float64
}

type WithdrawFees struct {
	Fee             float64
	IntegerMultiple float64
	Max             float64
	Min             float64
}

type MarketUpdate struct {
	BaseSymbol  string
	QuoteSymbol string
	HighestBuy  float64
	LowestSell  float64
}

type CEX interface {
	dex.Connector
	Notifications() <-chan interface{}
	Balance(symbol string) (*ExchangeBalance, error)
	WithdrawalFees(symbol string) (*WithdrawFees, error)
	Withdraw(ctx context.Context, symbol string, addr string, qty uint64) error
	Trade(ctx context.Context, baseSymbol, quoteSymbol string, sell bool, rate, qty uint64) error
	DepositAddress(ctx context.Context, symbol string) (string, error)
	SubscribeMarket(ctx context.Context, baseSymbol, quoteSymbol string, lotSize uint64) error
	UnsubscribeMarket(baseSymbol, quoteSymbol string) error
	VWAP(baseSymbol, quoteSymbol string, sell bool, qty float64) (vwap, extrema float64, filled bool, err error)
	BidsDownTo(baseSymbol, quoteSymbol string, price float64) (bids []*BookBin)
	AsksUpTo(baseSymbol, quoteSymbol string, price float64) (asks []*BookBin)
}

const (
	bncTimestampKey  = "timestamp"
	bncSignatureKey  = "signature"
	bncRecvWindowKey = "recvWindow"
)

type BookBin struct {
	Price float64
	Qty   float64
}

type bncAssetConfig struct {
	assetID          uint32
	symbol           string
	coin             string
	network          string
	conversionFactor uint64
}

type bncBook struct {
	updated             time.Time
	bids                []*BookBin
	asks                []*BookBin
	lotSize             uint64
	lotSizeConventional float64
	base                *bncAssetConfig
	quote               *bncAssetConfig
}

func bidsDownTo(bids []*BookBin, price float64) (bins []*BookBin) {
	for _, bin := range bids {
		if bin.Price < price {
			break
		}
		bins = append(bins, bin)
	}
	return
}

func asksUpTo(asks []*BookBin, price float64) (bins []*BookBin) {
	for _, bin := range asks {
		if bin.Price > price {
			break
		}
		bins = append(bins, bin)
	}
	return
}

func newBNCBook(baseSymbol, quoteSymbol string, lotSize uint64) (*bncBook, error) {
	baseCfg, err := bncSymbolData(baseSymbol)
	if err != nil {
		return nil, fmt.Errorf("error finding base symbol data: %v", err)
	}
	quoteCfg, err := bncSymbolData(quoteSymbol)
	if err != nil {
		return nil, fmt.Errorf("error finding quote symbol data: %v", err)
	}
	return &bncBook{
		bids:                make([]*BookBin, 0),
		asks:                make([]*BookBin, 0),
		lotSize:             lotSize,
		lotSizeConventional: float64(lotSize) / float64(baseCfg.conversionFactor),
		base:                baseCfg,
		quote:               quoteCfg,
	}, nil
}

type Binance struct {
	wg          sync.WaitGroup
	log         dex.Logger
	url         string
	wsURL       string
	apiKey      string
	secretKey   string
	knownAssets map[uint32]bool

	withdrawFees atomic.Value // map[string]*WithdrawFees

	balanceMtx sync.RWMutex
	balances   map[string]*ExchangeBalance

	marketStreamMtx sync.RWMutex
	marketStream    comms.WsConn

	booksMtx sync.RWMutex
	books    map[string]*bncBook

	noteChan chan interface{}
}

var _ CEX = (*Binance)(nil)

func NewBinance(apiKey, secretKey string, log dex.Logger, net dex.Network) *Binance {
	url, wsURL := httpURL, websocketURL
	if net == dex.Simnet {
		url, wsURL = simneHttpURL, simneWsURL
	}

	registeredAssets := asset.Assets()
	knownAssets := make(map[uint32]bool, len(registeredAssets))
	for _, a := range registeredAssets {
		knownAssets[a.ID] = true
	}

	bnc := &Binance{
		log:         log,
		url:         url,
		wsURL:       wsURL,
		apiKey:      apiKey,
		secretKey:   secretKey,
		knownAssets: knownAssets,
		balances:    make(map[string]*ExchangeBalance),
		books:       make(map[string]*bncBook),
	}
	bnc.withdrawFees.Store(make(map[string]*WithdrawFees))
	return bnc
}

func (bnc *Binance) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if err := bnc.getWithdrawFees(ctx); err != nil {
		return nil, err
	}

	// Refresh the withdraw fees periodically.
	bnc.wg.Add(1)
	go func() {
		defer bnc.wg.Done()
		for {
			nextTick := time.After(time.Hour)
			if err := bnc.getWithdrawFees(ctx); err != nil {
				bnc.log.Errorf("error fetching withdraw fees: %v", err)
				nextTick = time.After(time.Minute)
			}
			select {
			case <-nextTick:
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := bnc.getUserDataStream(ctx); err != nil {
		return nil, err
	}
	return &bnc.wg, nil
}

func (bnc *Binance) Notifications() <-chan interface{} {
	bnc.noteChan = make(chan interface{}, 32)
	return bnc.noteChan
}

func (bnc *Binance) Balance(symbol string) (*ExchangeBalance, error) {
	bnc.balanceMtx.RLock()
	defer bnc.balanceMtx.RUnlock()
	bal, found := bnc.balances[symbol]
	if !found {
		return nil, fmt.Errorf("no %q balance found", symbol)
	}
	return bal, nil
}

func (bnc *Binance) WithdrawalFees(symbol string) (*WithdrawFees, error) {
	fees := bnc.withdrawFees.Load().(map[string]*WithdrawFees)
	if fees == nil {
		return nil, errors.New("no fees stored")
	}
	wf, found := fees[symbol]
	if !found {
		return nil, fmt.Errorf("no withdraw fees cached for %q", symbol)
	}
	return wf, nil
}

func (bnc *Binance) DepositAddress(ctx context.Context, symbol string) (string, error) {
	assetCfg, err := bncSymbolData(symbol)
	if err != nil {
		return "", err
	}

	v := make(url.Values)
	v.Add("coin", assetCfg.coin)
	v.Add("network", assetCfg.network)
	v.Add("recvWindow", "5000") // 5000 ms is the default
	v.Add("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	var resp struct {
		Address string `json:"address"`
	}

	return resp.Address, bnc.getAPI(ctx, "/sapi/v1/capital/deposit/address", v, true, true, &resp)
}

func (bnc *Binance) BidsDownTo(baseSymbol, quoteSymbol string, price float64) (bins []*BookBin) {
	slug := BinanceSlug(baseSymbol, quoteSymbol)
	bnc.booksMtx.RLock()
	defer bnc.booksMtx.RUnlock()
	book := bnc.books[slug]
	if book == nil {
		bnc.log.Errorf("BidsDownTo: no book for %q", slug)
		return nil
	}
	return bidsDownTo(book.bids, price)
}

func (bnc *Binance) AsksUpTo(baseSymbol, quoteSymbol string, price float64) (bins []*BookBin) {
	slug := BinanceSlug(baseSymbol, quoteSymbol)
	bnc.booksMtx.RLock()
	defer bnc.booksMtx.RUnlock()
	book := bnc.books[slug]
	if book == nil {
		bnc.log.Errorf("AsksUpTo: no book for %q", slug)
		return nil
	}
	return asksUpTo(book.asks, price)
}

func bncSymbolData(symbol string) (*bncAssetConfig, error) {
	coin := strings.ToUpper(symbol)
	var ok bool
	assetID, ok := dex.BipSymbolID(symbol)
	if !ok {
		return nil, fmt.Errorf("not id found for %q", symbol)
	}
	networkID := assetID
	if token := asset.TokenInfo(assetID); token != nil {
		networkID = token.ParentID
	}
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, fmt.Errorf("no unit info found for %d", assetID)
	}
	return &bncAssetConfig{
		assetID:          assetID,
		symbol:           symbol,
		coin:             coin,
		network:          strings.ToUpper(dex.BipIDSymbol(networkID)),
		conversionFactor: ui.Conventional.ConversionFactor,
	}, nil
}

func (bnc *Binance) Withdraw(ctx context.Context, symbol string, addr string, val uint64) error {
	assetCfg, err := bncSymbolData(symbol)
	if err != nil {
		return err
	}

	prec := int(math.Round(math.Log10(float64(assetCfg.conversionFactor))))
	amt := float64(val) / float64(assetCfg.conversionFactor)

	v := make(url.Values)
	v.Add("coin", assetCfg.coin)
	// v.Add("withdrawOrderId", someUniqueStringIDThatWeUseForTracking)
	v.Add("network", assetCfg.network)
	v.Add("address", addr)
	// v.Add("addressTag", onlyUsedForCoinsLikeXrpAndXmr)
	v.Add("amount", strconv.FormatFloat(amt, 'f', prec, 64))
	// transactionFeeFlag	BOOLEAN	NO	When making internal transfer, true for returning the fee to the destination account; false for returning the fee back to the departure account. Default false.
	// name	STRING	NO	Description of the address. Space in name should be encoded into %20.
	// walletType	INTEGER	NO	The wallet type for withdraw，0-spot wallet ，1-funding wallet.Default spot wallet
	v.Add("recvWindow", strconv.FormatUint(5000, 10)) // The default is 5000
	v.Add("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	var resp struct {
		ID string `json:"id"`
	}

	return bnc.postAPI(ctx, "/sapi/v1/capital/withdraw/apply", v, nil, true, true, &resp)
}

func (bnc *Binance) Trade(ctx context.Context, baseSymbol, quoteSymbol string, sell bool, rate, qty uint64) error {
	const sideTypeBuy string = "BUY"
	const sideTypeSell string = "SELL"

	side := sideTypeBuy
	if sell {
		side = sideTypeSell
	}

	baseCfg, err := bncSymbolData(baseSymbol)
	if err != nil {
		return fmt.Errorf("error getting symbol data for %s: %w", baseSymbol, err)
	}

	quoteCfg, err := bncSymbolData(quoteSymbol)
	if err != nil {
		return fmt.Errorf("error getting symbol data for %s: %w", quoteSymbol, err)
	}

	slug := baseCfg.coin + quoteCfg.coin

	bconv, qconv := baseCfg.conversionFactor, quoteCfg.conversionFactor
	conventionalRateToAtomic := float64(qconv) / float64(bconv)
	prec := int(math.Round(math.Log10(float64(bconv))))

	price := float64(rate) / calc.RateEncodingFactor / conventionalRateToAtomic
	amt := float64(qty) / float64(bconv)

	v := make(url.Values)
	v.Add("symbol", slug)
	v.Add("side", side)
	v.Add("type", "LIMIT")
	// timeInForce	ENUM	NO
	v.Add("quantity", strconv.FormatFloat(amt, 'f', prec, 64))
	// quoteOrderQty	DECIMAL	NO -- market orders only
	v.Add("price", strconv.FormatFloat(price, 'f', 10, 64)) // 10 should be safe, right?
	// newClientOrderId	STRING	NO	A unique id among open orders. Automatically generated if not sent.
	// stopPrice	DECIMAL	NO	Used with STOP_LOSS, STOP_LOSS_LIMIT, TAKE_PROFIT, and TAKE_PROFIT_LIMIT orders.
	// trailingDelta	LONG	NO	Used with STOP_LOSS, STOP_LOSS_LIMIT, TAKE_PROFIT, and TAKE_PROFIT_LIMIT orders. For more details on SPOT implementation on trailing stops, please refer to Trailing Stop FAQ
	// icebergQty	DECIMAL	NO	Used with LIMIT, STOP_LOSS_LIMIT, and TAKE_PROFIT_LIMIT to create an iceberg order.
	v.Add("newOrderRespType", "ACK") // "FULL", "RESULT"
	// newOrderRespType	ENUM	NO	Set the response JSON. ACK, RESULT, or FULL; MARKET and LIMIT order types default to FULL, all other orders default to ACK.
	v.Add("recvWindow", "5000") // default is 5000
	v.Add("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	// timestamp	LONG	YES

	var resp struct {
		Symbol        string `json:"symbol"`
		OrderID       uint64 `json:"orderId"`
		OrderListID   int    `json:"orderListId"` //Unless OCO, value will be -1
		ClientOrderID string `json:"clientOrderId"`
		TransactTime  int64  `json:"transactTime"`
	}

	return bnc.postAPI(ctx, "/api/v3/order", v, nil, true, true, &resp)
}

func (bnc *Binance) getWithdrawFees(ctx context.Context) error {
	coinsData, err := bnc.GetCoinData(ctx)
	if err != nil {
		return fmt.Errorf("GetCoinData error: %v", err)
	}

	bnc.log.Tracef("coin data retrieved for %d coins", len(coinsData))

	fees := make(map[string]*WithdrawFees)
	for _, nfo := range coinsData {
		symbol := strings.ToLower(nfo.Coin)
		assetID, found := dex.BipSymbolID(symbol)
		if !found {
			continue
		}

		if known := bnc.knownAssets[assetID]; !known {
			continue
		}

		networkSymbol := nfo.Coin
		if ti := asset.TokenInfo(assetID); ti != nil {
			networkSymbol = strings.ToUpper(dex.BipIDSymbol(ti.ParentID))
		}

		var netInfo *BinanceNetworkInfo
		for _, ni := range nfo.NetworkList {
			if ni.Network == networkSymbol {
				netInfo = ni
				break
			}
		}

		if netInfo == nil {
			bnc.log.Errorf("failed to find network info for %s", symbol)
			continue
		}

		fees[symbol] = &WithdrawFees{
			Fee:             netInfo.WithdrawFee,
			IntegerMultiple: netInfo.WithdrawIntegerMultiple,
			Max:             netInfo.WithdrawMax,
			Min:             netInfo.WithdrawMin,
		}
	}

	bnc.log.Tracef("found withdraw fees for %d known assets", len(fees))

	bnc.withdrawFees.Store(fees)
	return nil
}

type BinanceNetworkInfo struct {
	AddressRegex            string  `json:"addressRegex"`
	Coin                    string  `json:"coin"`
	DepositEnable           bool    `json:"depositEnable"`
	IsDefault               bool    `json:"isDefault"`
	MemoRegex               string  `json:"memoRegex"`
	MinConfirm              int     `json:"minConfirm"`
	Name                    string  `json:"name"`
	Network                 string  `json:"network"`
	ResetAddressStatus      bool    `json:"resetAddressStatus"`
	SpecialTips             string  `json:"specialTips"`
	UnLockConfirm           int     `json:"unLockConfirm"`
	WithdrawEnable          bool    `json:"withdrawEnable"`
	WithdrawFee             float64 `json:"withdrawFee,string"`
	WithdrawIntegerMultiple float64 `json:"withdrawIntegerMultiple,string"`
	WithdrawMax             float64 `json:"withdrawMax,string"`
	WithdrawMin             float64 `json:"withdrawMin,string"`
	SameAddress             bool    `json:"sameAddress"`
	EstimatedArrivalTime    int     `json:"estimatedArrivalTime"`
	Busy                    bool    `json:"busy"`
}

type BinanceCoinInfo struct {
	Coin              string                `json:"coin"`
	DepositAllEnable  bool                  `json:"depositAllEnable"`
	Free              float64               `json:"free,string"`
	Freeze            float64               `json:"freeze,string"`
	Ipoable           float64               `json:"ipoable,string"`
	Ipoing            float64               `json:"ipoing,string"`
	IsLegalMoney      bool                  `json:"isLegalMoney"`
	Locked            float64               `json:"locked,string"`
	Name              string                `json:"name"`
	Storage           float64               `json:"storage,string"`
	Trading           bool                  `json:"trading"`
	WithdrawAllEnable bool                  `json:"withdrawAllEnable"`
	Withdrawing       float64               `json:"withdrawing,string"`
	NetworkList       []*BinanceNetworkInfo `json:"networkList"`
}

func (bnc *Binance) GetCoinData(ctx context.Context) (coins []*BinanceCoinInfo, err error) {
	return coins, bnc.getAPI(ctx, "/sapi/v1/capital/config/getall", nil, true, true, &coins)
}

type BinanceOrderBook struct {
	LastUpdateID uint64           `json:"lastUpdateId"`
	Bids         [][2]json.Number `json:"bids"`
	Asks         [][2]json.Number `json:"asks"`
}

func (bnc *Binance) GetOrderBook(ctx context.Context, baseSymbol, quoteSymbol string) (ob *BinanceOrderBook, err error) {
	v := make(url.Values)
	v.Add("symbol", BinanceSlug(baseSymbol, quoteSymbol))
	return ob, bnc.getAPI(ctx, "/api/v3/depth", v, true, true, &ob)
}

func (bnc *Binance) getAPI(ctx context.Context, endpoint string, query url.Values, key, sign bool, thing interface{}) error {
	req, err := bnc.generateRequest(ctx, http.MethodGet, endpoint, query, nil, key, sign)
	if err != nil {
		return fmt.Errorf("generateRequest error: %w", err)
	}
	return bnc.requestInto(req, thing)
}

func (bnc *Binance) postAPI(ctx context.Context, endpoint string, query, form url.Values, key, sign bool, thing interface{}) error {
	req, err := bnc.generateRequest(ctx, http.MethodPost, endpoint, query, form, key, sign)
	if err != nil {
		return fmt.Errorf("generateRequest error: %w", err)
	}
	return bnc.requestInto(req, thing)
}

func (bnc *Binance) requestInto(req *http.Request, thing interface{}) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("httpClient.Do error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http error (%d) %s", resp.StatusCode, resp.Status)
	}

	if thing == nil {
		return nil
	}

	reader := io.LimitReader(resp.Body, 1<<18)
	if err := json.NewDecoder(reader).Decode(thing); err != nil {
		return fmt.Errorf("json Decode error: %w", err)
	}
	return nil
}

func (bnc *Binance) generateRequest(ctx context.Context, method, endpoint string, query, form url.Values, key, sign bool) (*http.Request, error) {
	// if r.header != nil {
	// 	header = r.header.Clone()
	// }
	fullURL := bnc.url + endpoint
	// recvWindow default 5 seconds
	// if r.recvWindow > 0 {
	// 	r.setParam(recvWindowKey, r.recvWindow)
	// }
	if query == nil {
		query = make(url.Values)
	}
	if sign {
		query.Add(bncTimestampKey, strconv.FormatInt(time.Now().UnixMilli(), 10))
	}
	queryString := query.Encode()
	bodyString := form.Encode()
	header := make(http.Header, 2)
	body := bytes.NewBuffer(nil)
	if bodyString != "" {
		header.Set("Content-Type", "application/x-www-form-urlencoded")
		body = bytes.NewBufferString(bodyString)
	}
	if key || sign {
		header.Set("X-MBX-APIKEY", bnc.apiKey)
	}

	if sign {
		raw := queryString + bodyString
		mac := hmac.New(sha256.New, []byte(bnc.secretKey))
		if _, err := mac.Write([]byte(raw)); err != nil {
			return nil, fmt.Errorf("hmax Write error: %w", err)
		}
		v := url.Values{}
		v.Set(bncSignatureKey, hex.EncodeToString(mac.Sum(nil)))
		if queryString == "" {
			queryString = v.Encode()
		} else {
			queryString = fmt.Sprintf("%s&%s", queryString, v.Encode())
		}
	}
	if queryString != "" {
		fullURL = fmt.Sprintf("%s?%s", fullURL, queryString)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("NewRequestWithContext error: %w", err)
	}

	req.Header = header

	return req, nil
}

func (bnc *Binance) getListenID(ctx context.Context) (string, error) {
	var resp struct {
		ListenKey string `json:"listenKey"`
	}
	return resp.ListenKey, bnc.postAPI(ctx, "/api/v3/userDataStream", nil, nil, true, false, &resp)
}

type wsBalance struct {
	Asset  string  `json:"a"`
	Free   float64 `json:"f,string"`
	Locked float64 `json:"l,string"`
}

type bncStreamUpdate struct {
	Asset     string `json:"a"`
	EventType string `json:"e"`
	// EventTime     string       `json:"E"`
	Balances []*wsBalance `json:"B"`
	// Free          float64      `json:"f,string"`
	// Locked        float64      `json:"l,string"`
	BalanceDelta float64 `json:"d,string"`
	// ClearTime     int64   `json:"T"`
	// ExecutionType string  `json:"x"`
	// OrderStatus   string  `json"X"`
	// OrderID       uint64  `json"i"`
	// Filled        float64 `json:"z,string"`
	// OrderQty      float64 `json:"q,string"`
	// QuoteQty      float64 `json:"Q,string"`
}

func decodeStreamUpdate(b []byte) (*bncStreamUpdate, error) {
	var msg *bncStreamUpdate
	// Go's json doesn't handle case-sensitivity all that well.
	b = bytes.ReplaceAll(b, []byte(`"E"`), []byte(`"E0"`))
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (bnc *Binance) updateBalance(symbol string, free, locked float64) {
	bnc.balanceMtx.Lock()
	bnc.balances[symbol] = &ExchangeBalance{
		Available: free,
		Locked:    locked,
	}
	bnc.log.Tracef("%s balance updated: %f free %f locked", symbol, free, locked)
	bnc.balanceMtx.Unlock()
}

func (bnc *Binance) getUserDataStream(ctx context.Context) (err error) {
	hdr := make(http.Header)
	hdr.Set("X-MBX-APIKEY", bnc.apiKey)

	streamHandler := func(b []byte) {
		u, err := decodeStreamUpdate(b)
		if err != nil {
			bnc.log.Errorf("error unmarshaling user stream update: %v", err)
			bnc.log.Errorf("raw message: %s", string(b))
			return
		}
		switch u.EventType {
		case "outboundAccountPosition":
			for _, bal := range u.Balances {
				symbol := strings.ToLower(bal.Asset)
				assetID, found := dex.BipSymbolID(symbol)
				if !found {
					return
				}
				if known := bnc.knownAssets[assetID]; !known {
					return
				}
				bnc.updateBalance(symbol, bal.Free, bal.Locked)
			}
		case "balanceUpdate":
			bnc.log.Infof("Balance update: asset: %s delta: %f ", u.Asset, u.BalanceDelta)
		}
	}

	var listenKey string
	newConn := func() (*dex.ConnectionMaster, error) {
		listenKey, err = bnc.getListenID(ctx)
		if err != nil {
			return nil, err
		}

		bnc.log.Debugf("Creating a new Binance wallet stream handler")

		// Need to send key but not signature
		conn, err := comms.NewWsConn(&comms.WsCfg{
			URL: bnc.wsURL + "/ws/" + listenKey,
			// The websocket server will send a ping frame every 3 minutes. If the
			// websocket server does not receive a pong frame back from the
			// connection within a 10 minute period, the connection will be
			// disconnected. Unsolicited pong frames are allowed.
			PingWait: time.Minute * 4,
			// Cert: ,
			ReconnectSync: func() {
				bnc.log.Debugf("Binance reconnected")
			},
			ConnectEventFunc: func(cs comms.ConnectionStatus) {},
			Logger:           bnc.log.SubLogger("BNCWS"),
			RawHandler:       streamHandler,
			ConnectHeaders:   hdr,
		})
		if err != nil {
			return nil, err
		}

		cm := dex.NewConnectionMaster(conn)
		if err = cm.ConnectOnce(ctx); err != nil {
			return nil, fmt.Errorf("websocketHandler remote connect: %v", err)
		}

		return cm, nil
	}

	cm, err := newConn()
	if err != nil {
		return fmt.Errorf("error initializing connection: %v", err)
	}

	bnc.wg.Add(1)
	go func() {
		defer bnc.wg.Done()
		// A single connection to stream.binance.com is only valid for 24 hours;
		// expect to be disconnected at the 24 hour mark.
		reconnect := time.After(time.Hour * 12)
		// Keepalive a user data stream to prevent a time out. User data streams
		// will close after 60 minutes. It's recommended to send a ping about
		// every 30 minutes.
		keepAlive := time.NewTicker(time.Minute * 30)
		for {
			select {
			case <-reconnect:
				if cm != nil {
					cm.Disconnect()
				}
				cm, err = newConn()
				if err != nil {
					bnc.log.Errorf("error reconnecting: %v", err)
					reconnect = time.After(time.Second * 30)
				} else {
					reconnect = time.After(time.Hour * 12)
				}
			case <-keepAlive.C:
				q := make(url.Values)
				q.Add("listenKey", listenKey)
				// Doing a POST on an account with an active listenKey will
				// return the currently active listenKey and extend its validity
				// for 60 minutes.
				req, err := bnc.generateRequest(ctx, http.MethodPut, "/api/v3/userDataStream", q, nil, true, false)
				if err != nil {
					bnc.log.Errorf("error generating keep-alive request: %v", err)
					continue
				}
				if err := bnc.requestInto(req, nil); err != nil {
					bnc.log.Errorf("error sending keep-alive request: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

var subscribeID uint64

type bncBookNote struct {
	StreamName string            `json:"stream"`
	Data       *bncBookBinUpdate `json:"data"`
}

type bncBookBinUpdate struct {
	LastUpdateID uint64           `json:"lastUpdateId"`
	Bids         [][2]json.Number `json:"bids"`
	Asks         [][2]json.Number `json:"asks"`
}

func bncParseBookUpdates(pts [][2]json.Number) ([]*BookBin, error) {
	bins := make([]*BookBin, 0, len(pts))
	for _, nums := range pts {
		price, err := nums[0].Float64()
		if err != nil {
			return nil, fmt.Errorf("error parsing price: %v", err)
		}
		qty, err := nums[0].Float64()
		if err != nil {
			return nil, fmt.Errorf("error quantity price: %v", err)
		}
		bins = append(bins, &BookBin{
			Price: price,
			Qty:   qty,
		})
	}
	return bins, nil
}

func (bnc *Binance) storeMarketStream(conn comms.WsConn) {
	bnc.marketStreamMtx.Lock()
	bnc.marketStream = conn
	bnc.marketStreamMtx.Unlock()
}

func (bnc *Binance) stopMarketDataStream(slug string) (err error) {
	bnc.marketStreamMtx.RLock()
	conn := bnc.marketStream
	bnc.marketStreamMtx.RUnlock()
	if conn == nil {
		return fmt.Errorf("can't unsubscribe. no stream")
	}

	if err := bnc.subUnsubDepth(conn, "UNSUBSCRIBE", slug); err != nil {
		return fmt.Errorf("subUnsubDepth(UNSUBSCRIBE): %v", err)
	}

	return nil
}

func (bnc *Binance) subUnsubDepth(conn comms.WsConn, method, slug string) error {
	req := &struct {
		Method string   `json:"method"`
		Params []string `json:"params"`
		ID     uint64   `json:"id"`
	}{
		Method: method,
		Params: []string{
			slug + "@depth20",
		},
		ID: atomic.AddUint64(&subscribeID, 1),
	}

	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("error marshaling subscription stream request: %w", err)
	}

	if err := conn.SendRaw(b); err != nil {
		return fmt.Errorf("error sending subscription stream request: %w", err)
	}

	return nil
}

func (bnc *Binance) UnsubscribeMarket(baseSymbol, quoteSymbol string) error {
	return bnc.stopMarketDataStream(BinanceSlug(baseSymbol, quoteSymbol))
}

func (bnc *Binance) SubscribeMarket(ctx context.Context, baseSymbol, quoteSymbol string, lotSize uint64) error {
	return bnc.startMarketDataStream(ctx, baseSymbol, quoteSymbol, lotSize)
}

func (bnc *Binance) VWAP(baseSymbol, quoteSymbol string, sell bool, qty float64) (avgPrice, extrema float64, filled bool, err error) {
	slug := BinanceSlug(baseSymbol, quoteSymbol)
	var side []*BookBin
	bnc.booksMtx.RLock()
	book, found := bnc.books[slug]
	if found {
		if sell {
			side = book.asks
		} else {
			side = book.bids
		}
	}
	bnc.booksMtx.RUnlock()
	if side == nil {
		return 0, 0, false, fmt.Errorf("no book found for %s", slug)
	}

	p, extrema, err := vwap(qty, side)
	if err != nil {
		return 0, 0, false, nil
	}

	return p, extrema, true, nil
}

func vwap(vol float64, side []*BookBin) (vwap, extrema float64, err error) {
	remaining := vol
	var filled bool
	var sum, weightedSum, weight float64
	for _, bin := range side {
		extrema = bin.Price
		if bin.Qty >= remaining {
			sum += remaining
			filled = true
			weightedSum += remaining * bin.Price
			weight += remaining
			break
		}
		sum += bin.Qty
		remaining -= bin.Qty
		weightedSum += bin.Qty * bin.Price
		weight += bin.Qty
	}

	if !filled {
		return 0, 0, fmt.Errorf("not enough book to calculate vwap. %f < %f", sum, vol)
	}

	if weight == 0 {
		return 0, 0, fmt.Errorf("empty order book")
	}

	return weightedSum / weight, extrema, nil
}

func (bnc *Binance) startMarketDataStream(ctx context.Context, baseSymbol, quoteSymbol string, lotSize uint64) (err error) {
	slug := BinanceSlug(baseSymbol, quoteSymbol)

	bnc.marketStreamMtx.Lock()
	defer bnc.marketStreamMtx.Unlock()

	// If we already have a market stream, things are easier.
	if bnc.marketStream != nil {
		// Store the book record before subbing, so that any updates received
		// before we get our book can be cached.
		bnc.booksMtx.Lock()
		book, err := newBNCBook(baseSymbol, quoteSymbol, lotSize)
		if err == nil {
			bnc.books[slug] = book
		}
		bnc.booksMtx.Unlock()
		if err != nil {
			return err
		}

		if err := bnc.subUnsubDepth(bnc.marketStream, "SUBSCRIBE", slug); err != nil {
			return fmt.Errorf("subUnsubDepth(SUBSCRIBE): %v", err)
		}
		return nil
	}

	// No connection yet. Start it now.
	streamHandler := func(b []byte) {
		var note *bncBookNote
		if err := json.Unmarshal(b, &note); err != nil {
			bnc.log.Errorf("error unmarshaling book note: %v", err)
			return
		}

		if note == nil || note.Data == nil {
			bnc.log.Debugf("no data in %q update", slug)
			return
		}

		bnc.log.Tracef("received %d bids and %d asks in a book update for %s", len(note.Data.Bids), len(note.Data.Asks), slug)

		parts := strings.Split(note.StreamName, "@")
		if len(parts) != 2 || parts[1] != "depth20" {
			bnc.log.Errorf("unknown stream name %q", note.StreamName)
			return
		}
		slug = parts[0] // will be lower-case

		bids, err := bncParseBookUpdates(note.Data.Bids)
		if err != nil {
			bnc.log.Errorf("error parsing bid updates: %v", err)
			return
		}

		asks, err := bncParseBookUpdates(note.Data.Asks)
		if err != nil {
			bnc.log.Errorf("error parsing ask updates: %v", err)
			return
		}

		bnc.booksMtx.Lock()
		defer bnc.booksMtx.Unlock()
		book := bnc.books[slug]
		if book == nil {
			bnc.log.Errorf("no book for stream %q", slug)
			return
		}

		book.asks = asks
		book.bids = bids
		book.updated = time.Now()

		if bnc.log.Level() == dex.LevelTrace {
			sellVWAP, _, err := vwap(book.lotSizeConventional, asks)
			if err != nil {
				bnc.log.Tracef("(%s) Error calculating sell VWAP: %v", slug, err)
			} else {
				bnc.log.Tracef("(%s) Sell VWAP for 1 lot's worth: %f", slug, sellVWAP)
			}
			buyVWAP, _, err := vwap(book.lotSizeConventional, bids)
			if err != nil {
				bnc.log.Tracef("(%s) Error calculating buy VWAP: %v", slug, err)
			} else {
				bnc.log.Tracef("(%s) Buy VWAP for 1 lot's worth: %f", slug, buyVWAP)
			}
		}
	}

	newConn := func() (comms.WsConn, *dex.ConnectionMaster, error) {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		bnc.log.Debugf("Creating a new Binance market stream handler")
		// Get a list of current subscriptions so we can restart them all.
		bnc.booksMtx.Lock()
		subs := make([]string, 1, len(bnc.books)+1)
		slugs := make([]string, 1, len(bnc.books)+1)
		// We'll use the <slug>@depth20 endpoint, which just sends the best 20
		// order bins from each side of the market every second. This is more
		// bandwidth (roughly 100 MB per day per market), but requires much less
		// storage and juggling to keep the market synced.
		subs[0] = slug + "@depth20"
		slugs[0] = slug
		for s := range bnc.books {
			if s != slug {
				subs = append(subs, s+"@depth20")
			}
			slugs = append(slugs, s)
		}
		for _, s := range slugs {
			book, err := newBNCBook(baseSymbol, quoteSymbol, lotSize)
			if err != nil {
				bnc.log.Errorf("error creating bnc book: %v", err)
				continue
			}
			bnc.books[s] = book
		}
		bnc.booksMtx.Unlock()

		addr := fmt.Sprintf("%s/stream?streams=%s", bnc.wsURL, strings.Join(subs, "/"))

		// Need to send key but not signature
		conn, err := comms.NewWsConn(&comms.WsCfg{
			URL: addr,
			// The websocket server will send a ping frame every 3 minutes. If the
			// websocket server does not receive a pong frame back from the
			// connection within a 10 minute period, the connection will be
			// disconnected. Unsolicited pong frames are allowed.
			PingWait: time.Minute * 4,
			// Cert: ,
			ReconnectSync: func() {
				bnc.log.Debugf("Binance reconnected")
			},
			ConnectEventFunc: func(cs comms.ConnectionStatus) {},
			Logger:           bnc.log.SubLogger("BNCBOOK"),
			RawHandler:       streamHandler,
			// ConnectHeaders:   hdr,
		})
		if err != nil {
			return nil, nil, err
		}

		cm := dex.NewConnectionMaster(conn)
		if err = cm.ConnectOnce(ctx); err != nil {
			return nil, nil, fmt.Errorf("websocketHandler remote connect: %v", err)
		}

		return conn, cm, nil
	}

	conn, cm, err := newConn()
	if err != nil {
		return fmt.Errorf("error initializing connection: %v", err)
	}
	bnc.marketStream = conn

	bnc.wg.Add(1)
	go func() {
		defer bnc.wg.Done()

		defer func() {
			bnc.storeMarketStream(nil)
			if cm != nil {
				cm.Disconnect()
			}
		}()
		reconnect := time.After(time.Hour * 12)
		for {
			select {
			case <-reconnect:
				if cm != nil {
					cm.Disconnect()
				}
				conn, cm, err = newConn()
				if err != nil {
					bnc.log.Errorf("error reconnecting: %v", err)
					reconnect = time.After(time.Second * 30)
				} else {
					bnc.storeMarketStream(conn)
					reconnect = time.After(time.Hour * 12)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

func BinanceSlug(baseSymbol, quoteSymbol string) string {
	return fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
}
