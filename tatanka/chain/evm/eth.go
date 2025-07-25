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
	EthereumID         = 60
	ethProtocolVersion = 1
)

func init() {
	chain.RegisterChainConstructor(EthereumID, NewEthereum)
}

type ethChain struct {
	net  dex.Network
	log  dex.Logger
	name string

	connected atomic.Bool
}

func NewEthereum(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {

	return &ethChain{
		net:  net,
		log:  log,
		name: "Ethereum",
	}, nil
}

func (c *ethChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
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

func (c *ethChain) Connected() bool {
	return c.connected.Load()
}

func (c *ethChain) Version() uint32 {
	return ethProtocolVersion
}
