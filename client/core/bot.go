// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/order"
)

const (
	// MakerBotV0 is the bot specifier associated with makerBot. The bot
	// specifier is used to determine how to decode stored program data.
	MakerBotV0 = "MakerV0"

	ErrNoMarkets           = dex.ErrorKind("no markets")
	defaultOracleWeighting = 0.2
)

// type SpreadMode string

// const (
// 	SpreadModeAuto     SpreadMode = "auto"
// 	SpreadModeAbsolute SpreadMode = "absolute"
// 	SpreadModeRatio    SpreadMode = "percent"
// )

// type SpreadLayer struct {
// 	// Mode determines how the ideal spread is determined.
// 	Mode SpreadMode `json:"mode"`
// 	// Value's meaning depends on the Mode.
// 	//   SpreadModeAuto: Value is read as a +/- adjustment to the break-even
// 	//     spread value. Value is restricted to +/-1.
// 	//   SpreadModeAbsolute: Value is interpreted as a a gap width in rate
// 	//     units, e.g. sell.Price - buy.Price.
// 	//   SpreadModeRatio: Value is read as a ratio of the mid-gap rate. For
// 	//	  example, if the mid-gap rate is 50, and Value is set to 0.001, then
// 	//    the target spread will be a 0.05.
// 	Value float64 `json:"value"`
// }

// MakerProgram is the program for a makerBot.
type MakerProgram struct {
	Host    string `json:"host"`
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`

	// Lots is the number of lots to allocate to each side of the market. This
	// is an ideal allotment, but at any given time, a side could have up to
	// 2 * Lots on order.
	Lots uint64 `json:"lots"`

	// // MaxFundedLots is the maximum number of lots (per-side) that can be
	// // either on order or in active matches up until redemption. The default
	// // behavior is to keep Lots lots on each side of the book (up to 2x on one
	// // side in an unbalanced situation), regardless of how many are settling.
	// // This could lead to many times Lots lots being commited at a given time.
	// // If set, MaxFundedLots must be >= Lots.
	// MaxFundedLots uint64 `json:"maxFundedLots"`

	// SpreadMultiplier will increase the spread over the break-even value.
	// Valid range 1 < SpreadMultiplier < 10, with 1 corresponding to the
	// break-even spread. A higher SpreadMultiplier has higher potential for
	// profits, but also increased risk of loss.
	SpreadMultiplier float64 `json:"spreadMultiplier"`

	// DriftTolerance is how far away from an ideal price an order can drift
	// before it will replaced (units: ratio of price). Default 0.1%
	DriftTolerance float64 `json:"driftTolerance"`

	// OracleWeighting affects how the target price is derived based on external
	// market data. OracleWeighting, r, determines the target price with the
	// formula:
	//   target_price = dex_mid_gap_price * (1 - r) + oracle_price * r
	// OracleWeighting is limited to 0 <= x <= 1.0.
	// Fetching of price data is disabled if OracleWeighting = 0.
	// OracleWeighting should probably be a *float64 so that we can set a
	// non-zero default value.
	OracleWeighting *float64 `json:"oracleWeighting"`

	// OracleBias applies a bias in the positive (higher price) or negative
	// (lower price) direction.
	OracleBias float64 `json:"oracleBias"`
}

// validateProgram checks the sensibility of a *MakerProgram's values and sets
// some defaults.
func validateProgram(pgm *MakerProgram) error {
	if pgm.Host == "" {
		return errors.New("no host specified")
	}
	if dex.BipIDSymbol(pgm.BaseID) == "" {
		return fmt.Errorf("base asset %d unknown", pgm.BaseID)
	}
	if dex.BipIDSymbol(pgm.QuoteID) == "" {
		return fmt.Errorf("quote asset %d unknown", pgm.QuoteID)
	}
	if pgm.Lots == 0 {
		return errors.New("cannot run with lots = 0")
	}
	if pgm.OracleBias < -0.05 || pgm.OracleBias > 0.05 {
		return fmt.Errorf("bias %f out of bounds", pgm.OracleBias)
	}
	if pgm.OracleWeighting != nil {
		w := *pgm.OracleWeighting
		if w < 0 || w > 1 {
			return fmt.Errorf("oracle weighting %f out of bounds", w)
		}
	}

	if pgm.DriftTolerance == 0 {
		pgm.DriftTolerance = 0.001
	}
	if pgm.DriftTolerance < 0 || pgm.DriftTolerance > 0.01 {
		return fmt.Errorf("drift tolerance %f out of bounds", pgm.DriftTolerance)
	}
	if pgm.SpreadMultiplier == 0 {
		pgm.SpreadMultiplier = 1
	}
	if pgm.SpreadMultiplier < 1 || pgm.SpreadMultiplier > 10 {
		return fmt.Errorf("spread multiplier %f is out of bounds", pgm.SpreadMultiplier)
	}
	return nil
}

// oracleWeighting returns the specified OracleWeighting, or the default if
// not set.
func (pgm *MakerProgram) oracleWeighting() float64 {
	if pgm.OracleWeighting == nil {
		return defaultOracleWeighting
	}
	return *pgm.OracleWeighting
}

// makerAsset combines a *dex.Asset with a WalletState.
type makerAsset struct {
	*dex.Asset
	Name string
	// walletV  atomic.Value // *WalletState
	balanceV atomic.Value // *WalletBalance

}

// balance retrieves the stored balance.
func (m *makerAsset) balance() *WalletBalance {
	return m.balanceV.Load().(*WalletBalance)
}

// storeBalance stores the balance.
func (m *makerAsset) storeBalance(bal *WalletBalance) {
	m.balanceV.Store(bal)
}

// makerBot is a *Core extension that enables operation of a market-maker bot.
// Given an order for L lots, every epoch the makerBot will...
//   1. Calculate a "basis price", which is based on DEX market data,
//      optionally mixed (OracleWeight) with external market data.
//   2. Calculate a "break-even spread". This is the spread at which tx fee
//      losses exactly match profits.
//   3. The break-even spread serves as a hard minimum, and is used to determine
//      the target spread based on the specified SpreadMultiplier, giving us our
//      target buy and sell prices.
//   4. Scan existing orders to determine if their prices are still valid,
//      within DriftTolerance of the buy or sell price. If not, schedule them
//      for cancellation.
//   5. Calculate how many lots are needed to be ordered in order to meet the
//      2 x L commitment. If low balance restricts the maintenance of L lots on
//      one side, allow the difference in lots to be added to the opposite side.
//   6. Place orders, cancels first, then buys and sells.
type makerBot struct {
	*Core
	pgmID uint64
	base  *makerAsset
	quote *makerAsset
	// TODO: enable updating of market, or just grab it live when needed.
	market *Market
	log    dex.Logger
	book   *orderbook.OrderBook

	conventionalRateToAtomic float64

	running uint32
	wg      sync.WaitGroup
	die     context.CancelFunc

	rebalanceRunning uint32

	programV atomic.Value // *MakerProgram

	ordMtx sync.RWMutex
	ords   map[order.OrderID]*Order

	oracleRunning uint32
}

func createMakerBot(ctx context.Context, c *Core, pgm *MakerProgram) (*makerBot, error) {
	if err := validateProgram(pgm); err != nil {
		return nil, err
	}
	dc, err := c.connectedDEX(pgm.Host)
	if err != nil {
		return nil, err
	}
	xcInfo := dc.exchangeInfo()
	supportedAssets := c.assetMap()

	if _, ok := supportedAssets[pgm.BaseID]; !ok {
		return nil, fmt.Errorf("base asset %d (%s) not supported", pgm.BaseID, unbip(pgm.BaseID))
	}
	if _, ok := supportedAssets[pgm.QuoteID]; !ok {
		return nil, fmt.Errorf("quote asset %d (%s) not supported", pgm.QuoteID, unbip(pgm.QuoteID))
	}

	baseAsset, found := xcInfo.Assets[pgm.BaseID]
	if !found {
		return nil, fmt.Errorf("no base asset %d -> %s info", pgm.BaseID, unbip(pgm.BaseID))
	}
	quoteAsset, found := xcInfo.Assets[pgm.QuoteID]
	if !found {
		return nil, fmt.Errorf("no quote asset %d -> %s info", pgm.QuoteID, unbip(pgm.QuoteID))
	}
	baseWallet := c.WalletState(pgm.BaseID)
	if baseWallet == nil {
		return nil, fmt.Errorf("no wallet found for base asset %d -> %s", pgm.BaseID, unbip(pgm.BaseID))
	}
	quoteWallet := c.WalletState(pgm.QuoteID)
	if quoteWallet == nil {
		return nil, fmt.Errorf("no wallet found for quote asset %d -> %s", pgm.QuoteID, unbip(pgm.QuoteID))
	}

	mktName := marketName(pgm.BaseID, pgm.QuoteID)
	mkt := xcInfo.Markets[mktName]
	if mkt == nil {
		return nil, fmt.Errorf("market %s not known at %s", mktName, pgm.Host)
	}

	base := &makerAsset{
		Asset: baseAsset,
		Name:  supportedAssets[pgm.BaseID].Info.Name, // TODO: Fix this after tokens is merged.
	}
	base.balanceV.Store(baseWallet.Balance)

	quote := &makerAsset{
		Asset: quoteAsset,
		Name:  supportedAssets[pgm.QuoteID].Info.Name, // TODO: Fix this after tokens is merged.
	}
	quote.balanceV.Store(quoteWallet.Balance)

	bconv, qconv := base.UnitInfo.Conventional.ConversionFactor, quote.UnitInfo.Conventional.ConversionFactor
	m := &makerBot{
		Core:                     c,
		base:                     base,
		quote:                    quote,
		market:                   mkt,
		conventionalRateToAtomic: float64(qconv) / float64(bconv),
		// oracle:    oracle,
		log:  c.log.SubLogger(fmt.Sprintf("BOT.%s", marketName(pgm.BaseID, pgm.QuoteID))),
		ords: make(map[order.OrderID]*Order),
	}
	m.programV.Store(pgm)

	// Get a rate now, because max buy won't have an oracle fallback.
	var rate uint64
	if mkt.SpotPrice != nil {
		rate = mkt.SpotPrice.Rate
	}
	if rate == 0 {
		price, err := m.syncOraclePrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to establish a starting price: %v", err)
		}
		rate = uint64(math.Round(price * m.conventionalRateToAtomic * calc.RateEncodingFactor))
	}

	// baseAvail := baseWallet.Balance.Available / mkt.LotSize
	// quoteAvail := quoteWallet.Balance.Available /
	var maxBuyLots, maxSellLots uint64
	maxBuy, err := c.MaxBuy(pgm.Host, pgm.BaseID, pgm.QuoteID, rate)
	if err == nil {
		maxBuyLots = maxBuy.Swap.Lots
	}
	maxSell, err := c.MaxSell(pgm.Host, pgm.BaseID, pgm.QuoteID)
	if err == nil {
		maxSellLots = maxSell.Swap.Lots
	}

	if maxBuyLots+maxSellLots < pgm.Lots*2 {
		return nil, fmt.Errorf("cannot create bot with %d lots. 2 x %d = %d lots total balance required to start, "+
			"and only %d %s lots and %d %s lots = %d total lots are available", pgm.Lots, pgm.Lots, pgm.Lots*2,
			maxBuyLots, unbip(pgm.QuoteID), maxSellLots, unbip(pgm.BaseID), maxSellLots+maxBuyLots)
	}

	return m, nil
}

func newMakerBot(ctx context.Context, c *Core, pgm *MakerProgram) (*makerBot, error) {
	m, err := createMakerBot(ctx, c, pgm)
	if err != nil {
		return nil, err
	}
	m.pgmID, err = m.saveProgram()
	if err != nil {
		return nil, fmt.Errorf("Error saving bot program: %v", err)
	}

	c.notify(newBotNote(TopicBotCreated, "", "", db.Data, m.report()))

	return m, nil
}

func recreateMakerBot(ctx context.Context, c *Core, pgmID uint64, pgm *MakerProgram) (*makerBot, error) {
	m, err := createMakerBot(ctx, c, pgm)
	if err != nil {
		return nil, err
	}
	m.pgmID = pgmID
	return m, nil
}

func (m *makerBot) program() *MakerProgram {
	return m.programV.Load().(*MakerProgram)
}

func (m *makerBot) liveOrderIDs() []order.OrderID {
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

func (m *makerBot) retire() {
	m.stop()
	if err := m.db.RetireBotProgram(m.pgmID); err != nil {
		m.log.Errorf("error retiring bot program")
	} else {
		m.notify(newBotNote(TopicBotRetired, "", "", db.Data, m.report()))
	}
}

// stop stops the bot and cancels all live orders. Note that stop is not called
// on *Core shutdown, so existing bot orders remain live if shutdown is forced.
func (m *makerBot) stop() {
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

func (m *makerBot) run(ctx context.Context) {
	if !atomic.CompareAndSwapUint32(&m.running, 0, 1) {
		m.log.Errorf("run called while makerBot already running")
		return
	}
	defer atomic.StoreUint32(&m.running, 0)

	ctx, m.die = context.WithCancel(ctx)
	defer m.die()

	pgm := m.program()

	book, bookFeed, err := m.syncBook(pgm.Host, pgm.BaseID, pgm.QuoteID)
	if err != nil {
		m.log.Errorf("Error establishing book feed: %v", err)
		return
	}
	m.book = book

	if pgm.OracleWeighting != nil {
		m.startOracleSync(ctx)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer bookFeed.Close()
		for {
			select {
			case n := <-bookFeed.Next():
				m.handleBookNote(n)
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
				m.handleNote(ctx, n)
			case <-ctx.Done():
				return
			}
		}
	}()

	m.notify(newBotNote(TopicBotStarted, "", "", db.Data, m.report()))

	m.wg.Wait()
}

func (m *makerBot) botOrders() []*BotOrder {
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

func (m *makerBot) report() *BotReport {
	return &BotReport{
		ProgramID: m.pgmID,
		Program:   m.program(),
		Running:   atomic.LoadUint32(&m.running) == 1,
		Orders:    m.botOrders(),
	}
}

func (m *makerBot) dbRecord() (*db.BotProgram, error) {
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

func (m *makerBot) saveProgram() (uint64, error) {
	dbRecord, err := m.dbRecord()
	if err != nil {
		return 0, err
	}
	return m.db.SaveBotProgram(dbRecord)
}

func (m *makerBot) updateProgram(pgm *MakerProgram) error {
	dbRecord, err := m.dbRecord()
	if err != nil {
		return err
	}
	if err := m.db.UpdateBotProgram(m.pgmID, dbRecord); err != nil {
		return err
	}

	if pgm.oracleWeighting() > 0 {
		m.startOracleSync(m.ctx) // no-op if sync is already running
	}

	m.programV.Store(pgm)
	m.notify(newBotNote(TopicBotUpdated, "", "", db.Data, m.report()))
	return nil
}

func (m *makerBot) oraclePrice() float64 {
	m.mm.cache.RLock()
	defer m.mm.cache.RUnlock()
	p := m.mm.cache.prices[m.market.Name]
	if p == nil {
		return 0
	}
	if time.Since(p.stamp) > time.Minute*10 {
		return 0
	}
	return p.price
}

func (m *makerBot) syncOraclePrice(ctx context.Context) (float64, error) {
	m.log.Trace("syncing oracle price")
	p, err := m.marketAveragedPrice(ctx)
	if err != nil {
		return 0, err
	}
	m.mm.cache.Lock()
	m.mm.cache.prices[m.market.Name] = &stampedPrice{
		stamp: time.Now(),
		price: p,
	}
	m.mm.cache.Unlock()
	return p, nil
}

func (m *makerBot) startOracleSync(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		m.syncOracle(ctx)
		m.wg.Done()
	}()
}

func (m *makerBot) syncOracle(ctx context.Context) {
	if !atomic.CompareAndSwapUint32(&m.oracleRunning, 0, 1) {
		m.log.Errorf("syncOracle called while already running")
		return
	}
	defer atomic.StoreUint32(&m.oracleRunning, 0)

	var lastCheck time.Time
	m.mm.cache.RLock()
	if p := m.mm.cache.prices[m.market.Name]; p != nil {
		lastCheck = p.stamp
	}
	m.mm.cache.RUnlock()

	// If we don't already have a price, sync now.
	if time.Since(lastCheck) > time.Minute {
		if _, err := m.syncOraclePrice(ctx); err != nil {
			m.log.Errorf("failed to establish a market-averaged price: %v", err)
		}
	}

	for {
		select {
		case <-time.After(time.Minute * 3):
		case <-ctx.Done():
			return
		}
		if _, err := m.syncOraclePrice(ctx); err != nil {
			m.log.Errorf("failed to get market-averaged price: %v", err)
		}
	}
}

// The basisPrice will be rounded to an integer multiple of the rate step.
func (m *makerBot) basisPrice() uint64 {
	pgm := m.program()

	// Error is almost certainly an empty book. Just ignore it and roll with
	// the zero.
	u, _ := m.book.MidGap()
	basisPrice := float64(u) // float64 message-rate units

	m.log.Tracef("basisPrice: mid-gap price = %d", u)

	if w := pgm.oracleWeighting(); w > 0 {
		p := m.oraclePrice()

		if p > 0 {
			m.log.Tracef("basisPrice: raw oracle price = %.8f", p)

			msgOracleRate := p * m.conventionalRateToAtomic * calc.RateEncodingFactor

			// Apply any specified of Bias.
			if pgm.OracleBias != 0 {
				msgOracleRate *= 1 + pgm.OracleBias

				m.log.Tracef("basisPrice: biased oracle price = %.0f", msgOracleRate)
			}

			if basisPrice == 0 { // no mid-gap available. Just use the oracle value straight.
				basisPrice = msgOracleRate
				m.log.Tracef("basisPrice: using basis price %.0f from oracle because no mid-gap was found in order book", basisPrice)
			} else {
				basisPrice = msgOracleRate*w + basisPrice*(1-w)
				m.log.Tracef("basisPrice: oracle-weighted basis price = %f", basisPrice)
			}
		} else {
			m.log.Warnf("no oracle price available for %s bot", m.market.Name)
		}

	}

	if basisPrice > 0 {
		return steppedRate(uint64(basisPrice), m.market.RateStep)
	}

	// If we're still unable to resolve a mid-gap price, infer it from the
	// fiat rates.
	m.log.Infof("no basis price available from order book data or known exchanges. using fiat-based fallback.")
	fiatRates := m.fiatConversions()
	baseRate, found := fiatRates[pgm.BaseID]
	if !found {
		return 0
	}
	quoteRate, found := fiatRates[pgm.QuoteID]
	if !found {
		return 0
	}
	convRate := baseRate / quoteRate // ($ / DCR) / ($ / BTC) => BTC / DCR
	basisPrice = convRate * m.conventionalRateToAtomic * calc.RateEncodingFactor

	return steppedRate(uint64(basisPrice), m.market.RateStep)
}

func (m *makerBot) marketAveragedPrice(ctx context.Context) (float64, error) {
	// They're going to return the quote prices in terms of USD, which is
	// sort of nonsense for a non-USD market like DCR-BTC.
	baseSlug := coinpapSlug(m.base.Symbol, m.base.Name)
	quoteSlug := coinpapSlug(m.quote.Symbol, m.quote.Name)

	type coinpapQuote struct {
		Price  float64 `json:"price"`
		Volume float64 `json:"volume_24h"`
	}

	type coinpapMarket struct {
		BaseCurrencyID         string                   `json:"base_currency_id"`
		QuoteCurrencyID        string                   `json:"quote_currency_id"`
		MarketURL              string                   `json:"market_url"`
		AdjustedVolume24hShare float64                  `json:"adjusted_volume_24h_share"`
		LastUpdated            time.Time                `json:"last_updated"`
		TrustScore             string                   `json:"trust_score"`
		Quotes                 map[string]*coinpapQuote `json:"quotes"`
	}

	// We use a cache for the market data in case there is more than one bot
	// running on the same market.
	var rawMarkets []*coinpapMarket
	url := fmt.Sprintf("https://api.coinpaprika.com/v1/coins/%s/markets", baseSlug)
	if err := getInto(ctx, url, &rawMarkets); err != nil {
		return 0, err
	}

	// Create filter for desireable matches.
	marketMatches := func(mkt *coinpapMarket) bool {
		if mkt.TrustScore != "high" {
			return false
		}
		if time.Since(mkt.LastUpdated) > time.Minute*30 {
			return false
		}
		return (mkt.BaseCurrencyID == baseSlug && mkt.QuoteCurrencyID == quoteSlug) ||
			(mkt.BaseCurrencyID == quoteSlug && mkt.QuoteCurrencyID == baseSlug)
	}

	var filteredResults []*coinpapMarket
	for _, mkt := range rawMarkets {
		if marketMatches(mkt) {
			filteredResults = append(filteredResults, mkt)
		}
	}

	var weightedSum, totalWeight, usdVolume float64
	var n int
	addMarket := func(mkt *coinpapMarket, midGap float64) {
		n++
		weightedSum += mkt.AdjustedVolume24hShare * midGap
		totalWeight += mkt.AdjustedVolume24hShare
		usdQuote, found := mkt.Quotes["USD"]
		if found {
			usdVolume += usdQuote.Volume
		}
	}

	for _, mkt := range filteredResults {
		if mkt.BaseCurrencyID == baseSlug {
			buy, sell := m.spread(ctx, mkt.MarketURL, m.base.Symbol, m.quote.Symbol)
			if buy > 0 && sell > 0 {
				addMarket(mkt, (buy+sell)/2)
			}
		} else {
			buy, sell := m.spread(ctx, mkt.MarketURL, m.quote.Symbol, m.base.Symbol) // base and quote switched
			if buy > 0 && sell > 0 {
				addMarket(mkt, 2/(buy+sell)) // inverted
			}
		}
	}

	if totalWeight == 0 {
		m.log.Tracef("marketAveragedPrice: no markets")
		return 0, ErrNoMarkets
	}

	rate := weightedSum / totalWeight
	// TODO: Require a minimum USD volume?
	m.log.Tracef("marketAveragedPrice: price calculated from %d markets: rate = %f, USD volume = %f", n, rate, usdVolume)
	return rate, nil
}

// handleNote handles the makerBot's Core notifications.
func (m *makerBot) handleNote(ctx context.Context, note Notification) {
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
	case *OrderNote:
		ord := n.Order
		if ord == nil {
			return
		}
		m.processTrade(ord)
	case *EpochNotification:
		go m.rebalance(ctx, n.Epoch)
	}
}

// processTrade processes an order update.
func (m *makerBot) processTrade(o *Order) {
	if len(o.ID) == 0 {
		return
	}

	var oid order.OrderID
	copy(oid[:], o.ID)

	m.log.Tracef("processTrade: oid = %s, status = %s", oid, o.Status)

	m.ordMtx.Lock()
	defer m.ordMtx.Unlock()
	_, found := m.ords[oid]
	if !found {
		return
	}

	convRate := float64(o.Rate) / m.conventionalRateToAtomic / calc.RateEncodingFactor
	m.log.Tracef("processTrade: oid = %s, status = %s, qty = %d, filled = %d, rate = %f", oid, o.Status, o.Qty, o.Filled, convRate)

	if o.Status > order.OrderStatusBooked {
		// We stop caring when the order is taken off the book.
		delete(m.ords, oid)

		switch {
		case o.Filled == o.Qty:
			m.log.Tracef("processTrade: order filled")
		case o.Status == order.OrderStatusCanceled:
			if len(o.Matches) == 0 {
				m.log.Tracef("processTrade: order canceled WITHOUT matches")
			} else {
				m.log.Tracef("processTrade: order canceled WITH matches")
			}
		}
		return
	}

	// Update our reference.
	m.ords[oid] = o
}

func (m *makerBot) handleBookNote(u *BookUpdate) {
	// Really nothing to do with the updates. We just need to keep the
	// subscription live in order to get a mid-gap rate when needed.
}

// rebalance is the per-epoch workhorse of makerBot, performing all calculations
// necessary and canceling and placing orders.
func (m *makerBot) rebalance(ctx context.Context, newEpoch uint64) {
	if !atomic.CompareAndSwapUint32(&m.rebalanceRunning, 0, 1) {
		return
	}
	defer atomic.StoreUint32(&m.rebalanceRunning, 0)

	newBuyLots, newSellLots, buyPrice, sellPrice := rebalance(ctx, m, m.market.RateStep, m.program(), m.log, newEpoch)

	// Place buy orders.
	if newBuyLots > 0 {
		ord := m.placeOrder(uint64(newBuyLots), buyPrice, false)
		if ord != nil {
			var oid order.OrderID
			copy(oid[:], ord.ID)
			m.ordMtx.Lock()
			m.ords[oid] = ord
			m.ordMtx.Unlock()
		}
	}

	// Place sell orders.
	if newSellLots > 0 {
		ord := m.placeOrder(uint64(newSellLots), sellPrice, true)
		if ord != nil {
			var oid order.OrderID
			copy(oid[:], ord.ID)
			m.ordMtx.Lock()
			m.ords[oid] = ord
			m.ordMtx.Unlock()
		}
	}
}

// rebalancer is a stub to enabling testing of the rebalance calculations.
type rebalancer interface {
	basisPrice() uint64
	breakEvenHalfSpread(basisPrice uint64) (uint64, error)
	sortedOrders() (buys, sells []*sortedOrder)
	cancelOrder(oid order.OrderID) error
	MaxBuy(host string, base, quote uint32, rate uint64) (*MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*MaxOrderEstimate, error)
}

func rebalance(ctx context.Context, m rebalancer, rateStep uint64, pgm *MakerProgram, log dex.Logger, newEpoch uint64) (newBuyLots, newSellLots int, buyPrice, sellPrice uint64) {
	basisPrice := m.basisPrice()
	if basisPrice == 0 {
		log.Errorf("No basis price available")
		return
	}

	log.Tracef("rebalance: basis price = %d", basisPrice)

	halfSpread, err := m.breakEvenHalfSpread(basisPrice)
	if err != nil {
		log.Errorf("Could not calculate break-even spread: %v", err)
	}

	log.Tracef("rebalance: half-spread = %d", halfSpread)

	if pgm.SpreadMultiplier > 0 {
		boostMult := 1 + pgm.SpreadMultiplier
		halfSpread = steppedRate(uint64(math.Round(float64(halfSpread)*boostMult)), rateStep)
		log.Tracef("rebalance: boosted half-spread = %d", halfSpread)
	}

	buyPrice = basisPrice - halfSpread
	sellPrice = basisPrice + halfSpread

	log.Tracef("rebalance: buy price = %d, sell price = %d", buyPrice, sellPrice)

	buys, sells := m.sortedOrders()

	// To account for tolerance and shifts and avoid self-matching, figure out
	// the best existing sell and buy.
	highestBuy, lowestSell := buyPrice, sellPrice
	for _, ord := range sells {
		if ord.rate < lowestSell {
			lowestSell = ord.rate
		}
	}
	for _, ord := range buys {
		if ord.rate > highestBuy {
			highestBuy = ord.rate
		}
	}

	var cantBuy, cantSell bool
	if buyPrice >= lowestSell {
		log.Tracef("rebalance: can't buy because delayed cancel sell order interferes. booked rate = %d, buy price = %d",
			lowestSell, buyPrice)
		cantBuy = true
	}
	if sellPrice <= highestBuy {
		log.Tracef("rebalance: can't sell because delayed cancel sell order interferes. booked rate = %d, sell price = %d",
			highestBuy, sellPrice)
		cantSell = true
	}

	var canceledBuyLots, canceledSellLots uint64 // for stats reporting
	cancels := make([]*sortedOrder, 0)
	addCancel := func(ord *sortedOrder) {
		if ord.Sell {
			canceledSellLots += ord.lots
		} else {
			canceledBuyLots += ord.lots
		}
		if ord.Status <= order.OrderStatusBooked {
			cancels = append(cancels, ord)
		}
	}

	processSide := func(ords []*sortedOrder, price uint64, sell bool) (keptLots int) {
		tol := uint64(math.Round(float64(price) * pgm.DriftTolerance))
		low, high := price-tol, price+tol

		// Limit large drift tolerances to their respective sides, i.e. mid-gap
		// is a hard cutoff.
		if !sell && high > basisPrice {
			high = basisPrice - 1
		}
		if sell && low < basisPrice {
			low = basisPrice + 1
		}

		for _, ord := range ords {
			if ord.rate < low || ord.rate > high {
				if newEpoch < ord.Epoch+2 { // https://github.com/decred/dcrdex/pull/1682
					log.Tracef("rebalance: postponing cancellation for order < 2 epochs old")
					keptLots += int(ord.lots)
				} else {
					log.Tracef("rebalance: cancelling out-of-bounds order (%d lots remaining). rate %d is not in range %d < r < %d",
						ord.lots, ord.rate, low, high)
					addCancel(ord)
				}
			} else {
				keptLots += int(ord.lots)
			}
		}
		return
	}

	newBuyLots, newSellLots = int(pgm.Lots), int(pgm.Lots)
	keptBuys := processSide(buys, buyPrice, false)
	keptSells := processSide(sells, sellPrice, true)
	newBuyLots -= keptBuys
	newSellLots -= keptSells

	// Cancel out of bounds or over-stacked orders.
	if len(cancels) > 0 {
		// Only cancel orders that are > 1 epoch old.
		log.Tracef("rebalance: cancelling %d orders", len(cancels))
		for _, cancel := range cancels {
			if err := m.cancelOrder(cancel.id); err != nil {
				log.Errorf("error cancelling order: %v", err)
				return
			}
		}
	}

	if cantBuy {
		newBuyLots = 0
	}
	if cantSell {
		newSellLots = 0
	}

	log.Tracef("rebalance: %d buy lots and %d sell lots scheduled after existing valid %d buy and %d sell lots accounted",
		newBuyLots, newSellLots, keptBuys, keptSells)

	// If we can't afford them all, we'll expand our order for the other side.
	var maxBuyLots int
	if newBuyLots > 0 {
		// TODO: MaxBuy and MaxSell shouldn't error for insufficient funds, but
		// they do. Maybe consider a constant error asset.InsufficientBalance.
		maxOrder, err := m.MaxBuy(pgm.Host, pgm.BaseID, pgm.QuoteID, buyPrice)
		if err != nil {
			log.Tracef("MaxBuy error: %v", err)
		} else {
			maxBuyLots = int(maxOrder.Swap.Lots)
		}
		if maxBuyLots < newBuyLots {
			shortLots := newBuyLots - maxBuyLots
			newSellLots += shortLots
			newBuyLots = maxBuyLots
			log.Tracef("rebalance: reduced buy lots to %d because of low balance", newBuyLots)
		}
	}

	if newSellLots > 0 {
		var maxLots int
		maxOrder, err := m.MaxSell(pgm.Host, pgm.BaseID, pgm.QuoteID)
		if err != nil {
			log.Tracef("MaxSell error: %v", err)
		} else {
			maxLots = int(maxOrder.Swap.Lots)
		}
		if maxLots < newSellLots {
			shortLots := newSellLots - maxLots
			newBuyLots += shortLots
			if newBuyLots > maxBuyLots {
				log.Tracef("rebalance: increased buy lot order to %d lots because sell balance is low", newBuyLots)
				newBuyLots = maxBuyLots
			}
			newSellLots = maxLots
			log.Tracef("rebalance: reduced sell lots to %d because of low balance", newSellLots)
		}
	}
	return
}

// placeOrder places a single order on the market.
func (m *makerBot) placeOrder(lots, rate uint64, sell bool) *Order {
	pgm := m.program()
	ord, err := m.Trade(nil, &TradeForm{
		Host:    pgm.Host,
		IsLimit: true,
		Sell:    sell,
		Base:    pgm.BaseID,
		Quote:   pgm.QuoteID,
		Qty:     lots * m.market.LotSize,
		Rate:    rate,
		Program: m.pgmID,
	})
	if err != nil {
		m.log.Errorf("Error placing rebalancing order: %v", err)
		return nil
	}
	return ord
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
func (m *makerBot) sortedOrders() (buys, sells []*sortedOrder) {
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
func (m *makerBot) feeEstimates(form *TradeForm) (swapFees, redeemFees uint64, err error) {
	dc, connected, err := m.dex(m.program().Host)
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

// breakEvenHalfSpread is the minimum spread that should be maintained to
// theoretically break even on a single-lot round-trip order pair. The returned
// spread half-width will be rounded up to an integer multiple of the rate step.
func (m *makerBot) breakEvenHalfSpread(basisPrice uint64) (uint64, error) {
	pgm := m.program()
	lotSize := m.market.LotSize
	form := &TradeForm{
		Host:    pgm.Host,
		IsLimit: true,
		Sell:    true,
		Base:    pgm.BaseID,
		Quote:   pgm.QuoteID,
		Qty:     lotSize,
		Rate:    basisPrice,
		TifNow:  false,
		// Options: ,
	}

	baseFees, quoteFees, err := m.feeEstimates(form)
	if err != nil {
		return 0, fmt.Errorf("error getting sell order estimate: %w", err)
	}

	m.log.Tracef("breakEvenHalfSpread: sell = true fees: base = %d, quote = %d", baseFees, quoteFees)

	form.Sell = false
	newQuoteFees, newBaseFees, err := m.feeEstimates(form)
	m.log.Tracef("breakEvenHalfSpread: sell = false fees: base = %d, quote = %d", newBaseFees, newQuoteFees)
	if err != nil {
		return 0, fmt.Errorf("error getting buy order estimate: %w", err)
	}
	baseFees += newBaseFees
	quoteFees += newQuoteFees

	quoteLot := calc.BaseToQuote(basisPrice, lotSize)

	baseLossRate := float64(baseFees) / float64(lotSize)
	quoteLossRate := float64(quoteFees) / float64(quoteLot)
	lossRatio := baseLossRate + quoteLossRate

	m.log.Tracef("breakEvenHalfSpread: baseLossRate = %f, quoteLossRate = %f", baseLossRate, quoteLossRate)

	// Looking for the condition in which profit == fee_loss, in units of quote asset
	// loss ~= (base_loss_rate + quote_lot_rate) * quote_lot // approximation all in terms of quote asset
	// loss = loss_ratio * quote_lot
	// profit = (sell_price * sell_qty) - (buy_price * buy_qty) // units: quote asset
	// profit = lot_size * (sell_price - buy_price)
	// profit = lot_size * spread
	// lot_size * spread_atomic = loss_ratio * quote_lot
	// spread_atomic = loss_ratio * quote_lot / lot_size = loss_ratio * (lot_size * atomic_rate) / lot_size
	// spread_atomic = loss_ratio * atomic_rate
	// half_spread_msgrate = loss_ratio * basis_price / 2
	halfGap := uint64(math.Round(lossRatio * float64(basisPrice) / 2)) // message-rate units

	m.log.Tracef("breakEvenHalfSpread: raw half gap: %d", halfGap)

	rateStep := m.market.RateStep
	remainder := halfGap % rateStep
	halfGap = halfGap - remainder
	// round up
	if remainder > 0 || halfGap == 0 {
		halfGap += rateStep
	}
	return halfGap, nil
}

// spread fetches market data and returns the best buy and sell prices.
// TODO: We may be able to do better. We could pull a small amount of market
// book data and do a VWAP-like integration of, say, 1 DEX lot's worth.
func (m *makerBot) spread(ctx context.Context, addr string, baseSymbol, quoteSymbol string) (sell, buy float64) {
	u, err := url.Parse(addr)
	if u == nil {
		m.log.Errorf("Error parsing URL %q: %v", addr, err)
		return 0, 0
	}
	// remove subdomains
	parts := strings.Split(u.Host, ".")
	if len(parts) < 2 {
		return 0, 0
	}
	host := parts[len(parts)-2] + "." + parts[len(parts)-1]

	s := spreaders[host]
	if s == nil {
		return 0, 0
	}
	sell, buy, err = s(ctx, baseSymbol, quoteSymbol)
	if err != nil {
		m.log.Errorf("Error getting spread from %q: %v", addr, err)
		return 0, 0
	}
	return sell, buy
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

// CreateBot creates a market-maker bot.
func (c *Core) CreateBot(pw []byte, botType string, pgm *MakerProgram) (uint64, error) {
	if botType != MakerBotV0 {
		return 0, fmt.Errorf("unknown bot type %q", botType)
	}

	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return 0, codedError(passwordErr, err)
	}

	if err := c.prepareBotWallets(crypter, pgm.BaseID, pgm.QuoteID); err != nil {
		return 0, err
	}

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

	c.mm.RLock()
	bot := c.mm.bots[pgmID]
	c.mm.RUnlock()
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}

	if atomic.LoadUint32(&bot.running) == 1 {
		c.log.Warnf("Ignoring attempt to start an alread-running bot for %s", bot.market.Name)
		return nil
	}

	if err := c.prepareBotWallets(crypter, bot.base.ID, bot.quote.ID); err != nil {
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
	c.mm.RLock()
	bot := c.mm.bots[pgmID]
	c.mm.RUnlock()
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}
	bot.stop()
	return nil
}

// UpdateBotProgram updates the program of an existing market-maker bot.
func (c *Core) UpdateBotProgram(pgmID uint64, pgm *MakerProgram) error {
	c.mm.RLock()
	bot := c.mm.bots[pgmID]
	c.mm.RUnlock()
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}
	return bot.updateProgram(pgm)
}

// RetireBot stops a bot and deletes its program from the database.
func (c *Core) RetireBot(pgmID uint64) error {
	c.mm.RLock()
	defer c.mm.RUnlock()
	bot := c.mm.bots[pgmID]
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}
	bot.retire()
	delete(c.mm.bots, pgmID)
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

// Spreader is a function that can generate market spread data for a known
// exchange.
type Spreader func(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error)

var spreaders = map[string]Spreader{
	"binance.com":  fetchBinanceSpread,
	"coinbase.com": fetchCoinbaseSpread,
	"bittrex.com":  fetchBittrexSpread,
	"hitbtc.com":   fetchHitBTCSpread,
	"exmo.com":     fetchEXMOSpread,
}

func fetchBinanceSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/bookTicker?symbol=%s", slug)

	var resp struct {
		BidPrice float64 `json:"bidPrice,string"`
		AskPrice float64 `json:"askPrice,string"`
	}
	return resp.AskPrice, resp.BidPrice, getInto(ctx, url, &resp)
}

func fetchCoinbaseSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.exchange.coinbase.com/products/%s/ticker", slug)

	var resp struct {
		Ask float64 `json:"ask,string"`
		Bid float64 `json:"bid,string"`
	}

	return resp.Ask, resp.Bid, getInto(ctx, url, &resp)
}

func fetchBittrexSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.bittrex.com/v3/markets/%s/ticker", slug)
	var resp struct {
		AskRate float64 `json:"askRate,string"`
		BidRate float64 `json:"bidRate,string"`
	}
	return resp.AskRate, resp.BidRate, getInto(ctx, url, &resp)
}

func fetchHitBTCSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.hitbtc.com/api/3/public/orderbook/%s?depth=1", slug)

	var resp struct {
		Ask [][2]json.Number `json:"ask"`
		Bid [][2]json.Number `json:"bid"`
	}
	if err := getInto(ctx, url, &resp); err != nil {
		return 0, 0, err
	}
	if len(resp.Ask) < 1 || len(resp.Bid) < 1 {
		return 0, 0, fmt.Errorf("not enough orders")
	}

	ask, err := resp.Ask[0][0].Float64()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode ask price %q", resp.Ask[0][0])
	}

	bid, err := resp.Bid[0][0].Float64()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode bid price %q", resp.Bid[0][0])
	}

	return ask, bid, nil
}

func fetchEXMOSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s_%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.exmo.com/v1.1/order_book?pair=%s&limit=1", slug)

	var resp map[string]*struct {
		AskTop float64 `json:"ask_top,string"`
		BidTop float64 `json:"bid_top,string"`
	}

	if err := getInto(ctx, url, &resp); err != nil {
		return 0, 0, err
	}

	mkt := resp[slug]
	if mkt == nil {
		return 0, 0, errors.New("slug not in response")
	}

	return mkt.AskTop, mkt.BidTop, nil
}

// steppedRate rounds the rate to the nearest integer multiple of the step.
func steppedRate(r, step uint64) uint64 {
	steps := math.Round(float64(r) / float64(step))
	return uint64(math.Round(steps * float64(step)))
}

// stampedPrice is used for caching price data that can expire.
type stampedPrice struct {
	stamp time.Time
	price float64
}
