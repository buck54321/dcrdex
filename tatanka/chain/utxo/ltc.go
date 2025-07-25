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
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"decred.org/dcrdex/tatanka/chain"
)

const (
	LitecoinID     = 2
	ltcVersion     = 1
	ltcFeeInterval = time.Minute * 5
	ltcDefaultFee  = 10
)

func init() {
	chain.RegisterChainConstructor(LitecoinID, NewLitecoin)
}

type ltcChain struct {
	net  dex.Network
	log  dex.Logger
	fees chan uint64
	name string

	connected atomic.Bool
}

func NewLitecoin(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {

	return &ltcChain{
		net:  net,
		log:  log,
		name: "Litecoin",
		fees: make(chan uint64, 1),
	}, nil
}

func (c *ltcChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
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

func (c *ltcChain) Connected() bool {
	return c.connected.Load()
}

func (c *ltcChain) Version() uint32 {
	return ltcVersion
}

func (c *ltcChain) FeeChannel() <-chan uint64 {
	return c.fees
}

func (c *ltcChain) monitorFees(ctx context.Context) {
	ticker := time.NewTicker(bchFeeInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				var feeRate uint64
				if c.net == dex.Mainnet {
					var err error
					feeRate, err = dexltc.ExternalFeeRate(ctx, dex.Mainnet)
					if err != nil {
						c.log.Errorf("Error fetching Bitcoin Cash fee: %v", err)
					}
				} else {
					feeRate = ltcDefaultFee
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
