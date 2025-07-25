// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package utxo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/feeratefetcher"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/tatanka/chain"
)

const (
	BitcoinID      = 0
	feeMonitorTick = time.Second * 10
	btcVersion     = 0
)

func init() {
	chain.RegisterChainConstructor(0, NewBitcoin)
}

type BitcoinConfig struct {
	dexbtc.PaidSourceConfig
}

type bitcoinChain struct {
	net        dex.Network
	log        dex.Logger
	fees       chan uint64
	name       string
	feeFetcher *feeratefetcher.FeeRateFetcher

	connected atomic.Bool
}

func NewBitcoin(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {
	var cfg BitcoinConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling Bitcoin config file: %w", err)
	}
	return &bitcoinChain{
		net:        net,
		log:        log,
		name:       "Bitcoin",
		feeFetcher: dexbtc.NewFeeFetcher(&cfg.PaidSourceConfig, log.SubLogger("FF")),
		fees:       make(chan uint64, 1),
	}, nil
}

func (c *bitcoinChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
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

func (c *bitcoinChain) Connected() bool {
	return c.connected.Load()
}

func (c *bitcoinChain) Version() uint32 {
	return btcVersion
}

func (c *bitcoinChain) FeeChannel() <-chan uint64 {
	return c.fees
}

func (c *bitcoinChain) monitorFees(ctx context.Context) {
	cm := dex.NewConnectionMaster(c.feeFetcher)
	if err := cm.Connect(ctx); err != nil {
		c.log.Errorf("error connecting fee Bitcoin rate fetcher: %w", err)
		return
	}

	go func() {
		for {
			select {
			case fr := <-c.feeFetcher.Next():
				select {
				case c.fees <- fr:
				default:
					c.log.Warnf("blocking Bitcoin fee rate report channel")
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	cm.Wait()
}
