// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/tatanka/tanka"
)

/*
	The chain package describes the interfaces that must be implemented for
	Tatatanka node blockchain backends.
*/

type BadQueryError error

type ChainConfig struct {
	ConfigPath string
}

// type Query json.RawMessage
type Result json.RawMessage

// Chain is an interface that must be implemented by every blockchain backend.
type Chain interface {
	Connect(context.Context) (*sync.WaitGroup, error)
	Connected() bool
	Version() uint32
}

type BondChecker interface {
	CheckBond(*tanka.Bond) error
}

// FeeRater is an optional interface that should be implemented by backends for
// which the network transaction fee rates are variable. The Tatanka Mesh
// provides a oracle service for these chains.
type FeeRater interface {
	FeeChannel() <-chan uint64
}

// ChainConstructor is a constructor for a Chain.
type ChainConstructor func(config json.RawMessage, log dex.Logger, net dex.Network) (Chain, error)

var chains = make(map[uint32]ChainConstructor)

// RegisterChainConstructor is called by chain backends to register their
// ChainConstructors.
func RegisterChainConstructor(chainID uint32, c ChainConstructor) {
	chains[chainID] = c
}

// New is used by the caller to construct a new Chain.
func New(chainID uint32, cfg json.RawMessage, log dex.Logger, net dex.Network) (Chain, error) {
	c, found := chains[chainID]
	if !found {
		return nil, fmt.Errorf("chain %d not known", chainID)
	}
	return c(cfg, log, net)
}
