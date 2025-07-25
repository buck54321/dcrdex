// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package utxo

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/tatanka/chain"
)

const (
	ZcashID    = 133
	zecVersion = 0
)

func init() {
	chain.RegisterChainConstructor(ZcashID, NewZcash)
}

type zecChain struct {
	net  dex.Network
	log  dex.Logger
	name string

	connected atomic.Bool
}

func NewZcash(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {

	return &zecChain{
		net:  net,
		log:  log,
		name: "Zcash",
	}, nil
}

func (c *zecChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
	c.connected.Store(true)
	defer c.connected.Store(false)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		<-ctx.Done()
		wg.Done()
	}()

	return &wg, nil
}

func (c *zecChain) Connected() bool {
	return c.connected.Load()
}

func (c *zecChain) Version() uint32 {
	return zecVersion
}
