// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package evm

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/tatanka/chain"
)

const (
	PolygonID          = 966
	polProtocolVersion = 1
)

func init() {
	chain.RegisterChainConstructor(PolygonID, NewPolygon)
}

type polChain struct {
	net  dex.Network
	log  dex.Logger
	name string

	connected atomic.Bool
}

func NewPolygon(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {

	return &polChain{
		net:  net,
		log:  log,
		name: "Zcash",
	}, nil
}

func (c *polChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
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

func (c *polChain) Connected() bool {
	return c.connected.Load()
}

func (c *polChain) Version() uint32 {
	return polProtocolVersion
}
