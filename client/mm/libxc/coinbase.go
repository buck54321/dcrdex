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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
)

// https://docs.cloud.coinbase.com/advanced-trade-api/docs/

var supportedNetworkTokens = map[uint32]struct{}{
	60001: {}, // USDC on ETH
}

type cbBook struct {
	mtx   sync.RWMutex
	buys  [][2]uint64
	sells [][2]uint64
	bui   *dex.UnitInfo
	qui   *dex.UnitInfo
}

type cbMarket struct {
	*coinbaseMarket
	bui *dex.UnitInfo
	qui *dex.UnitInfo
}

type cbOrder struct {
	*coinbaseOrder
	orderID string
}

type coinbase struct {
	log       dex.Logger
	ctx       context.Context
	basePath  string
	apiKey    string
	secret    []byte
	tickerIDs map[string][]uint32
	idTickers map[uint32][]string

	connMtx sync.RWMutex
	conn    comms.WsConn
	connCM  *dex.ConnectionMaster

	booksMtx sync.RWMutex
	books    map[string]*cbBook

	assetsMtx sync.RWMutex
	assets    map[string]*coinbaseAsset

	marketsMtx sync.RWMutex
	markets    map[string]*cbMarket

	cexUpdatersMtx sync.RWMutex
	cexUpdaters    map[chan interface{}]struct{}

	ordersMtx sync.RWMutex
	orders    map[string]*cbOrder

	tradeUpdaterMtx    sync.RWMutex
	tradeToUpdater     map[string]int
	tradeUpdaters      map[int]chan *Trade
	tradeUpdateCounter int

	tradeIDNonce atomic.Uint32

	seq atomic.Uint64
}

var _ CEX = (*coinbase)(nil)

func newCoinbase(apiKey, secret string, log dex.Logger, net dex.Network) (*coinbase, error) {
	var basePath string
	switch net {
	case dex.Mainnet:
		basePath = "https://api.coinbase.com/api/v3/brokerage/"
	default:
		// DRAFT TODO: Future home of fake coinbase simnet server.
		basePath = "http://127.0.0.1:23885"
	}

	tickerIDs := make(map[string][]uint32)
	idTickers := make(map[uint32][]string)

	addTicker := func(assetID uint32, ticker string) {
		// Add both the ticker and the "normalized ticker", e.g. USD for USDC.
		// The normalized ticker is what we'll use in messaging.
		cbTicker := normalizeTicker(ticker)
		if ticker != cbTicker {
			tickerIDs[cbTicker] = append(tickerIDs[cbTicker], assetID)
			idTickers[assetID] = append(idTickers[assetID], cbTicker)
		}
		tickerIDs[ticker] = append(tickerIDs[ticker], assetID)
		idTickers[assetID] = append(idTickers[assetID], ticker)

	}

	for _, a := range asset.Assets() {
		addTicker(a.ID, a.Info.UnitInfo.Conventional.Unit)
		for tokenID, tkn := range a.Tokens {
			if _, supported := supportedNetworkTokens[tokenID]; !supported {
				continue
			}
			addTicker(tokenID, tkn.UnitInfo.Conventional.Unit)
		}
	}
	c := &coinbase{
		log:         log,
		basePath:    basePath,
		apiKey:      apiKey,
		secret:      []byte(secret),
		tickerIDs:   tickerIDs,
		idTickers:   idTickers,
		books:       make(map[string]*cbBook),
		assets:      make(map[string]*coinbaseAsset),
		markets:     make(map[string]*cbMarket),
		cexUpdaters: make(map[chan interface{}]struct{}),
		orders:      make(map[string]*cbOrder),
	}

	return c, nil
}

func (c *coinbase) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// Get our balances.
	err := c.updateAssets()
	if err != nil {
		return nil, fmt.Errorf("error fetching accounts: %w", err)
	}

	if err = c.updateMarkets(); err != nil {
		return nil, fmt.Errorf("error fetching markets: %w", err)
	}

	c.ctx = ctx
	if err := c.connectWebsockets(); err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		<-ctx.Done()
		wg.Done()
		c.closeWebsockets()
	}()

	// DRAFT TODO: Subscribe to trade updates.

	return &wg, nil
}

// Balance returns the balance of an asset at the CEX.
func (c *coinbase) Balance(assetID uint32) (*ExchangeBalance, error) {
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, fmt.Errorf("error getting unit info for asset ID %d", assetID)
	}
	symbol := dex.BipIDSymbol(assetID)

	tickers := c.idTickers[assetID]
	if len(tickers) == 0 {
		return nil, fmt.Errorf("no known tickers for %s", symbol)
	}
	var b ExchangeBalance
	for _, ticker := range tickers {
		a, err := c.getAsset(ticker)
		if err != nil {
			return nil, fmt.Errorf("error fetching account info for ticker %s", ticker)
		}
		b.Available += toAtomic(a.AvailableBalance.Value, &ui)
		b.Locked += toAtomic(a.Hold.Value, &ui)
	}
	return &b, nil
}

// Balances returns a list of all asset balances at the CEX. Only assets that are
// registered in the DEX client will be returned.
func (c *coinbase) Balances() (map[uint32]*ExchangeBalance, error) {
	bs := make(map[uint32]*ExchangeBalance, len(c.idTickers))
	for assetID, tickers := range c.idTickers {
		ticker := tickers[0]
		c.assetsMtx.RLock()
		_, supported := c.assets[ticker]
		c.assetsMtx.RUnlock()
		if !supported {
			continue
		}
		b, err := c.Balance(assetID)
		if err != nil {
			return nil, fmt.Errorf("error fetching balance for asset ID %d: %v", assetID, err)
		}
		bs[assetID] = b
	}
	return bs, nil
}

// CancelTrade cancels a trade on the CEX.
func (c *coinbase) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	c.ordersMtx.Lock()
	defer c.ordersMtx.Unlock()
	ord, found := c.orders[tradeID]
	if !found {
		return errors.New("unknown trade ID")
	}
	orderIDs := []string{ord.orderID}
	var res struct {
		Results []struct {
			Success       bool   `json:"success"`
			FailureReason string `json:"failure_reason"`
			OrderID       string `json:"order_id"`
		} `json:"results"`
	}
	if err := c.request(http.MethodPost, "orders/batch_cancel", orderIDs, &res); err != nil {
		return fmt.Errorf("error cancelling order: %w", err)
	}
	if len(res.Results) != 1 {
		return fmt.Errorf("expected 1 cancellation result, got %d", len(res.Results))
	}
	if !res.Results[0].Success {
		return fmt.Errorf("cancellation failed: %s", res.Results[0].FailureReason)
	}
	delete(c.orders, tradeID)
	return nil
}

// Markets returns the list of markets at the CEX.
func (c *coinbase) Markets() ([]*Market, error) {
	markets := make([]*Market, 0)
	added := make(map[[2]uint32]bool)
	c.marketsMtx.RLock()
	defer c.marketsMtx.RUnlock()
	for _, m := range c.markets {
		baseIDs := c.tickerIDs[normalizeTicker(m.BaseCurrencyID)]
		quoteIDs := c.tickerIDs[normalizeTicker(m.QuoteCurrencyID)]
		for _, baseID := range baseIDs {
			for _, quoteID := range quoteIDs {
				mktID := [2]uint32{baseID, quoteID}
				if added[mktID] {
					continue
				}
				added[mktID] = true
				markets = append(markets, &Market{
					BaseID:  baseID,
					QuoteID: quoteID,
				})
			}
		}
	}
	return markets, nil
}

// SubscribeCEXUpdates returns a channel which sends an empty struct when
// the balance of an asset on the CEX has been updated.
func (c *coinbase) SubscribeCEXUpdates() (<-chan interface{}, func()) {
	updater := make(chan interface{}, 128)
	c.cexUpdatersMtx.Lock()
	c.cexUpdaters[updater] = struct{}{}
	c.cexUpdatersMtx.Unlock()

	unsubscribe := func() {
		c.cexUpdatersMtx.Lock()
		delete(c.cexUpdaters, updater)
		c.cexUpdatersMtx.Unlock()
	}

	return updater, unsubscribe
}

// SubscribeMarket subscribes to order book updates on a market. This must
// be called before calling VWAP.
func (c *coinbase) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	return c.subscribeOrderbook(baseID, quoteID)
}

// SubscribeTradeUpdates returns a channel that the caller can use to
// listen for updates to a trade's status. When the subscription ID
// returned from this function is passed as the updaterID argument to
// Trade, then updates to the trade will be sent on the updated channel
// returned from this function.
func (c *coinbase) SubscribeTradeUpdates() (<-chan *Trade, func(), int) {
	c.tradeUpdaterMtx.Lock()
	defer c.tradeUpdaterMtx.Unlock()
	updaterID := c.tradeUpdateCounter
	c.tradeUpdateCounter++
	updater := make(chan *Trade, 256)
	c.tradeUpdaters[updaterID] = updater

	unsubscribe := func() {
		c.tradeUpdaterMtx.Lock()
		delete(c.tradeUpdaters, updaterID)
		c.tradeUpdaterMtx.Unlock()
	}

	return updater, unsubscribe, updaterID
}

type coinbaseOrder struct {
	ClientOrderID      string `json:"client_order_id"`
	ProductID          string `json:"product_id"`
	Side               string `json:"side"` // "BUY" or "SELL"
	OrderConfiguration struct {
		Limit struct {
			BaseSize   float64 `json:"base_size,string"`
			LimitPrice float64 `json:"limit_price,string"`
			PostOnly   bool    `json:"post_only"`
		} `json:"limit_limit_gtc"`
	} `json:"order_configuration"`
}

func (c *coinbase) generateTradeID() string {
	nonce := c.tradeIDNonce.Add(1)
	nonceB := encode.Uint32Bytes(nonce)
	return hex.EncodeToString(nonceB)
}

// Trade executes a trade on the CEX. updaterID takes a subscriptionID
// returned from SubscribeTradeUpdates.
func (c *coinbase) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, subscriptionID int) (string, error) {
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return "", fmt.Errorf("error generating product ID: %w", err)
	}
	c.marketsMtx.RLock()
	mkt, found := c.markets[productID]
	c.marketsMtx.RUnlock()
	if !found {
		return "", fmt.Errorf("no book found for %s", productID)
	}
	side := "BUY"
	if sell {
		side = "SELL"
	}
	tradeID := c.generateTradeID()

	lotSize := toAtomic(mkt.BaseIncrement, mkt.bui)

	lots := uint64(math.Round(float64(qty) / float64(lotSize)))
	qty = lots * lotSize

	rateStep := calc.MessageRateAlt(mkt.PriceIncrement, mkt.bui.Conventional.ConversionFactor, mkt.qui.Conventional.ConversionFactor)
	steps := uint64(math.Round(float64(rate) / float64(rateStep)))
	rate = steps * rateStep
	convRate := float64(rate) / calc.RateEncodingFactor * float64(mkt.bui.Conventional.ConversionFactor) / float64(mkt.qui.Conventional.ConversionFactor)

	ord := &coinbaseOrder{
		ProductID:     productID,
		Side:          side,
		ClientOrderID: tradeID,
	}
	limitConfig := &ord.OrderConfiguration.Limit
	limitConfig.BaseSize = float64(qty) / float64(mkt.bui.Conventional.ConversionFactor)
	limitConfig.LimitPrice = convRate

	var res struct {
		Success         bool   `json:"success"`
		FailureReason   string `json:"failure_reason"`
		OrderID         string `json:"order_id"`
		SuccessResponse struct {
			OrderID       string `json:"order_id"`
			ProductID     string `json:"product_id"`
			Side          string `json:"side"`
			ClientOrderID string `json:"client_order_id"`
		} `json:"success_response"`
		ErrorResponse struct {
			Error                 string `json:"error"`
			Message               string `json:"message"`
			ErrorDetails          string `json:"error_details"`
			PreviewFailureReason  string `json:"preview_failure_reason"`
			NewOrderFailureReason string `json:"new_order_failure_reason"`
		} `json:"error_response"`
		OrderConfiguration struct {
			Limit struct {
				BaseSize   float64 `json:"base_size,string"`
				LimitPrice float64 `json:"limit_price,string"`
				PostOnly   bool    `json:"post_only"`
			} `json:"limit_limit_gtc"`
		}
		// `json:"limit_limit_gtd"``
		// `json:"stop_limit_stop_limit_gtc"``
		// `json:"stop_limit_stop_limit_gtd"``
	}
	if err := c.request(http.MethodPost, "orders", ord, &res); err != nil {
		return "", fmt.Errorf("error posting order: %w", err)
	}

	if !res.Success {
		e := &res.ErrorResponse
		c.log.Errorf("Error placing order: %q, %q, %q, %q, %q",
			res.FailureReason, e.Error, e.Message, e.ErrorDetails, e.NewOrderFailureReason)
		return "", fmt.Errorf("error placing order: %s", res.FailureReason)
	}

	limit := &res.OrderConfiguration.Limit
	placedQty := toAtomic(limit.BaseSize, mkt.bui)
	placedRate := calc.MessageRateAlt(limit.LimitPrice, mkt.bui.Conventional.ConversionFactor, mkt.qui.Conventional.ConversionFactor)
	if placedQty != qty || placedRate != rate {
		c.log.Errorf("Order response does not match our parameters. ordered qty = %d, rate = %d, responded with qty = %d, rate = %d",
			qty, placedQty, rate, placedRate)
		if err := c.CancelTrade(ctx, baseID, quoteID, tradeID); err != nil {
			c.log.Errorf("Error canceling invalid order: %v", err)
		}
		return "", errors.New("wrong order echoed")
	}

	c.ordersMtx.Lock()
	c.orders[tradeID] = &cbOrder{
		coinbaseOrder: ord,
		orderID:       res.OrderID,
	}
	c.ordersMtx.Unlock()

	return tradeID, nil
}

// UnsubscribeMarket unsubscribes from order book updates on a market.
func (c *coinbase) UnsubscribeMarket(baseID, quoteID uint32) {
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		c.log.Errorf("unsubscribe product ID error: %v", err)
		return
	}
	if err := c.subUnsub("unsubscribe", "level2", []string{productID}); err != nil {
		c.log.Errorf("Error unsubscribing from %s: %v", productID, err)
	}
}

// VWAP returns the volume weighted average price for a certain quantity
// of the base asset on a market.
func (c *coinbase) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	fail := func(s string, a ...interface{}) (uint64, uint64, bool, error) {
		return 0, 0, false, fmt.Errorf(s, a...)
	}
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return fail("error generating product ID: %v", err)
	}
	c.booksMtx.RLock()
	book, found := c.books[productID]
	c.booksMtx.RUnlock()
	if !found {
		return fail("no book found for %s", productID)
	}

	var side [][2]uint64
	if sell {
		side = book.sells
	} else {
		side = book.buys
	}

	remaining := qty
	var weightedSum uint64
	n := len(side)
	for i := range side {
		j := i
		if !sell { // buys iterate from the back
			j = n - i - 1
		}
		bin := side[j]
		r, qty := bin[0], bin[1]
		extrema = r
		if qty >= remaining {
			filled = true
			weightedSum += remaining * extrema
			break
		}
		remaining -= qty
		weightedSum += qty * extrema
	}

	if !filled {
		return 0, 0, false, nil
	}

	return weightedSum / qty, extrema, true, nil
}

func (c *coinbase) connectWebsockets() error {
	const wsURL = "wss://advanced-trade-ws.coinbase.com"
	c.connMtx.Lock()
	defer c.connMtx.Unlock()
	conn, err := comms.NewWsConn(&comms.WsCfg{
		URL: wsURL,
		// The websocket server will send a ping frame every 3 minutes. If the
		// websocket server does not receive a pong frame back from the
		// connection within a 10 minute period, the connection will be
		// disconnected. Unsolicited pong frames are allowed.
		PingWait: time.Minute * 4,
		ReconnectSync: func() {
			fmt.Println("--reconnected")
		},
		ConnectEventFunc: func(cs comms.ConnectionStatus) {},
		Logger:           c.log.SubLogger("CBBOOK"),
		RawHandler:       c.handleWebsocketMessage,
	})
	if err != nil {
		return fmt.Errorf("error creating WsConn: %w", err)
	}
	cm := dex.NewConnectionMaster(conn)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		return fmt.Errorf("error connecting to websocket feed: %w", err)
	}
	c.conn = conn
	c.connCM = cm
	return nil
}

func (c *coinbase) closeWebsockets() {
	c.connMtx.RLock()
	cm := c.connCM
	c.conn, c.connCM = nil, nil
	c.connMtx.RUnlock()
	if cm == nil {
		return
	}
	cm.Disconnect()
	cm.Wait()
}

func (c *coinbase) withWS(f func(comms.WsConn) error) error {
	c.connMtx.RLock()
	conn := c.conn
	c.connMtx.RUnlock()
	if conn == nil {
		return errors.New("no websockets connection")
	}
	return f(conn)
}

type coinbaseWebsocketSubscription struct {
	Type       string   `json:"type"`
	ProductIDs []string `json:"product_ids"`
	Channel    string   `json:"channel"`
	Signature  string   `json:"signature"`
	ApiKey     string   `json:"api_key"`
	Timestamp  string   `json:"timestamp"`
}

func normalizeTicker(ticker string) string {
	switch ticker {
	case "USDC": // USDC and USD are treated identically by Coinbase, it seems.
		return "USD"
	}
	return ticker
}

// func denormalizeTicker(ticker string) string {
// 	switch ticker {
// 	case "USD":
// 		return "USDC"
// 	}
// 	return ticker
// }

func (c *coinbase) newProductID(baseID, quoteID uint32) (string, error) {
	baseTickers, found := c.idTickers[baseID]
	if !found {
		return "", fmt.Errorf("ticker not found for base asset ID %d", baseID)
	}
	quoteTickers, found := c.idTickers[quoteID]
	if !found {
		return "", fmt.Errorf("ticker not found for quote asset ID %d", baseID)
	}
	return normalizeTicker(baseTickers[0]) + "-" + normalizeTicker(quoteTickers[0]), nil
}

func (c *coinbase) subscribeOrderbook(baseID, quoteID uint32) error {
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return err
	}
	c.marketsMtx.RLock()
	_, found := c.markets[productID]
	c.marketsMtx.RUnlock()
	if !found {
		return fmt.Errorf("no market found for product ID %s", productID)
	}
	bui, err := asset.UnitInfo(baseID)
	if err != nil {
		return fmt.Errorf("error getting unit info for base asset ID %d: %v", baseID, err)
	}
	qui, err := asset.UnitInfo(quoteID)
	if err != nil {
		return fmt.Errorf("error getting unit info for quote asset ID %d: %v", quoteID, err)
	}
	c.booksMtx.Lock()
	_, exists := c.books[productID]
	if exists {
		c.booksMtx.Unlock()
		return fmt.Errorf("order book for %s already exists", productID)
	}
	c.books[productID] = &cbBook{
		buys:  make([][2]uint64, 0),
		sells: make([][2]uint64, 0),
		bui:   &bui,
		qui:   &qui,
	}
	c.booksMtx.Unlock()
	return c.subUnsub("subscribe", "level2", []string{productID})

}

func (c *coinbase) subUnsub(subunsub, channel string, productIDs []string) error {
	stamp := time.Now().Unix()
	stampStr := strconv.FormatUint(uint64(stamp), 10)
	preimage := stampStr + channel + strings.Join(productIDs, ",")
	sig, err := c.sign(preimage)
	if err != nil {
		return err
	}

	reqB, err := json.Marshal(&coinbaseWebsocketSubscription{
		Type:       subunsub,
		ProductIDs: productIDs,
		Channel:    channel,
		Signature:  sig,
		ApiKey:     c.apiKey,
		Timestamp:  stampStr,
	})
	if err != nil {
		return fmt.Errorf("error marshaling subscription: %v", err)
	}
	return c.withWS(func(conn comms.WsConn) error {
		return conn.SendRaw(reqB)
	})
}

func (c *coinbase) handleWebsocketMessage(b []byte) {
	var errMsg struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &errMsg); err == nil && errMsg.Type == "error" {
		c.log.Errorf("Websocket error: %s", errMsg.Message)
		c.closeWebsockets()
		return
	}
	var channelProbe struct {
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(b, &channelProbe); err != nil || channelProbe.Channel == "" {
		c.log.Errorf("Error parsing websocket message channel: channel = %q, err = %v", channelProbe.Channel, err)
		return
	}
	switch channelProbe.Channel {
	case "l2_data":
		c.handleLevel2Message(b)
	case "subscriptions":
		c.handleSubscriptionMessage(b)
	default:
		c.log.Errorf("message received for unknown channel %q: %s", channelProbe.Channel, string(b))
	}
}

func (c *coinbase) handleSubscriptionMessage(b []byte) {
	var msg struct {
		Channel     string    `json:"channel"`
		ClientID    string    `json:"client_id"`
		Timestamp   time.Time `json:"timestamp"`
		SequenceNum uint64    `json:"sequence_num"`
		Events      []struct {
			Subscriptions map[string][]string `json:"subscriptions"`
		} `json:"events"`
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		c.log.Errorf("Error unmarshaling level2 message: %v", err)
		return
	}
	lastSeq := c.seq.Swap(msg.SequenceNum)
	if lastSeq != 0 && lastSeq != msg.SequenceNum-1 {
		c.log.Errorf("subscriptions message out of sequence. %d -> %d", lastSeq, msg.SequenceNum)
		return
	}
	if len(msg.Events) != 1 {
		c.log.Errorf("subscriptions message received with %d events", len(msg.Events))
		return
	}
	productIDs := c.subscribedMarkets()
	if n := len(msg.Events[0].Subscriptions["level2"]); n == 0 && len(productIDs) > 0 {
		c.log.Debugf("resubscribing to %d markets", len(productIDs))
		if err := c.subUnsub("subscribe", "level2", productIDs); err != nil {
			c.log.Errorf("Error resubscribing to level2 feed: %v", err)
		}
	}
}

func (c *coinbase) handleLevel2Message(b []byte) {
	var msg struct {
		Channel     string    `json:"channel"`
		ClientID    string    `json:"client_id"`
		Timestamp   time.Time `json:"timestamp"`
		SequenceNum uint64    `json:"sequence_num"`
		Events      []*struct {
			Type      string `json:"type"`
			ProductID string `json:"product_id"`
			Updates   []*struct {
				Side        string    `json:"side"` // "bid" or "offer"
				EventTime   time.Time `json:"event_time"`
				PriceLevel  float64   `json:"price_level,string"`
				NewQuantity float64   `json:"new_quantity,string"`
			} `json:"updates"`
		} `json:"events"`
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		c.log.Errorf("Error unmarshaling level2 message: %v", err)
		return
	}
	lastSeq := c.seq.Swap(msg.SequenceNum)
	if lastSeq != 0 && lastSeq != msg.SequenceNum-1 {
		c.log.Errorf("level2 message out of sequence. %d -> %d", lastSeq, msg.SequenceNum)
		c.refreshLevel2()
		return
	}
	for _, ev := range msg.Events {
		c.booksMtx.RLock()
		book, found := c.books[ev.ProductID]
		c.booksMtx.RUnlock()
		if !found {
			c.log.Errorf("book update received for unknown order book %q", ev.ProductID)
			return
		}
		book.mtx.Lock()
		switch ev.Type {
		case "snapshot":
			book.buys = make([][2]uint64, 0)
			book.sells = make([][2]uint64, 0)
			for _, u := range ev.Updates {
				v := toAtomic(u.NewQuantity, book.bui)
				r := messageRate(u.PriceLevel, book.bui, book.qui)
				switch u.Side {
				case "bid":
					book.buys = append(book.buys, [2]uint64{r, v})
				case "offer":
					book.sells = append(book.buys, [2]uint64{r, v})
				}
			}
			sort.Slice(book.buys, func(i, j int) bool {
				return book.buys[i][0] < book.buys[j][0]
			})
			sort.Slice(book.sells, func(i, j int) bool {
				return book.sells[i][0] < book.sells[j][0]
			})
		case "update":
			for _, u := range ev.Updates {
				// We'll pull the side out as a separate variable to make our
				// job easier. We just need to remember to re-assign it later.
				// Both sides are sorted lowest to highest in this
				// implementation.
				side := book.sells
				if u.Side == "bid" {
					side = book.buys
				}
				v := toAtomic(u.NewQuantity, book.bui)
				r := messageRate(u.PriceLevel, book.bui, book.qui)

				if found, i := bookBindex(side, r); found {
					// Update
					side[i][1] = v
				} else {
					// New bin. Append for now. Will sort later.
					side = append(side, [2]uint64{r, v})
				}

				// Clean up zeros.
				n := len(side)
				var deleted int
				for i := range side {
					if side[i][1] == 0 {
						deleted++
						continue
					}
					if deleted > 0 {
						side[i-deleted] = side[i]
					}
				}
				side = side[:n-deleted]

				// Sort. This should be fairly quick, since the slice is mostly
				// sorted already, just with new bins at the end.
				sort.Slice(side, func(i, j int) bool {
					return side[i][0] < side[j][0]
				})

				// Re-assign the side to the book.
				switch u.Side {
				case "bid":
					book.buys = side
				case "offer":
					book.sells = side
				}
			}
		default:
			c.log.Errorf("level 2 message has unknown message type %q", ev.Type)
		}
		fmt.Printf("--book updated. %d buys and %d sells\n", len(book.buys), len(book.sells))
		book.mtx.Unlock()
	}
}

func bookBindex(side [][2]uint64, msgRate uint64) (bool, int) {
	n := len(side)
	i := sort.Search(n, func(i int) bool {
		return side[i][0] >= msgRate
	})
	if i == n {
		return false, -1
	}
	if side[i][0] == msgRate {
		return true, i
	}
	return false, i
}

func (c *coinbase) subscribedMarkets() []string {
	c.booksMtx.RLock()
	defer c.booksMtx.RUnlock()
	productIDs := make([]string, 0, len(c.books))
	for productID := range c.books {
		productIDs = append(productIDs, productID)
	}
	return productIDs
}

func (c *coinbase) refreshLevel2() {
	productIDs := c.subscribedMarkets()
	c.log.Debugf("Unsubscribing from %+v \n", productIDs)
	if err := c.subUnsub("unsubscribe", "level2", productIDs); err != nil {
		c.log.Errorf("Error unsubscribing from level2: %v", err)
		return
	}
	// Will resubscribe in handleSubscriptionMessage.
}

func (c *coinbase) request(method, endpoint string, params, res interface{}) error {
	req, cancel, err := c.prepareRequest(method, endpoint, params)
	if err != nil {
		return fmt.Errorf("prepareRequest error: %v", err)
	}
	defer cancel()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error requesting %q: %v", req.URL, err)
	}
	defer resp.Body.Close()

	if res != nil {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("error reading response body: %v", err)
		}
		if err := json.Unmarshal(b, res); err != nil {
			return fmt.Errorf("error unmarshaling response: %v", err)
		}
	}
	return nil
}

func (c *coinbase) prepareRequest(method, endpoint string, params interface{}) (_ *http.Request, _ context.CancelFunc, err error) {
	stamp := time.Now().Unix()
	stampString := strconv.FormatUint(uint64(stamp), 10)

	var body []byte
	if params != nil {
		body, err = json.Marshal(params)
		if err != nil {
			return nil, nil, fmt.Errorf("error marshaling request: %w", err)
		}
	}

	preimage := stampString + method + "/api/v3/brokerage/" + endpoint + string(body)
	sig, err := c.sign(preimage)
	if err != nil {
		return nil, nil, err
	}

	const timeout = time.Second * 30
	ctx, cancel := context.WithTimeout(c.ctx, timeout)

	req, err := http.NewRequestWithContext(ctx, method, c.basePath+endpoint, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("error generating http request: %w", err)
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("CB-ACCESS-KEY", c.apiKey)
	req.Header.Set("CB-ACCESS-SIGN", sig)
	req.Header.Set("CB-ACCESS-TIMESTAMP", stampString)
	return req, cancel, nil
}

func (c *coinbase) sign(preimage string) (string, error) {
	mac := hmac.New(sha256.New, c.secret)
	_, err := mac.Write([]byte(preimage))
	if err != nil {
		return "", fmt.Errorf("error writing preimage: %w", err)
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

type coinbaseAssetBalance struct {
	Value    float64 `json:"value,string"`
	Currency string  `json:"currency"`
}

type coinbaseAsset struct {
	UUID             string               `json:"uuid"`
	Name             string               `json:"name"`
	Currency         string               `json:"currency"`
	AvailableBalance coinbaseAssetBalance `json:"available_balance"`
	Default          bool                 `json:"default"`
	Active           bool                 `json:"active"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
	DeletedAt        *time.Time           `json:"deleted_at"`
	Type             string               `json:"type"`
	Ready            bool                 `json:"ready"`
	Hold             coinbaseAssetBalance `json:"hold"`
}

type coinbaseAccountsResult struct {
	Assets []*coinbaseAsset `json:"accounts"`
}

func (c *coinbase) updateAssets() error {
	var res coinbaseAccountsResult
	if err := c.request(http.MethodGet, "accounts", nil, &res); err != nil {
		return err
	}
	c.assetsMtx.Lock()
	// Should delete existing assets first?
	for _, a := range res.Assets {
		if _, supported := c.tickerIDs[a.Currency]; supported {
			c.assets[a.Currency] = a
		}
	}
	c.assetsMtx.Unlock()
	return nil
}

type coinbaseAccountResult struct {
	Asset coinbaseAsset `json:"account"`
}

func (c *coinbase) getAsset(ticker string) (*coinbaseAsset, error) {
	c.assetsMtx.RLock()
	a, found := c.assets[ticker]
	c.assetsMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("no account found for ticker %s", ticker)
	}
	var res coinbaseAccountResult
	return &res.Asset, c.request(http.MethodGet, "accounts/"+a.UUID, nil, &res)
}

type coinbaseMarket struct {
	ProductID                string  `json:"product_id"`
	Price                    float64 `json:"price,string"`
	DayPriceChangePctStr     string  `json:"price_percentage_change_24h"`
	Volume                   string  `json:"volume_24h"`
	DayVolumeChangePctStr    string  `json:"volume_percentage_change_24h"`
	BaseIncrement            float64 `json:"base_increment,string"`
	QuoteIncrement           float64 `json:"quote_increment,string"`
	QuoteMinSize             float64 `json:"quote_min_size,string"`
	QuoteMaxSize             float64 `json:"quote_max_size,string"`
	BaseMinSize              float64 `json:"base_min_size,string"`
	BaseMaxSize              float64 `json:"base_max_size,string"`
	BaseName                 string  `json:"base_name"`
	QuoteName                string  `json:"quote_name"`
	Watched                  bool    `json:"watched"`
	IsDisabled               bool    `json:"is_disabled"`
	New                      bool    `json:"new"`
	Status                   string  `json:"status"`
	CancelOnly               bool    `json:"cancel_only"`
	LimitOnly                bool    `json:"limit_only"`
	PostOnly                 bool    `json:"post_only"`
	TradingDisabled          bool    `json:"trading_disabled"`
	AuctionMode              bool    `json:"auction_mode"`
	ProductType              string  `json:"product_type"`
	QuoteCurrencyID          string  `json:"quote_currency_id"`
	BaseCurrencyID           string  `json:"base_currency_id"`
	FCMTradingSessionDetails struct {
		IsSessionOpen bool      `json:"is_session_open"`
		OpenTime      time.Time `json:"open_time"`
		CloseTime     time.Time `json:"close_time"`
	} `json:"fcm_trading_session_details"`
	MidMarketPrice     string   `json:"mid_market_price"`
	Alias              string   `json:"alias"`
	AliastTo           []string `json:"alias_to"`
	BaseDisplaySymbol  string   `json:"base_display_symbol"`
	QuoteDisplaySymbol string   `json:"quote_display_symbol"`
	ViewOnly           bool     `json:"view_only"`
	PriceIncrement     float64  `json:"price_increment,string"`
	// FutureProductDetails struct { ... } `json:"future_product_details"`
}

type coinbaseProductsResult struct {
	Markets []*coinbaseMarket `json:"products"`
}

func (c *coinbase) updateMarkets() error {
	var res coinbaseProductsResult
	if err := c.request(http.MethodGet, "products", nil, &res); err != nil {
		return err
	}
	c.marketsMtx.Lock()
	// Delete first?
	for _, mkt := range res.Markets {
		baseIDs, found := c.tickerIDs[mkt.BaseCurrencyID]
		if !found {
			continue
		}
		bui, err := asset.UnitInfo(baseIDs[0])
		if err != nil {
			return fmt.Errorf("failed to locate unit info for base asset ID %d", baseIDs[0])
		}
		quoteIDs, found := c.tickerIDs[mkt.QuoteCurrencyID]
		if !found {
			continue
		}
		qui, err := asset.UnitInfo(quoteIDs[0])
		if err != nil {
			return fmt.Errorf("failed to locate unit info for quote asset ID %d", quoteIDs[0])
		}
		c.markets[mkt.ProductID] = &cbMarket{
			coinbaseMarket: mkt,
			bui:            &bui,
			qui:            &qui,
		}
	}
	c.marketsMtx.Unlock()
	return nil
}

func (c *coinbase) getMarket(productID string) (product *coinbaseMarket, _ error) {
	return product, c.request(http.MethodGet, "products/"+productID, nil, &product)
}

func toAtomic(v float64, ui *dex.UnitInfo) uint64 {
	return uint64(math.Round(v * float64(ui.Conventional.ConversionFactor)))
}

func messageRate(conventionalRate float64, bui, qui *dex.UnitInfo) uint64 {
	return calc.MessageRateAlt(conventionalRate, bui.Conventional.ConversionFactor, qui.Conventional.ConversionFactor)
}
