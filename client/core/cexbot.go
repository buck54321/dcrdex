// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
)

type ExchangeID string

const (
	ExchangeIDBinance ExchangeID = "binance"
)

type CEXBalanceRange struct {
	Low  uint64 `json:"low"`
	High uint64 `json:"high"`
}

type CEXBotProgram struct {
	*BaseBotConfig
	Exchange   ExchangeID       `json:"exchangeID"`
	BaseRange  *CEXBalanceRange `json:"baseRange"`
	QuoteRange *CEXBalanceRange `json:"quoteRange"`
	APIKey     string           `json:"apiKey"`
	APISecret  string           `json:"apiSecret"`
	// ProfitTrigger is the minimum profit before a cross-exchange trade
	// sequence is initiated. Range: 0 < ProfitTrigger << 1. For example, if
	// the ProfitTrigger is 0.01 and a trade sequence would produce a 1% profit
	// or better, a trade sequence will be initiated.
	ProfitTrigger float64 `json:"profitTrigger"`
	// TakerBuffer is an additional buffer to add to the CEX taker order price.
	// Higher TakerBuffer should correspond to higher fill rates, but
	// potentially lower profits. Range: 0 < TakerBuffer << 1
	TakerPriceBuffer float64 `json:"takerPriceBuffer"`
}

type arbitrageSequence struct {
	*Order
	extrema float64
}

type cexBot struct {
	*baseMarketBot
	cex libxc.CEX

	// rebalance synchronization-related fields
	rb struct {
		epochSeen   uint64
		epochActive uint64
		running     uint32
	}

	rebalanceRunning uint32

	seqMtx    sync.RWMutex
	sequences map[order.OrderID]*arbitrageSequence
}

func newCEXBot(ctx context.Context, c *Core, pgm *CEXBotProgram) (*cexBot, error) {
	var cex libxc.CEX
	switch pgm.Exchange {
	case ExchangeIDBinance:
	default:
		return nil, fmt.Errorf("exhange %q not known", pgm.Exchange)
	}

	bb, err := newBaseMarketBot(ctx, CEXBotV0, c, pgm)
	if err != nil {
		return nil, err
	}

	return &cexBot{
		baseMarketBot: bb,
		cex:           cex,
		sequences:     make(map[order.OrderID]*arbitrageSequence),
	}, nil
}

func (c *cexBot) run(ctx context.Context) {
	c.baseMarketBot.run(ctx, &baseBotNoteHandlers{
		book: c.handleBookNote,
		core: func(n Notification) {
			c.handleNote(ctx, n)
		},
	})

	pgm := c.cexProgram()

	var notes chan interface{}
	var cex *CEX
	switch pgm.Exchange {
	case ExchangeIDBinance:
		cex = &c.Core.cex.binance
		cex.Lock()
		if cex.CEX == nil {
			cex.CEX = libxc.NewBinance(pgm.APIKey, pgm.APISecret, c.Core.log.SubLogger("BN"), c.net)
			cex.listeners = make(map[string]map[chan interface{}]struct{})
		}
		cex.Unlock()
	default:
		c.log.Errorf("cexBox.run: exchange %q not known", pgm.Exchange)
		return
	}

	notes, err := c.newCEXListener(ctx, cex, c)
	if err != nil {
		c.log.Errorf("Error running CEX bot: %v", err)
		return
	}

	go func() {
		defer c.returnCEXListener(cex, c, notes)
		for {
			select {
			case n := <-notes:
				switch nt := n.(type) {
				case *libxc.MarketUpdate:
					c.log.Infof("--Need to check if right market first MarketUpdate %+v", nt)
					go c.rebalance(ctx)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *cexBot) cexProgram() *CEXBotProgram {
	return c.baseMarketBot.program().(*CEXBotProgram)
}

func (m *cexBot) updateCEXProgram(pgm *CEXBotProgram) error {
	if err := m.baseMarketBot.updateProgram(pgm); err != nil {
		return err
	}
	m.programV.Store(pgm)
	return nil
}

func (c *cexBot) handleBookNote(u *BookUpdate) {

}

func (c *cexBot) handleNote(ctx context.Context, note Notification) {
	c.baseMarketBot.handleNote(ctx, note)
	switch n := note.(type) {
	case *OrderNote:
		// ord := n.Order
		// if ord == nil {
		// 	return
		// }
		// m.processTrade(ord)
	case *EpochNotification:
		atomic.StoreUint64(&c.rb.epochSeen, n.Epoch)
		go c.rebalance(ctx)
	}
}

func (c *cexBot) rebalance(ctx context.Context) {
	if !atomic.CompareAndSwapUint32(&c.rebalanceRunning, 0, 1) {
		return
	}
	defer atomic.StoreUint32(&c.rebalanceRunning, 0)

	epochActive, epochSeen := atomic.LoadUint64(&c.rb.epochActive), atomic.LoadUint64(&c.rb.epochSeen)
	if epochSeen == epochActive {
		// We've already acted this epoch.
		return
	}

	avgPrice := func(ords []*orderbook.Fill) float64 {
		var weightedSum, weight float64
		for _, ord := range ords {
			q := float64(ord.Quantity) / float64(c.base.UnitInfo.Conventional.ConversionFactor)
			r := c.msgRateToConventional(ord.Rate)
			weight += q
			weightedSum += q * r
		}
		if weight == 0 {
			return 0 // caller shouldn't run zero orders, so should be hard to get here
		}
		return weightedSum / weight
	}

	pgm := c.cexProgram()

	tradeOverlap := func(targetingSells bool) (ordered bool) {
		var lots, dextrema uint64
		var cextrema float64
		for {
			tryLots := lots + 1
			qty := tryLots * c.market.LotSize
			fills, filled := c.book.BestFill(targetingSells, tryLots*c.market.LotSize)
			if !filled || len(fills) == 0 /* sanity */ {
				break
			}
			dexPrice := avgPrice(fills)
			// lowestPrice := buys[len(buys)-1].Rate
			convQty := float64(qty) / float64(c.base.UnitInfo.Conventional.ConversionFactor)
			vwap, highestSell, filled, err := c.cex.VWAP(c.base.Symbol, c.quote.Symbol, !targetingSells, convQty)
			if err != nil {
				c.log.Errorf("Error calculating VWAP: %v", err)
				break
			}
			if !filled {
				break
			}
			// If we're selling on dex and buying it back on cex, we want dex
			// price to be higher.
			profit := dexPrice / vwap
			if targetingSells {
				profit = vwap / dexPrice
			}
			if profit < pgm.ProfitTrigger {
				break
			}
			cextrema = highestSell
			dextrema = fills[len(fills)-1].Rate
			lots++
		}

		if lots == 0 {
			return false
		}

		ord := c.placeOrder(lots, dextrema, !targetingSells)
		if ord == nil {
			return false
		}
		var oid order.OrderID
		copy(oid[:], ord.ID)
		c.seqMtx.Lock()
		c.sequences[oid] = &arbitrageSequence{
			Order:   ord,
			extrema: cextrema,
		}
		c.seqMtx.Unlock()
		return true
	}

	if !tradeOverlap(false) || tradeOverlap(true) {
		atomic.StoreUint64(&c.rb.epochActive, epochSeen)
	}
}

// cex should be read-locked
func totalCEXSubs(cex *CEX) (n int) {
	for _, mktSubs := range cex.listeners {
		n += len(mktSubs)
	}
	return
}

func (c *Core) newCEXListener(ctx context.Context, cex *CEX, bot *cexBot) (chan interface{}, error) {
	cex.Lock()
	defer cex.Unlock()

	mkt := marketName(bot.base.ID, bot.quote.ID)

	mktSubs := cex.listeners[mkt]
	if mktSubs == nil {
		mktSubs = make(map[chan interface{}]struct{})
		cex.listeners[mkt] = mktSubs
	}

	ch := make(chan interface{}, 32)
	mktSubs[ch] = struct{}{}
	if len(mktSubs) == 1 {
		err := cex.SubscribeMarket(ctx, bot.base.Symbol, bot.quote.Symbol, bot.market.LotSize)
		if err != nil {
			delete(mktSubs, ch)
			return nil, err
		}
	}

	if totalCEXSubs(cex) != 1 {
		return ch, nil
	}
	notes := cex.Notifications()

	ctx, cex.cancel = context.WithCancel(ctx)

	cex.conn = dex.NewConnectionMaster(cex)
	if err := cex.conn.Connect(ctx); err != nil {
		delete(mktSubs, ch)
		return nil, err
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			select {
			case n := <-notes:
				switch nt := n.(type) {
				case *libxc.MarketUpdate:
					mkt := strings.ToLower(nt.BaseSymbol) + "_" + strings.ToLower(nt.QuoteSymbol)
					cex.RLock()
					for ch := range cex.listeners[mkt] {
						select {
						case ch <- nt:
						default:
						}
					}
					cex.RUnlock()
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (c *Core) returnCEXListener(cex *CEX, bot *cexBot, ch chan interface{}) {
	mkt := marketName(bot.base.ID, bot.quote.ID)
	cex.Lock()
	defer cex.Unlock()
	mktSubs := cex.listeners[mkt]
	if mktSubs == nil {
		bot.log.Errorf("No market sub map for %q", mkt)
		return
	}
	totalSubsBefore := totalCEXSubs(cex)
	delete(mktSubs, ch)
	if len(mktSubs) == 0 {
		if err := cex.UnsubscribeMarket(bot.base.Symbol, bot.quote.Symbol); err != nil {
			bot.log.Errorf("CEX(%s).UnsubscribeMarket error: %v", bot.cexProgram().Exchange, err)
		}
	}
	if totalCEXSubs(cex) == 0 && totalSubsBefore == 1 && cex.cancel != nil {
		cex.cancel()
	}
}

func (c *Core) createCEXBot(pgm *CEXBotProgram) (uint64, error) {
	bot, err := newCEXBot(c.ctx, c, pgm)
	if err != nil {
		return 0, err
	}
	c.cex.Lock()
	c.cex.bots[bot.pgmID] = bot
	c.cex.Unlock()

	c.wg.Add(1)
	go func() {
		bot.run(c.ctx)
		c.wg.Done()
	}()
	return bot.pgmID, nil
}
