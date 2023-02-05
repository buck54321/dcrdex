// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Check that rpcclient satisfies the ethFetcher interface.
var (
	_ ethFetcher = (*rpcclient)(nil)

	bigZero              = new(big.Int)
	headerExpirationTime = time.Minute
)

type ContextCaller interface {
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
}

type ethConn struct {
	*ethclient.Client
	endpoint string
	// swapContract is the current ETH swapContract.
	swapContract swapContract
	// tokens are tokeners for loaded tokens. tokens is not protected by a
	// mutex, as it is expected that the caller will connect and place calls to
	// loadToken sequentially in the same thread during initialization.
	tokens map[uint32]*tokener
	// caller is a client for raw calls not implemented by *ethclient.Client.
	caller          ContextCaller
	txPoolSupported bool
}

type rpcclient struct {
	net       dex.Network
	log       dex.Logger
	endpoints []string
	clients   []*ethConn

	idxMtx      sync.RWMutex
	endpointIdx int
}

func newRPCClient(net dex.Network, endpoints []string, log dex.Logger) *rpcclient {
	return &rpcclient{
		net:       net,
		endpoints: endpoints,
		log:       log,
	}
}

func (c *rpcclient) withClient(f func(ec *ethConn) error, haltOnNotFound ...bool) (err error) {
	for range c.endpoints {
		c.idxMtx.RLock()
		idx := c.endpointIdx
		ec := c.clients[idx]
		c.idxMtx.RUnlock()

		err = f(ec)
		if err == nil {
			return nil
		}
		if len(haltOnNotFound) > 0 && haltOnNotFound[0] && (errors.Is(err, ethereum.NotFound) || strings.Contains(err.Error(), "not found")) {
			return err
		}
		c.log.Errorf("Unpropagated error from %q: %v", c.endpoints[idx], err)
		// Try the next client.
		c.idxMtx.Lock()
		// Only advance it if another thread hasn't.
		if c.endpointIdx == idx && len(c.endpoints) > 0 {
			c.endpointIdx = (c.endpointIdx + 1) % len(c.endpoints)
			c.log.Infof("Switching RPC endpoint to %q", c.endpoints[c.endpointIdx])
		}
		c.idxMtx.Unlock()
	}
	return fmt.Errorf("all providers failed. last error: %w", err)
}

// connect connects to an ipc socket. It then wraps ethclient's client and
// bundles commands in a form we can easily use.
func (c *rpcclient) connect(ctx context.Context) (err error) {
	netAddrs, found := dexeth.ContractAddresses[ethContractVersion]
	if !found {
		return fmt.Errorf("no contract address for eth version %d", ethContractVersion)
	}
	contractAddr, found := netAddrs[c.net]
	if !found {
		return fmt.Errorf("no contract address for eth version %d on %s", ethContractVersion, c.net)
	}

	var success bool

	c.clients = make([]*ethConn, len(c.endpoints))
	for i, endpoint := range c.endpoints {
		client, err := rpc.DialContext(ctx, endpoint)
		if err != nil {
			return fmt.Errorf("unable to dial rpc to %q: %v", endpoint, err)
		}

		defer func() {
			if !success {
				client.Close()
			}
		}()

		ec := &ethConn{
			Client:   ethclient.NewClient(client),
			endpoint: endpoint,
			tokens:   make(map[uint32]*tokener),
		}

		reqModules := []string{"eth", "txpool"}
		if err := dexeth.CheckAPIModules(client, endpoint, c.log, reqModules); err != nil {
			c.log.Warnf("Error checking required modules at %q: %v", endpoint, err)
			c.log.Warnf("Will not account for pending transactions in balance calculations at %q", endpoint)
			ec.txPoolSupported = false
		} else {
			ec.txPoolSupported = true
		}

		hdr, err := ec.HeaderByNumber(ctx, nil)
		if err != nil {
			return fmt.Errorf("error getting best header from %q: %v", endpoint, err)
		}
		if c.headerIsOutdated(hdr) {
			return fmt.Errorf("initial header fetched from %q appears to be outdated (time %s). If you continue to see this message, you might need to check your system clock",
				endpoint, time.Unix(int64(hdr.Time), 0))
		}

		es, err := swapv0.NewETHSwap(contractAddr, ec.Client)
		if err != nil {
			return fmt.Errorf("unable to initialize eth contract for %q: %v", endpoint, err)
		}
		ec.swapContract = &swapSourceV0{es}
		ec.caller = client

		c.clients[i] = ec
	}
	success = true
	return nil
}

func (c *rpcclient) headerIsOutdated(hdr *types.Header) bool {
	return c.net != dex.Simnet && hdr.Time < uint64(time.Now().Add(-headerExpirationTime).Unix())
}

// shutdown shuts down the client.
func (c *rpcclient) shutdown() {
	for _, ec := range c.clients {
		ec.Close()
	}
}

func (c *rpcclient) loadToken(ctx context.Context, assetID uint32) error {
	for _, cl := range c.clients {
		tkn, err := newTokener(ctx, assetID, c.net, cl.Client)
		if err != nil {
			return fmt.Errorf("error constructing ERC20Swap: %w", err)
		}

		cl.tokens[assetID] = tkn
	}
	return nil
}

func (c *rpcclient) withTokener(assetID uint32, f func(*tokener) error) error {
	return c.withClient(func(ec *ethConn) error {
		tkn, found := ec.tokens[assetID]
		if !found {
			return fmt.Errorf("no swap source for asset %d", assetID)
		}
		return f(tkn)
	})

}

// bestHeader gets the best header at the time of calling.
func (c *rpcclient) bestHeader(ctx context.Context) (hdr *types.Header, err error) {
	return hdr, c.withClient(func(ec *ethConn) error {
		hdr, err = ec.HeaderByNumber(ctx, nil)
		if err == nil && c.headerIsOutdated(hdr) {
			c.log.Errorf("Best header from %q appears to be outdated (time %s). If you continue to see this message, you might need to check your system clock",
				ec.endpoint, time.Unix(int64(hdr.Time), 0))
			if len(c.endpoints) > 0 {
				c.idxMtx.Lock()
				c.endpointIdx = (c.endpointIdx + 1) % len(c.endpoints)
				endpoint := c.endpoints[c.endpointIdx]
				c.idxMtx.Unlock()
				c.log.Infof("Switching RPC endpoint to %q", endpoint)
			}
		}
		return err
	})

}

// headerByHeight gets the best header at height.
func (c *rpcclient) headerByHeight(ctx context.Context, height uint64) (hdr *types.Header, err error) {
	return hdr, c.withClient(func(ec *ethConn) error {
		hdr, err = ec.HeaderByNumber(ctx, big.NewInt(int64(height)))
		return err
	})
}

// suggestGasTipCap retrieves the currently suggested priority fee to allow a
// timely execution of a transaction.
func (c *rpcclient) suggestGasTipCap(ctx context.Context) (tipCap *big.Int, err error) {
	return tipCap, c.withClient(func(ec *ethConn) error {
		tipCap, err = ec.SuggestGasTipCap(ctx)
		return err
	})
}

// syncProgress return the current sync progress. Returns no error and nil when not syncing.
func (c *rpcclient) syncProgress(ctx context.Context) (prog *ethereum.SyncProgress, err error) {
	return prog, c.withClient(func(ec *ethConn) error {
		prog, err = ec.SyncProgress(ctx)
		return err
	})
}

// blockNumber gets the chain length at the time of calling.
func (c *rpcclient) blockNumber(ctx context.Context) (bn uint64, err error) {
	return bn, c.withClient(func(ec *ethConn) error {
		bn, err = ec.BlockNumber(ctx)
		return err
	})
}

// swap gets a swap keyed by secretHash in the contract.
func (c *rpcclient) swap(ctx context.Context, assetID uint32, secretHash [32]byte) (state *dexeth.SwapState, err error) {
	if assetID == BipID {
		return state, c.withClient(func(ec *ethConn) error {
			state, err = ec.swapContract.Swap(ctx, secretHash)
			return err
		})
	}
	return state, c.withTokener(assetID, func(tkn *tokener) error {
		state, err = tkn.Swap(ctx, secretHash)
		return err
	})
}

// transaction gets the transaction that hashes to hash from the chain or
// mempool. Errors if tx does not exist.
func (c *rpcclient) transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error) {
	return tx, isMempool, c.withClient(func(ec *ethConn) error {
		tx, isMempool, err = ec.TransactionByHash(ctx, hash)
		return err
	}, true) // stop on first provider with "not found", because this should be an error if tx does not exist
}

// dumbBalance gets the account balance, ignoring the effects of unmined
// transactions.
func (c *rpcclient) dumbBalance(ctx context.Context, ec *ethConn, assetID uint32, addr common.Address) (bal *big.Int, err error) {
	if assetID == BipID {
		return ec.BalanceAt(ctx, addr, nil)
	}
	tkn := ec.tokens[assetID]
	if tkn == nil {
		return nil, fmt.Errorf("no tokener for asset ID %d", assetID)
	}
	return tkn.balanceOf(ctx, addr)
}

// smartBalance gets the account balance, including the effects of known
// unmined transactions.
func (c *rpcclient) smartBalance(ctx context.Context, ec *ethConn, assetID uint32, addr common.Address) (bal *big.Int, err error) {
	tip, err := c.blockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("blockNumber error: %v", err)
	}

	// We need to subtract and pending outgoing value, but ignore any pending
	// incoming value since that can't be spent until mined. So we can't using
	// PendingBalanceAt or BalanceAt by themselves.
	// We'll iterate tx pool transactions and subtract any value and fees being
	// sent from this account. The rpc.Client doesn't expose the
	// txpool_contentFrom => (*TxPool).ContentFrom RPC method, for whatever
	// reason, so we'll have to use CallContext and copy the mimic the
	// internal RPCTransaction type.
	var txs map[string]map[string]*RPCTransaction
	if err := ec.caller.CallContext(ctx, &txs, "txpool_contentFrom", addr); err != nil {
		return nil, fmt.Errorf("contentFrom error: %w", err)
	}

	if assetID == BipID {
		ethBalance, err := ec.BalanceAt(ctx, addr, big.NewInt(int64(tip)))
		if err != nil {
			return nil, err
		}
		outgoingEth := new(big.Int)
		for _, group := range txs { // 2 groups, pending and queued
			for _, tx := range group {
				outgoingEth.Add(outgoingEth, tx.Value.ToInt())
				gas := new(big.Int).SetUint64(uint64(tx.Gas))
				if tx.GasPrice != nil && tx.GasPrice.ToInt().Cmp(bigZero) > 0 {
					outgoingEth.Add(outgoingEth, new(big.Int).Mul(gas, tx.GasPrice.ToInt()))
				} else if tx.GasFeeCap != nil {
					outgoingEth.Add(outgoingEth, new(big.Int).Mul(gas, tx.GasFeeCap.ToInt()))
				} else {
					return nil, fmt.Errorf("cannot find fees for tx %s", tx.Hash)
				}
			}
		}
		return ethBalance.Sub(ethBalance, outgoingEth), nil
	}

	// For tokens, we'll do something similar, but with checks for pending txs
	// that transfer tokens or pay to the swap contract.
	// Can't use withTokener because we need to use the same ethConn due to
	// txPoolSupported being used to decide between {smart/dumb}Balance.
	tkn := ec.tokens[assetID]
	if tkn == nil {
		return nil, fmt.Errorf("no tokener for asset ID %d", assetID)
	}
	bal, err = tkn.balanceOf(ctx, addr)
	if err != nil {
		return nil, err
	}
	for _, group := range txs {
		for _, rpcTx := range group {
			to := *rpcTx.To
			if to == tkn.tokenAddr {
				if sent := tkn.transferred(rpcTx.Input); sent != nil {
					bal.Sub(bal, sent)
				}
			}
			if to == tkn.contractAddr {
				if swapped := tkn.swapped(rpcTx.Input); swapped != nil {
					bal.Sub(bal, swapped)
				}
			}
		}
	}
	return bal, nil
}

// accountBalance gets the account balance. If txPool functions are supported by the
// client, it will include the effects of unmined transactions, otherwise it will not.
func (c *rpcclient) accountBalance(ctx context.Context, assetID uint32, addr common.Address) (bal *big.Int, err error) {
	return bal, c.withClient(func(ec *ethConn) error {
		if ec.txPoolSupported {
			bal, err = c.smartBalance(ctx, ec, assetID, addr)
		} else {
			bal, err = c.dumbBalance(ctx, ec, assetID, addr)
		}
		return err
	})

}

type RPCTransaction struct {
	Value     *hexutil.Big    `json:"value"`
	Gas       hexutil.Uint64  `json:"gas"`
	GasPrice  *hexutil.Big    `json:"gasPrice"`
	GasFeeCap *hexutil.Big    `json:"maxFeePerGas,omitempty"`
	Hash      common.Hash     `json:"hash"`
	To        *common.Address `json:"to"`
	Input     hexutil.Bytes   `json:"input"`
	// BlockHash        *common.Hash      `json:"blockHash"`
	// BlockNumber      *hexutil.Big      `json:"blockNumber"`
	// From             common.Address    `json:"from"`
	// GasTipCap        *hexutil.Big      `json:"maxPriorityFeePerGas,omitempty"`
	// Nonce            hexutil.Uint64    `json:"nonce"`
	// TransactionIndex *hexutil.Uint64   `json:"transactionIndex"`
	// Type             hexutil.Uint64    `json:"type"`
	// Accesses         *types.AccessList `json:"accessList,omitempty"`
	// ChainID          *hexutil.Big      `json:"chainId,omitempty"`
	// V                *hexutil.Big      `json:"v"`
	// R                *hexutil.Big      `json:"r"`
	// S                *hexutil.Big      `json:"s"`
}
