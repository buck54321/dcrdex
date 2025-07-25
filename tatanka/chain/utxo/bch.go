// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package utxo

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/tatanka/chain"
)

const (
	BitcoinCashID  = 145
	bchVersion     = 0
	bchFeeInterval = time.Minute * 5
	bchDefaultFee  = 100
)

func init() {
	chain.RegisterChainConstructor(BitcoinCashID, NewBitcoinCash)
}

type bitcoinCashChain struct {
	net  dex.Network
	log  dex.Logger
	fees chan uint64
	name string

	connected atomic.Bool
}

func NewBitcoinCash(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {

	return &bitcoinCashChain{
		net:  net,
		log:  log,
		name: "Bitcoin Cash",
		fees: make(chan uint64, 1),
	}, nil
}

func (c *bitcoinCashChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
	c.connected.Store(true)
	defer c.connected.Store(false)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.monitorFees(ctx)
	}()

	return &wg, nil
}

func (c *bitcoinCashChain) Connected() bool {
	return c.connected.Load()
}

func (c *bitcoinCashChain) Version() uint32 {
	return bchVersion
}

func (c *bitcoinCashChain) FeeChannel() <-chan uint64 {
	return c.fees
}

func (c *bitcoinCashChain) monitorFees(ctx context.Context) {
	fetch := dexbtc.BitcoreRateFetcher("BCH")
	ticker := time.NewTicker(bchFeeInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				var feeRate uint64
				if c.net == dex.Mainnet {
					var err error
					feeRate, err = fetch(ctx, dex.Mainnet)
					if err != nil {
						c.log.Errorf("Error fetching Bitcoin Cash fee: %v", err)
					}
				} else {
					feeRate = bchDefaultFee
				}
				select {
				case c.fees <- feeRate:
				default:
					c.log.Warnf("blocking Bitcoin Cash fee rate report channel")
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}
