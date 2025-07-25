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
	dexfiro "decred.org/dcrdex/dex/networks/firo"
	"decred.org/dcrdex/tatanka/chain"
)

const (
	FiroID          = 136
	firoVersion     = 0
	firoFeeInterval = time.Minute * 5
)

func init() {
	chain.RegisterChainConstructor(FiroID, NewFiro)
}

type firoChain struct {
	net  dex.Network
	log  dex.Logger
	fees chan uint64
	name string

	connected atomic.Bool
}

func NewFiro(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {

	return &firoChain{
		net:  net,
		log:  log,
		name: "Firo",
		fees: make(chan uint64, 1),
	}, nil
}

func (c *firoChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
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

func (c *firoChain) Connected() bool {
	return c.connected.Load()
}

func (c *firoChain) Version() uint32 {
	return firoVersion
}

func (c *firoChain) FeeChannel() <-chan uint64 {
	return c.fees
}

func (c *firoChain) monitorFees(ctx context.Context) {
	ticker := time.NewTicker(bchFeeInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				var feeRate uint64
				if c.net == dex.Mainnet {
					var err error
					feeRate, err = dexfiro.ExternalFeeRate(ctx, dex.Mainnet)
					if err != nil {
						c.log.Errorf("Error fetching Bitcoin Cash fee: %v", err)
					}
				} else {
					feeRate = dexfiro.DefaultFee * 1000
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
