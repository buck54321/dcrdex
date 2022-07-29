// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/order"
)

const (
	CEXBotV0 = "CEXBotV0"
)

type BaseBotConfig struct {
	Host       string `json:"host"`
	BaseID     uint32 `json:"baseID"`
	QuoteID    uint32 `json:"quoteID"`
	BookLots   uint64 `json:"bookLots"`
	SettleLots uint64 `json:"settleLots"`
}

func (cfg *BaseBotConfig) host() string {
	return cfg.Host
}

func (cfg *BaseBotConfig) baseID() uint32 {
	return cfg.BaseID
}

func (cfg *BaseBotConfig) quoteID() uint32 {
	return cfg.QuoteID
}

type marketConfig interface {
	host() string
	baseID() uint32
	quoteID() uint32
}

type coreBot interface {
	run(context.Context)
	stop()
	baseBot() *baseMarketBot
	retire()
	updateProgram(pgm marketConfig) error
}

type baseMarketBot struct {
	*Core
	pgmID   uint64
	botType string
	base    *makerAsset
	quote   *makerAsset
	// TODO: enable updating of market, or just grab it live when needed.
	market *Market
	log    dex.Logger
	book   *orderbook.OrderBook

	conventionalRateToAtomic float64

	running uint32
	wg      sync.WaitGroup
	die     context.CancelFunc

	rebalanceRunning uint32

	programV atomic.Value // marketConfig

	ordMtx sync.RWMutex
	ords   map[order.OrderID]*Order
}

func newBaseMarketBot(ctx context.Context, botType string, c *Core, pgm marketConfig) (*baseMarketBot, error) {
	// if err := validateProgram(pgm); err != nil {
	// 	return nil, err
	// }
	host, baseID, quoteID := pgm.host(), pgm.baseID(), pgm.quoteID()

	dc, err := c.connectedDEX(host)
	if err != nil {
		return nil, err
	}
	xcInfo := dc.exchangeInfo()
	supportedAssets := c.assetMap()

	if _, ok := supportedAssets[baseID]; !ok {
		return nil, fmt.Errorf("base asset %d (%s) not supported", baseID, unbip(baseID))
	}
	if _, ok := supportedAssets[quoteID]; !ok {
		return nil, fmt.Errorf("quote asset %d (%s) not supported", quoteID, unbip(quoteID))
	}

	baseAsset, found := xcInfo.Assets[baseID]
	if !found {
		return nil, fmt.Errorf("no base asset %d -> %s info", baseID, unbip(baseID))
	}
	quoteAsset, found := xcInfo.Assets[quoteID]
	if !found {
		return nil, fmt.Errorf("no quote asset %d -> %s info", quoteID, unbip(quoteID))
	}
	baseWallet := c.WalletState(baseID)
	if baseWallet == nil {
		return nil, fmt.Errorf("no wallet found for base asset %d -> %s", baseID, unbip(baseID))
	}
	quoteWallet := c.WalletState(quoteID)
	if quoteWallet == nil {
		return nil, fmt.Errorf("no wallet found for quote asset %d -> %s", quoteID, unbip(quoteID))
	}

	mktName := marketName(baseID, quoteID)
	mkt := xcInfo.Markets[mktName]
	if mkt == nil {
		return nil, fmt.Errorf("market %s not known at %s", mktName, host)
	}

	base := &makerAsset{
		Asset: baseAsset,
		Name:  supportedAssets[baseID].Info.Name, // TODO: Fix this after tokens is merged.
	}
	base.balanceV.Store(baseWallet.Balance)

	quote := &makerAsset{
		Asset: quoteAsset,
		Name:  supportedAssets[quoteID].Info.Name, // TODO: Fix this after tokens is merged.
	}
	quote.balanceV.Store(quoteWallet.Balance)

	bconv, qconv := base.UnitInfo.Conventional.ConversionFactor, quote.UnitInfo.Conventional.ConversionFactor
	m := &baseMarketBot{
		Core:                     c,
		botType:                  botType,
		base:                     base,
		quote:                    quote,
		market:                   mkt,
		conventionalRateToAtomic: float64(qconv) / float64(bconv),
		// oracle:    oracle,
		log:  c.log.SubLogger(fmt.Sprintf("BOT.%s", marketName(baseID, quoteID))),
		ords: make(map[order.OrderID]*Order),
	}
	m.programV.Store(pgm)

	// // Get a rate now, because max buy won't have an oracle fallback.
	// var rate uint64
	// if mkt.SpotPrice != nil {
	// 	rate = mkt.SpotPrice.Rate
	// }
	// if rate == 0 {
	// 	price, err := m.syncOraclePrice(ctx)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("failed to establish a starting price: %v", err)
	// 	}
	// 	rate = uint64(math.Round(price * m.conventionalRateToAtomic * calc.RateEncodingFactor))
	// }

	// baseAvail := baseWallet.Balance.Available / mkt.LotSize
	// quoteAvail := quoteWallet.Balance.Available /
	// var maxBuyLots, maxSellLots uint64
	// maxBuy, err := c.MaxBuy(pgm.Host, pgm.BaseID, pgm.QuoteID, rate)
	// if err == nil {
	// 	maxBuyLots = maxBuy.Swap.Lots
	// }
	// maxSell, err := c.MaxSell(pgm.Host, pgm.BaseID, pgm.QuoteID)
	// if err == nil {
	// 	maxSellLots = maxSell.Swap.Lots
	// }

	// if maxBuyLots+maxSellLots < pgm.Lots*2 {
	// 	return nil, fmt.Errorf("cannot create bot with %d lots. 2 x %d = %d lots total balance required to start, "+
	// 		"and only %d %s lots and %d %s lots = %d total lots are available", pgm.Lots, pgm.Lots, pgm.Lots*2,
	// 		maxBuyLots, unbip(pgm.QuoteID), maxSellLots, unbip(pgm.BaseID), maxSellLots+maxBuyLots)
	// }

	return m, nil
}

type baseBotNoteHandlers struct {
	book func(*BookUpdate)
	core func(Notification)
}

func (m *baseMarketBot) run(ctx context.Context, h *baseBotNoteHandlers) {
	if !atomic.CompareAndSwapUint32(&m.running, 0, 1) {
		m.log.Errorf("run called while makerBot already running")
		return
	}
	defer atomic.StoreUint32(&m.running, 0)

	ctx, m.die = context.WithCancel(ctx)
	defer m.die()

	pgm := m.program()

	book, bookFeed, err := m.syncBook(pgm.host(), pgm.baseID(), pgm.quoteID())
	if err != nil {
		m.log.Errorf("Error establishing book feed: %v", err)
		return
	}
	m.book = book

	// if pgm.OracleWeighting != nil {
	// 	m.startOracleSync(ctx)
	// }

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer bookFeed.Close()
		for {
			select {
			case n := <-bookFeed.Next():
				h.book(n)
			case <-ctx.Done():
				return
			}
		}
	}()

	cid, notes := m.notificationFeed()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { m.notify(newBotNote(TopicBotStopped, "", "", db.Data, m.report())) }()
		defer atomic.StoreUint32(&m.running, 0)
		defer m.returnFeed(cid)
		for {
			select {
			case n := <-notes:
				h.core(n)
			case <-ctx.Done():
				return
			}
		}
	}()

	m.notify(newBotNote(TopicBotStarted, "", "", db.Data, m.report()))

	m.wg.Wait()
}

func (m *baseMarketBot) program() marketConfig {
	return m.programV.Load().(marketConfig)
}

func (m *baseMarketBot) liveOrderIDs() []order.OrderID {
	m.ordMtx.RLock()
	oids := make([]order.OrderID, 0, len(m.ords))
	for oid, ord := range m.ords {
		if ord.Status <= order.OrderStatusBooked {
			oids = append(oids, oid)
		}
	}
	m.ordMtx.RUnlock()
	return oids
}

func (m *baseMarketBot) retire() {
	m.stop()
	if err := m.db.RetireBotProgram(m.pgmID); err != nil {
		m.log.Errorf("error retiring bot program")
	} else {
		m.notify(newBotNote(TopicBotRetired, "", "", db.Data, m.report()))
	}
}

// stop stops the bot and cancels all live orders. Note that stop is not called
// on *Core shutdown, so existing bot orders remain live if shutdown is forced.
func (m *baseMarketBot) stop() {
	if m.die != nil {
		m.die()
		m.wg.Wait()
	}
	for _, oid := range m.liveOrderIDs() {
		if err := m.cancelOrder(oid); err != nil {
			m.log.Errorf("error cancelling order %s while stopping bot %d: %v", oid, m.pgmID, err)
		}
	}

	// TODO: Should really wait until the cancels match. If we're cancelling
	// orders placed in the same epoch, they might miss and need to be replaced.
}

func (m *baseMarketBot) botOrders() []*BotOrder {
	m.ordMtx.RLock()
	defer m.ordMtx.RUnlock()
	ords := make([]*BotOrder, 0, len(m.ords))
	for _, ord := range m.ords {
		ords = append(ords, &BotOrder{
			Host:     ord.Host,
			MarketID: ord.MarketID,
			OrderID:  ord.ID,
			Status:   ord.Status,
		})
	}
	return ords
}

func (m *baseMarketBot) dbRecord() (*db.BotProgram, error) {
	pgm := m.program()
	pgmB, err := json.Marshal(pgm)
	if err != nil {
		return nil, err
	}
	return &db.BotProgram{
		Type:    MakerBotV0,
		Program: pgmB,
	}, nil
}

func (m *baseMarketBot) saveProgram() (uint64, error) {
	dbRecord, err := m.dbRecord()
	if err != nil {
		return 0, err
	}
	return m.db.SaveBotProgram(dbRecord)
}

func (m *baseMarketBot) updateProgram(pgm marketConfig) error {
	dbRecord, err := m.dbRecord()
	if err != nil {
		return err
	}
	if err := m.db.UpdateBotProgram(m.pgmID, dbRecord); err != nil {
		return err
	}

	// if pgm.oracleWeighting() > 0 {
	// 	m.startOracleSync(m.ctx) // no-op if sync is already running
	// }

	m.programV.Store(pgm)
	m.notify(newBotNote(TopicBotUpdated, "", "", db.Data, m.report()))
	return nil
}

func (m *baseMarketBot) report() *BotReport {
	return &BotReport{
		ProgramID: m.pgmID,
		BotType:   m.botType,
		Program:   m.program(),
		Running:   atomic.LoadUint32(&m.running) == 1,
		Orders:    m.botOrders(),
	}
}

func (m *baseMarketBot) handleNote(ctx context.Context, note Notification) {
	switch n := note.(type) {
	case *BalanceNote:
		switch n.AssetID {
		case m.market.BaseID:
			m.log.Tracef("%s balance updated: %+v", unbip(m.market.BaseID), n.Balance.Balance.Balance /* lol */)
			m.base.storeBalance(n.Balance)
		case m.market.QuoteID:
			m.log.Tracef("%s balance updated: %+v", unbip(m.market.QuoteID), n.Balance.Balance.Balance)
			m.quote.storeBalance(n.Balance)
		}
	}
}

// placeOrder places a single order on the market.
func (m *baseMarketBot) placeOrder(lots, rate uint64, sell bool) *Order {
	pgm := m.program()
	ord, err := m.Trade(nil, &TradeForm{
		Host:    pgm.host(),
		IsLimit: true,
		Sell:    sell,
		Base:    pgm.baseID(),
		Quote:   pgm.quoteID(),
		Qty:     lots * m.market.LotSize,
		Rate:    rate,
		Program: m.pgmID,
	})
	if err != nil {
		m.log.Errorf("Error placing rebalancing order: %v", err)
		return nil
	}
	var oid order.OrderID
	copy(oid[:], ord.ID)
	m.ordMtx.Lock()
	m.ords[oid] = ord
	m.ordMtx.Unlock()
	return ord
}

func (m *baseMarketBot) msgRateToConventional(msgRate uint64) float64 {
	return float64(msgRate) / calc.RateEncodingFactor / m.conventionalRateToAtomic
}

func (m *baseMarketBot) conventionalRateToMsg(price float64) uint64 {
	return uint64(math.Round(price * m.conventionalRateToAtomic * calc.RateEncodingFactor))
}

func (m *baseMarketBot) baseBot() *baseMarketBot {
	return m
}

// sortedOrder is a subset of an *Order used internally for sorting.
type sortedOrder struct {
	*Order
	id   order.OrderID
	rate uint64
	lots uint64
}

// sortedOrders returns lists of buy and sell orders, with buys sorted
// high to low by rate, and sells low to high.
func (m *baseMarketBot) sortedOrders() (buys, sells []*sortedOrder) {
	makeSortedOrder := func(o *Order) *sortedOrder {
		var oid order.OrderID
		copy(oid[:], o.ID)
		return &sortedOrder{
			Order: o,
			id:    oid,
			rate:  o.Rate,
			lots:  (o.Qty - o.Filled) / m.market.LotSize,
		}
	}

	buys, sells = make([]*sortedOrder, 0), make([]*sortedOrder, 0)
	m.ordMtx.RLock()
	for _, ord := range m.ords {
		if ord.Sell {
			sells = append(sells, makeSortedOrder(ord))
		} else {
			buys = append(buys, makeSortedOrder(ord))
		}
	}
	m.ordMtx.RUnlock()

	sort.Slice(buys, func(i, j int) bool { return buys[i].rate > buys[j].rate })
	sort.Slice(sells, func(i, j int) bool { return sells[i].rate < sells[j].rate })

	return buys, sells
}

// feeEstimates calculates the swap and redeem fees on an order. If the wallet's
// PreSwap/PreRedeem method cannot provide a value (because no balance, likely),
// and the Wallet implements BotWallet, then the estimate from
// SingleLotSwapFees/SingleLotRedeemFees will be used.
func (m *baseMarketBot) feeEstimates(form *TradeForm) (swapFees, redeemFees uint64, err error) {
	dc, connected, err := m.dex(form.Host)
	if err != nil {
		return 0, 0, err
	}
	if !connected {
		return 0, 0, errors.New("dex not connected")
	}

	baseWallet, found := m.wallet(m.market.BaseID)
	if !found {
		return 0, 0, fmt.Errorf("no base wallet found")
	}

	quoteWallet, found := m.wallet(m.market.QuoteID)
	if !found {
		return 0, 0, fmt.Errorf("no quote wallet found")
	}

	fromWallet, toWallet := quoteWallet, baseWallet
	if form.Sell {
		fromWallet, toWallet = baseWallet, quoteWallet
	}

	swapFeeSuggestion := m.feeSuggestion(dc, fromWallet.AssetID)
	if swapFeeSuggestion == 0 {
		return 0, 0, fmt.Errorf("failed to get swap fee suggestion for %s at %s", unbip(fromWallet.AssetID), form.Host)
	}

	redeemFeeSuggestion := m.feeSuggestionAny(toWallet.AssetID)
	if redeemFeeSuggestion == 0 {
		return 0, 0, fmt.Errorf("failed to get redeem fee suggestion for %s at %s", unbip(toWallet.AssetID), form.Host)
	}

	lotSize := m.market.LotSize
	lots := form.Qty / lotSize
	rate := form.Rate

	swapLotSize := m.market.LotSize
	if !form.Sell {
		swapLotSize = calc.BaseToQuote(rate, m.market.LotSize)
	}

	xcInfo := dc.exchangeInfo()
	fromAsset := xcInfo.Assets[fromWallet.AssetID]
	toAsset := xcInfo.Assets[toWallet.AssetID]

	preSwapForm := &asset.PreSwapForm{
		LotSize:         swapLotSize,
		Lots:            lots,
		AssetConfig:     fromAsset,
		RedeemConfig:    toAsset,
		Immediate:       (form.IsLimit && form.TifNow),
		FeeSuggestion:   swapFeeSuggestion,
		SelectedOptions: form.Options,
	}

	swapEstimate, err := fromWallet.PreSwap(preSwapForm)
	if err == nil {
		swapFees = swapEstimate.Estimate.RealisticWorstCase
	} else {
		// Maybe they offer a single-lot estimate.
		if bw, is := fromWallet.Wallet.(asset.BotWallet); is {
			var err2 error
			swapFees, err2 = bw.SingleLotSwapFees(preSwapForm)
			if err2 != nil {
				return 0, 0, fmt.Errorf("error getting swap estimate (%v) and single-lot estimate (%v)", err, err2)
			}
		} else {
			return 0, 0, fmt.Errorf("error getting swap estimate: %w", err)
		}
	}

	preRedeemForm := &asset.PreRedeemForm{
		Lots:            lots,
		FeeSuggestion:   redeemFeeSuggestion,
		SelectedOptions: form.Options,
		AssetConfig:     toAsset,
	}
	redeemEstimate, err := toWallet.PreRedeem(preRedeemForm)
	if err == nil {
		redeemFees = redeemEstimate.Estimate.RealisticWorstCase
	} else {
		if bw, is := fromWallet.Wallet.(asset.BotWallet); is {
			var err2 error
			redeemFees, err2 = bw.SingleLotRedeemFees(preRedeemForm)
			if err2 != nil {
				return 0, 0, fmt.Errorf("error getting redemption estimate (%v) and single-lot estimate (%v)", err, err2)
			}
		} else {
			return 0, 0, fmt.Errorf("error getting redemption estimate: %v", err)
		}
	}
	return
}

// prepareBotWallets unlocks the wallets that marketBot uses.
func (c *Core) prepareBotWallets(crypter encrypt.Crypter, baseID, quoteID uint32) error {
	baseWallet, found := c.wallet(baseID)
	if !found {
		return fmt.Errorf("no base %s wallet", unbip(baseID))
	}

	if !baseWallet.unlocked() {
		if err := c.connectAndUnlock(crypter, baseWallet); err != nil {
			return fmt.Errorf("failed to unlock base %s wallet: %v", unbip(baseID), err)
		}
	}

	quoteWallet, found := c.wallet(quoteID)
	if !found {
		return fmt.Errorf("no quote %s wallet", unbip(quoteID))
	}

	if !quoteWallet.unlocked() {
		if err := c.connectAndUnlock(crypter, quoteWallet); err != nil {
			return fmt.Errorf("failed to unlock quote %s wallet: %v", unbip(quoteID), err)
		}
	}

	return nil
}

func (c *Core) findBot(pgmID uint64, del bool) coreBot {
	c.mm.Lock()
	mmBot := c.mm.bots[pgmID]
	if mmBot != nil {
		if del {
			delete(c.mm.bots, pgmID)
		}
		c.mm.Unlock()
		return mmBot
	}
	c.mm.Unlock()
	c.cex.Lock()
	defer c.cex.Unlock()
	cexBot := c.cex.bots[pgmID]
	if cexBot != nil {
		if del {
			delete(c.cex.bots, pgmID)
		}
		return cexBot
	}
	return nil
}

// CreateBot creates a market-maker bot.
func (c *Core) CreateBot(pw []byte, botType string, pgm json.RawMessage) (uint64, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return 0, codedError(passwordErr, err)
	}

	var baseCfg BaseBotConfig
	if err := json.Unmarshal(pgm, &baseCfg); err != nil {
		return 0, err
	}

	if err := c.prepareBotWallets(crypter, baseCfg.BaseID, baseCfg.QuoteID); err != nil {
		return 0, err
	}

	switch botType {
	case MakerBotV0:
		var makerPgm MakerProgram
		if err := json.Unmarshal(pgm, &makerPgm); err != nil {
			return 0, err
		}
		return c.createMakerBot(&makerPgm)
	case ArberV0:
		var cexPgm CEXBotProgram
		if err := json.Unmarshal(pgm, &cexPgm); err != nil {
			return 0, err
		}
		return c.createCEXBot(&cexPgm)
	default:
		return 0, fmt.Errorf("bot type %q not known", botType)
	}
}

func (c *Core) createMakerBot(pgm *MakerProgram) (uint64, error) {
	bot, err := newMakerBot(c.ctx, c, pgm)
	if err != nil {
		return 0, err
	}
	c.mm.Lock()
	c.mm.bots[bot.pgmID] = bot
	c.mm.Unlock()

	c.wg.Add(1)
	go func() {
		bot.run(c.ctx)
		c.wg.Done()
	}()
	return bot.pgmID, nil
}

// StartBot starts an existing market-maker bot.
func (c *Core) StartBot(pw []byte, pgmID uint64) error {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return codedError(passwordErr, err)
	}

	bot := c.findBot(pgmID, false)
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}

	bb := bot.baseBot()

	if atomic.LoadUint32(&bb.running) == 1 {
		c.log.Warnf("Ignoring attempt to start an alread-running bot for %s", bb.market.Name)
		return nil
	}

	if err := c.prepareBotWallets(crypter, bb.base.ID, bb.quote.ID); err != nil {
		return err
	}

	c.wg.Add(1)
	go func() {
		bot.run(c.ctx)
		c.wg.Done()
	}()
	return nil
}

// StopBot stops a running market-maker bot. Stopping the bot cancels all
// existing orders.
func (c *Core) StopBot(pgmID uint64) error {
	bot := c.findBot(pgmID, false)
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}
	bot.stop()
	return nil
}

// UpdateBotProgram updates the program of an existing market-maker bot.
func (c *Core) UpdateBotProgram(pgmID uint64, pgm json.RawMessage) error {
	c.mm.RLock()
	mmBot := c.mm.bots[pgmID]
	c.mm.RUnlock()
	if mmBot != nil {
		var mmPgm MakerProgram
		if err := json.Unmarshal(pgm, &mmPgm); err != nil {
			return err
		}
		mmBot.updateMakerProgram(&mmPgm)
	}
	c.cex.RLock()
	cexBot := c.cex.bots[pgmID]
	c.cex.RUnlock()
	if cexBot == nil {
		return fmt.Errorf("no bot found with id %d", pgmID)
	}
	var cexPgm CEXBotProgram
	if err := json.Unmarshal(pgm, &cexPgm); err != nil {
		return err
	}
	cexBot.updateCEXProgram(&cexPgm)
	return nil
}

// RetireBot stops a bot and deletes its program from the database.
func (c *Core) RetireBot(pgmID uint64) error {
	bot := c.findBot(pgmID, true)
	if bot == nil {
		return fmt.Errorf("no bot found with id %d", pgmID)
	}
	bot.retire()
	return nil
}

// loadBotPrograms is used as part of the startup routine run during Login.
// Loads all existing programs from the database and matches bots with
// existing orders.
func (c *Core) loadBotPrograms() error {
	botPrograms, err := c.db.ActiveBotPrograms()
	if err != nil {
		return err
	}
	if len(botPrograms) == 0 {
		return nil
	}
	botTrades := make(map[uint64][]*Order)
	for _, dc := range c.dexConnections() {
		for _, t := range dc.trackedTrades() {
			if t.metaData.ProgramID > 0 {
				botTrades[t.metaData.ProgramID] = append(botTrades[t.metaData.ProgramID], t.coreOrder())
			}
		}
	}
	c.mm.Lock()
	defer c.mm.Unlock()
	for pgmID, botPgm := range botPrograms {
		if c.mm.bots[pgmID] != nil {
			continue
		}
		var makerPgm *MakerProgram
		if err := json.Unmarshal(botPgm.Program, &makerPgm); err != nil {
			c.log.Errorf("Error decoding maker program: %v", err)
			continue
		}
		bot, err := recreateMakerBot(c.ctx, c, pgmID, makerPgm)
		if err != nil {
			c.log.Errorf("Error recreating maker bot: %v", err)
			continue
		}
		for _, ord := range botTrades[pgmID] {
			var oid order.OrderID
			copy(oid[:], ord.ID)
			bot.ords[oid] = ord
		}
		c.mm.bots[pgmID] = bot
	}
	return nil
}

// bots returns a list of BotReport for all existing bots.
func (c *Core) bots() []*BotReport {
	c.mm.RLock()
	defer c.mm.RUnlock()
	bots := make([]*BotReport, 0, len(c.mm.bots))
	for _, bot := range c.mm.bots {
		bots = append(bots, bot.report())
	}
	// Sort them newest first.
	sort.Slice(bots, func(i, j int) bool { return bots[i].ProgramID > bots[j].ProgramID })
	return bots
}
