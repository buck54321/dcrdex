// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"context"
	"fmt"
	"math/big"

	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
)

// Check that rpcclient satisfies the ethFetcher interface.
var (
	_ ethFetcher = (*rpcclient)(nil)

	bigZero = new(big.Int)
)

type rpcclient struct {
	// ec wraps a *rpc.Client with some useful calls.
	ec *ethclient.Client
	// c is a direct client for raw calls.
	c *rpc.Client
	// es is a wrapper for contract calls.
	es *swapv0.ETHSwap
}

// connect connects to an ipc socket. It then wraps ethclient's client and
// bundles commands in a form we can easil use.
func (c *rpcclient) connect(ctx context.Context, IPC string, contractAddr *common.Address) error {
	client, err := rpc.DialIPC(ctx, IPC)
	if err != nil {
		return fmt.Errorf("unable to dial rpc: %v", err)
	}
	c.ec = ethclient.NewClient(client)
	c.es, err = swapv0.NewETHSwap(*contractAddr, c.ec)
	if err != nil {
		return fmt.Errorf("unable to find swap contract: %v", err)
	}
	c.c = client
	return nil
}

// shutdown shuts down the client.
func (c *rpcclient) shutdown() {
	if c.ec != nil {
		c.ec.Close()
	}
}

// bestBlockHash gets the best blocks hash at the time of calling. Due to the
// speed of Ethereum blocks, this changes often.
func (c *rpcclient) bestBlockHash(ctx context.Context) (common.Hash, error) {
	header, err := c.bestHeader(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	return header.Hash(), nil
}

// bestHeader gets the best header at the time of calling.
func (c *rpcclient) bestHeader(ctx context.Context) (*types.Header, error) {
	bn, err := c.ec.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	return c.ec.HeaderByNumber(ctx, big.NewInt(int64(bn)))
}

// block gets the block identified by hash.
func (c *rpcclient) block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return c.ec.BlockByHash(ctx, hash)
}

// suggestGasPrice retrieves the currently suggested gas price to allow a timely
// execution of a transaction.
func (c *rpcclient) suggestGasPrice(ctx context.Context) (sgp *big.Int, err error) {
	return c.ec.SuggestGasPrice(ctx)
}

// syncProgress return the current sync progress. Returns no error and nil when not syncing.
func (c *rpcclient) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return c.ec.SyncProgress(ctx)
}

// blockNumber gets the chain length at the time of calling.
func (c *rpcclient) blockNumber(ctx context.Context) (uint64, error) {
	return c.ec.BlockNumber(ctx)
}

// peers returns connected peers.
func (c *rpcclient) peers(ctx context.Context) ([]*p2p.PeerInfo, error) {
	var peers []*p2p.PeerInfo
	return peers, c.c.CallContext(ctx, &peers, "admin_peers")
}

// swap gets a swap keyed by secretHash in the contract.
func (c *rpcclient) swap(ctx context.Context, secretHash [32]byte) (*swapv0.ETHSwapSwap, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		Context: ctx,
	}
	swap, err := c.es.Swap(callOpts, secretHash)
	if err != nil {
		return nil, err
	}
	return &swap, nil
}

// transaction gets the transaction that hashes to hash from the chain or
// mempool. Errors if tx does not exist.
func (c *rpcclient) transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error) {
	return c.ec.TransactionByHash(ctx, hash)
}

// accountBalance gets the account balance, including the effects of known
// unmined transactions.
func (c *rpcclient) accountBalance(ctx context.Context, addr common.Address) (*big.Int, error) {
	tip, err := c.blockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("blockNumber error: %v", err)
	}
	currentBal, err := c.ec.BalanceAt(ctx, addr, big.NewInt(int64(tip)))
	if err != nil {
		return nil, err
	}

	var txs map[string]map[string]*RPCTransaction
	err = c.c.CallContext(ctx, &txs, "txpool_contentFrom", addr)
	if err != nil {
		return nil, fmt.Errorf("contentFrom error: %w", err)
	}

	outgoing := new(big.Int)
	for _, group := range txs { // 2 groups, pending and queued
		for _, tx := range group {
			outgoing.Add(outgoing, tx.Value.ToInt())
			gas := new(big.Int).SetUint64(uint64(tx.Gas))
			if tx.GasPrice != nil && tx.GasPrice.ToInt().Cmp(bigZero) > 0 {
				outgoing.Add(outgoing, new(big.Int).Mul(gas, tx.GasPrice.ToInt()))
			} else if tx.GasFeeCap != nil {
				outgoing.Add(outgoing, new(big.Int).Mul(gas, tx.GasFeeCap.ToInt()))
			}
		}
	}

	return currentBal.Sub(currentBal, outgoing), nil
}

type RPCTransaction struct {
	Value     *hexutil.Big   `json:"value"`
	Gas       hexutil.Uint64 `json:"gas"`
	GasPrice  *hexutil.Big   `json:"gasPrice"`
	GasFeeCap *hexutil.Big   `json:"maxFeePerGas,omitempty"`
	// BlockHash        *common.Hash      `json:"blockHash"`
	// BlockNumber      *hexutil.Big      `json:"blockNumber"`
	// From             common.Address    `json:"from"`
	// GasTipCap        *hexutil.Big      `json:"maxPriorityFeePerGas,omitempty"`
	// Hash             common.Hash       `json:"hash"`
	// Input            hexutil.Bytes     `json:"input"`
	// Nonce            hexutil.Uint64    `json:"nonce"`
	// To               *common.Address   `json:"to"`
	// TransactionIndex *hexutil.Uint64   `json:"transactionIndex"`
	// Type             hexutil.Uint64    `json:"type"`
	// Accesses         *types.AccessList `json:"accessList,omitempty"`
	// ChainID          *hexutil.Big      `json:"chainId,omitempty"`
	// V                *hexutil.Big      `json:"v"`
	// R                *hexutil.Big      `json:"r"`
	// S                *hexutil.Big      `json:"s"`
}
