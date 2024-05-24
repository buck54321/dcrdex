// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
)

const (
	// Our mid-gap rate derived from the local DEX order book is converted to an
	// effective mid-gap that can only vary by up to 3% from the oracle rate.
	// This is to prevent someone from taking advantage of a sparse market to
	// force a bot into giving a favorable price. In reality a market maker on
	// an empty market should use a high oracle bias anyway, but this should
	// prevent catastrophe.
	maxOracleMismatch = 0.03
)

// GapStrategy is a specifier for an algorithm to choose the maker bot's target
// spread.
type GapStrategy string

const (
	// GapStrategyMultiplier calculates the spread by multiplying the
	// break-even gap by the specified multiplier, 1 <= r <= 100.
	GapStrategyMultiplier GapStrategy = "multiplier"
	// GapStrategyAbsolute sets the spread to the rate difference.
	GapStrategyAbsolute GapStrategy = "absolute"
	// GapStrategyAbsolutePlus sets the spread to the rate difference plus the
	// break-even gap.
	GapStrategyAbsolutePlus GapStrategy = "absolute-plus"
	// GapStrategyPercent sets the spread as a ratio of the mid-gap rate.
	// 0 <= r <= 0.1
	GapStrategyPercent GapStrategy = "percent"
	// GapStrategyPercentPlus sets the spread as a ratio of the mid-gap rate
	// plus the break-even gap.
	GapStrategyPercentPlus GapStrategy = "percent-plus"
)

// OrderPlacement represents the distance from the mid-gap and the
// amount of lots that should be placed at this distance.
type OrderPlacement struct {
	// Lots is the max number of lots to place at this distance from the
	// mid-gap rate. If there is not enough balance to place this amount
	// of lots, the max that can be afforded will be placed.
	Lots uint64 `json:"lots"`

	// GapFactor controls the gap width in a way determined by the GapStrategy.
	GapFactor float64 `json:"gapFactor"`
}

// BasicMarketMakingConfig is the configuration for a simple market
// maker that places orders on both sides of the order book.
type BasicMarketMakingConfig struct {
	// GapStrategy selects an algorithm for calculating the distance from
	// the basis price to place orders.
	GapStrategy GapStrategy `json:"gapStrategy"`

	// SellPlacements is a list of order placements for sell orders.
	// The orders are prioritized from the first in this list to the
	// last.
	SellPlacements []*OrderPlacement `json:"sellPlacements"`

	// BuyPlacements is a list of order placements for buy orders.
	// The orders are prioritized from the first in this list to the
	// last.
	BuyPlacements []*OrderPlacement `json:"buyPlacements"`

	// DriftTolerance is how far away from an ideal price orders can drift
	// before they are replaced (units: ratio of price). Default: 0.1%.
	// 0 <= x <= 0.01.
	DriftTolerance float64 `json:"driftTolerance"`

	// OracleWeighting affects how the target price is derived based on external
	// market data. OracleWeighting, r, determines the target price with the
	// formula:
	//   target_price = dex_mid_gap_price * (1 - r) + oracle_price * r
	// OracleWeighting is limited to 0 <= x <= 1.0.
	// Fetching of price data is disabled if OracleWeighting = 0.
	OracleWeighting *float64 `json:"oracleWeighting"`

	// OracleBias applies a bias in the positive (higher price) or negative
	// (lower price) direction. -0.05 <= x <= 0.05.
	OracleBias float64 `json:"oracleBias"`

	// EmptyMarketRate can be set if there is no market data available, and is
	// ignored if there is market data available.
	EmptyMarketRate float64 `json:"emptyMarketRate"`
}

func needBreakEvenHalfSpread(strat GapStrategy) bool {
	return strat == GapStrategyAbsolutePlus || strat == GapStrategyPercentPlus || strat == GapStrategyMultiplier
}

func (c *BasicMarketMakingConfig) Validate() error {
	if c.OracleBias < -0.05 || c.OracleBias > 0.05 {
		return fmt.Errorf("bias %f out of bounds", c.OracleBias)
	}
	if c.OracleWeighting != nil {
		w := *c.OracleWeighting
		if w < 0 || w > 1 {
			return fmt.Errorf("oracle weighting %f out of bounds", w)
		}
	}

	if c.DriftTolerance == 0 {
		c.DriftTolerance = 0.001
	}
	if c.DriftTolerance < 0 || c.DriftTolerance > 0.01 {
		return fmt.Errorf("drift tolerance %f out of bounds", c.DriftTolerance)
	}

	if c.GapStrategy != GapStrategyMultiplier &&
		c.GapStrategy != GapStrategyPercent &&
		c.GapStrategy != GapStrategyPercentPlus &&
		c.GapStrategy != GapStrategyAbsolute &&
		c.GapStrategy != GapStrategyAbsolutePlus {
		return fmt.Errorf("unknown gap strategy %q", c.GapStrategy)
	}

	validatePlacement := func(p *OrderPlacement) error {
		var limits [2]float64
		switch c.GapStrategy {
		case GapStrategyMultiplier:
			limits = [2]float64{1, 100}
		case GapStrategyPercent, GapStrategyPercentPlus:
			limits = [2]float64{0, 0.1}
		case GapStrategyAbsolute, GapStrategyAbsolutePlus:
			limits = [2]float64{0, math.MaxFloat64} // validate at < spot price at creation time
		default:
			return fmt.Errorf("unknown gap strategy %q", c.GapStrategy)
		}

		if p.GapFactor < limits[0] || p.GapFactor > limits[1] {
			return fmt.Errorf("%s gap factor %f is out of bounds %+v", c.GapStrategy, p.GapFactor, limits)
		}

		return nil
	}

	sellPlacements := make(map[float64]bool, len(c.SellPlacements))
	for _, p := range c.SellPlacements {
		if _, duplicate := sellPlacements[p.GapFactor]; duplicate {
			return fmt.Errorf("duplicate sell placement %f", p.GapFactor)
		}
		sellPlacements[p.GapFactor] = true
		if err := validatePlacement(p); err != nil {
			return fmt.Errorf("invalid sell placement: %w", err)
		}
	}

	buyPlacements := make(map[float64]bool, len(c.BuyPlacements))
	for _, p := range c.BuyPlacements {
		if _, duplicate := buyPlacements[p.GapFactor]; duplicate {
			return fmt.Errorf("duplicate buy placement %f", p.GapFactor)
		}
		buyPlacements[p.GapFactor] = true
		if err := validatePlacement(p); err != nil {
			return fmt.Errorf("invalid buy placement: %w", err)
		}
	}

	return nil
}

type basicMMCalculator interface {
	basisPrice() uint64
	halfSpread(uint64) (uint64, error)
	feeGapStats(uint64) (*FeeGapStats, error)
}

type basicMMCalculatorImpl struct {
	*market
	book   dexOrderBook
	oracle oracle
	core   botCoreAdaptor
	cfg    *BasicMarketMakingConfig
	log    dex.Logger
}

// basisPrice calculates the basis price for the market maker.
// The mid-gap of the dex order book is used, and if oracles are
// available, and the oracle weighting is > 0, the oracle price
// is used to adjust the basis price.
// If the dex market is empty, but there are oracles available and
// oracle weighting is > 0, the oracle rate is used.
// If the dex market is empty and there are either no oracles available
// or oracle weighting is 0, the fiat rate is used.
// If there is no fiat rate available, the empty market rate in the
// configuration is used.
func (b *basicMMCalculatorImpl) basisPrice() uint64 {
	midGap, err := b.book.MidGap()
	if err != nil && !errors.Is(err, orderbook.ErrEmptyOrderbook) {
		b.log.Errorf("MidGap error: %v", err)
		return 0
	}

	basisPrice := float64(midGap) // float64 message-rate units

	var oracleWeighting, oraclePrice float64
	if b.cfg.OracleWeighting != nil && *b.cfg.OracleWeighting > 0 {
		oracleWeighting = *b.cfg.OracleWeighting
		oraclePrice = b.oracle.getMarketPrice(b.baseID, b.quoteID)
		if oraclePrice == 0 {
			b.log.Warnf("no oracle price available for %s bot", b.name)
		}
	}

	if oraclePrice > 0 {
		msgOracleRate := float64(b.msgRate(oraclePrice))

		// Apply the oracle mismatch filter.
		if basisPrice > 0 {
			low, high := msgOracleRate*(1-maxOracleMismatch), msgOracleRate*(1+maxOracleMismatch)
			if basisPrice < low {
				b.log.Debugf("local mid-gap is below safe range. Using effective mid-gap of %d%% below the oracle rate.", maxOracleMismatch*100)
				basisPrice = low
			} else if basisPrice > high {
				b.log.Debugf("local mid-gap is above safe range. Using effective mid-gap of %d%% above the oracle rate.", maxOracleMismatch*100)
				basisPrice = high
			}
		}

		if b.cfg.OracleBias != 0 {
			msgOracleRate *= 1 + b.cfg.OracleBias
		}

		if basisPrice == 0 { // no mid-gap available. Use the oracle price.
			basisPrice = msgOracleRate
			b.log.Tracef("basisPrice: using basis price %s from oracle because no mid-gap was found in order book", b.fmtRate(uint64(msgOracleRate)))
		} else {
			basisPrice = msgOracleRate*oracleWeighting + basisPrice*(1-oracleWeighting)
			b.log.Tracef("basisPrice: oracle-weighted basis price = %f", b.fmtRate(uint64(msgOracleRate)))
		}
	}

	if basisPrice > 0 {
		return steppedRate(uint64(basisPrice), b.rateStep)
	}

	// TODO: add a configuration to turn off use of fiat rate?
	fiatRate := b.core.ExchangeRateFromFiatSources()
	if fiatRate > 0 {
		return steppedRate(fiatRate, b.rateStep)
	}

	if b.cfg.EmptyMarketRate > 0 {
		emptyMsgRate := b.msgRate(b.cfg.EmptyMarketRate)
		return steppedRate(emptyMsgRate, b.rateStep)
	}

	return 0
}

// halfSpread calculates the distance from the mid-gap where if you sell a lot
// at the basis price plus half-gap, then buy a lot at the basis price minus
// half-gap, you will have one lot of the base asset plus the total fees in
// base units. Since the fees are in base units, basis price can be used to
// convert the quote fees to base units. In the case of tokens, the fees are
// converted using fiat rates.
func (b *basicMMCalculatorImpl) halfSpread(basisPrice uint64) (uint64, error) {
	feeStats, err := b.feeGapStats(basisPrice)
	if err != nil {
		return 0, err
	}
	return feeStats.FeeGap / 2, nil
}

// FeeGapStats is info about market and fee state. The intepretation of the
// various statistics may vary slightly with bot type.
type FeeGapStats struct {
	BasisPrice    uint64 `json:"basisPrice"`
	RemoteGap     uint64 `json:"remoteGap"`
	FeeGap        uint64 `json:"feeGap"`
	RoundTripFees uint64 `json:"roundTripFees"` // base units
}

func (b *basicMMCalculatorImpl) feeGapStats(basisPrice uint64) (*FeeGapStats, error) {
	if basisPrice == 0 { // prevent divide by zero later
		return nil, fmt.Errorf("basis price cannot be zero")
	}

	sellFeesInBaseUnits, err := b.core.OrderFeesInUnits(true, true, basisPrice)
	if err != nil {
		return nil, fmt.Errorf("error getting sell fees in base units: %w", err)
	}

	buyFeesInBaseUnits, err := b.core.OrderFeesInUnits(false, true, basisPrice)
	if err != nil {
		return nil, fmt.Errorf("error getting buy fees in base units: %w", err)
	}

	/*
	 * g = half-gap
	 * r = basis price (atomic ratio)
	 * l = lot size
	 * f = total fees in base units
	 *
	 * We must choose a half-gap such that:
	 * (r + g) * l / (r - g) = l + f
	 *
	 * This means that when you sell a lot at the basis price plus half-gap,
	 * then buy a lot at the basis price minus half-gap, you will have one
	 * lot of the base asset plus the total fees in base units.
	 *
	 * Solving for g, you get:
	 * g = f * r / (f + 2l)
	 */

	f := sellFeesInBaseUnits + buyFeesInBaseUnits
	l := b.lotSize

	r := float64(basisPrice) / calc.RateEncodingFactor
	g := float64(f) * r / float64(f+2*l)

	halfGap := uint64(math.Round(g * calc.RateEncodingFactor))

	b.log.Tracef("halfSpread: base basis price = %s, lot size = %s, aggregate fees = %s, half-gap = %s",
		b.fmtRate(basisPrice), b.fmtBase(l), b.fmtBaseFees(f), b.fmtRate(halfGap))

	return &FeeGapStats{
		BasisPrice:    basisPrice,
		FeeGap:        halfGap * 2,
		RoundTripFees: f,
	}, nil
}

type basicMarketMaker struct {
	*unifiedExchangeAdaptor
	cfgV             atomic.Value // *BasicMarketMakingConfig
	core             botCoreAdaptor
	oracle           oracle
	rebalanceRunning atomic.Bool
	calculator       basicMMCalculator
}

var _ bot = (*basicMarketMaker)(nil)

func (m *basicMarketMaker) cfg() *BasicMarketMakingConfig {
	return m.cfgV.Load().(*BasicMarketMakingConfig)
}

func (m *basicMarketMaker) orderPrice(basisPrice, breakEven uint64, sell bool, gapFactor float64) uint64 {
	var halfSpread uint64

	// Apply the base strategy.
	switch m.cfg().GapStrategy {
	case GapStrategyMultiplier:
		halfSpread = uint64(math.Round(float64(breakEven) * gapFactor))
	case GapStrategyPercent, GapStrategyPercentPlus:
		halfSpread = uint64(math.Round(gapFactor * float64(basisPrice)))
	case GapStrategyAbsolute, GapStrategyAbsolutePlus:
		halfSpread = m.msgRate(gapFactor)
	}

	// Add the break-even to the "-plus" strategies
	switch m.cfg().GapStrategy {
	case GapStrategyAbsolutePlus, GapStrategyPercentPlus:
		halfSpread += breakEven
	}

	halfSpread = steppedRate(halfSpread, m.rateStep)

	if sell {
		return basisPrice + halfSpread
	}

	if basisPrice < halfSpread {
		return 0
	}

	return basisPrice - halfSpread
}

func (m *basicMarketMaker) ordersToPlace() (buyOrders, sellOrders []*multiTradePlacement) {
	basisPrice := m.calculator.basisPrice()
	if basisPrice == 0 {
		m.log.Errorf("No basis price available and no empty-market rate set")
		return
	}

	var breakEven uint64
	if needBreakEvenHalfSpread(m.cfg().GapStrategy) {
		var err error
		feeGap, err := m.calculator.feeGapStats(basisPrice)
		if err != nil {
			m.log.Errorf("Could not calculate break-even spread: %v", err)
			return
		}
		m.core.registerFeeGap(feeGap)
		breakEven = feeGap.FeeGap
	}

	if m.log.Level() == dex.LevelTrace {
		m.log.Tracef("ordersToPlace %s, basis price = %s, break-even gap = %s",
			m.name, m.fmtRate(basisPrice), m.fmtRate(breakEven))
	}

	orders := func(orderPlacements []*OrderPlacement, sell bool) []*multiTradePlacement {
		placements := make([]*multiTradePlacement, 0, len(orderPlacements))
		for i, p := range orderPlacements {
			rate := m.orderPrice(basisPrice, breakEven, sell, p.GapFactor)

			if m.log.Level() == dex.LevelTrace {
				m.log.Tracef("ordersToPlace.orders: %s placement # %d, gap factor = %f, rate = %s",
					sellStr(sell), i, p.GapFactor, m.fmtRate(rate))
			}

			lots := p.Lots
			if rate == 0 {
				lots = 0
			}
			placements = append(placements, &multiTradePlacement{
				rate: rate,
				lots: lots,
			})
		}
		return placements
	}

	buyOrders = orders(m.cfg().BuyPlacements, false)
	sellOrders = orders(m.cfg().SellPlacements, true)
	return buyOrders, sellOrders
}

func (m *basicMarketMaker) rebalance(newEpoch uint64) {
	if !m.rebalanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer m.rebalanceRunning.Store(false)
	m.log.Tracef("rebalance: epoch %d", newEpoch)

	buyOrders, sellOrders := m.ordersToPlace()
	m.core.MultiTrade(buyOrders, false, m.cfg().DriftTolerance, newEpoch, nil, nil)
	m.core.MultiTrade(sellOrders, true, m.cfg().DriftTolerance, newEpoch, nil, nil)
}

func (m *basicMarketMaker) botLoop(ctx context.Context) (*sync.WaitGroup, error) {

	book, bookFeed, err := m.core.SyncBook(m.host, m.baseID, m.quoteID)
	if err != nil {
		return nil, fmt.Errorf("failed to sync book: %v", err)
	}

	m.calculator = &basicMMCalculatorImpl{
		market: m.market,
		book:   book,
		oracle: m.oracle,
		core:   m.core,
		cfg:    m.cfg(),
		log:    m.log,
	}

	// Process book updates
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case n := <-bookFeed.Next():
				if n.Action == core.EpochMatchSummary {
					payload := n.Payload.(*core.EpochMatchSummaryPayload)
					m.rebalance(payload.Epoch + 1)
				}
			case <-m.ctx.Done():
				return
			}
		}
	}()

	return &wg, nil
}

func (m *basicMarketMaker) updateConfig(cfg *BotConfig) error {
	if cfg.BasicMMConfig == nil {
		// implies bug in caller
		return errors.New("no market making config provided")
	}

	err := cfg.BasicMMConfig.Validate()
	if err != nil {
		return fmt.Errorf("invalid market making config: %v", err)
	}

	m.cfgV.Store(cfg.BasicMMConfig)
	return nil
}

// RunBasicMarketMaker starts a basic market maker bot.
func newBasicMarketMaker(cfg *BotConfig, adaptorCfg *exchangeAdaptorCfg, oracle oracle, log dex.Logger) (*basicMarketMaker, error) {
	if cfg.BasicMMConfig == nil {
		// implies bug in caller
		return nil, errors.New("no market making config provided")
	}

	adaptor, err := newUnifiedExchangeAdaptor(adaptorCfg)
	if err != nil {
		return nil, fmt.Errorf("error constructing exchange adaptor: %w", err)
	}

	err = cfg.BasicMMConfig.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid market making config: %v", err)
	}

	basicMM := &basicMarketMaker{
		unifiedExchangeAdaptor: adaptor,
		core:                   adaptor,
		oracle:                 oracle,
	}
	basicMM.cfgV.Store(cfg.BasicMMConfig)
	adaptor.setBotLoop(basicMM.botLoop)
	return basicMM, nil
}
