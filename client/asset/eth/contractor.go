// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// contractor is a translation layer between the abigen bindings and the DEX app.
// The intention is that if a new contract is implemented, the contractor
// interface itself will not require any updates.
type contractor interface {
	swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error)
	initiate(*bind.TransactOpts, []*asset.Contract) (*types.Transaction, error)
	redeem(txOpts *bind.TransactOpts, redeems []*asset.Redemption) (*types.Transaction, error)
	refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error)
	estimateInitGas(ctx context.Context, n int) (uint64, error)
	estimateRedeemGas(ctx context.Context, secrets [][32]byte) (uint64, error)
	estimateRefundGas(ctx context.Context, secretHash [32]byte) (uint64, error)
	isRedeemable(secretHash, secret [32]byte) (bool, error)
	// incomingValue checks if the transaction redeems or refunds to the
	// contract and sums the incoming value. It is not an error if the
	// transaction does not pay to the contract, and the value returned in that
	// case will always be zero.
	incomingValue(context.Context, *types.Transaction) (uint64, error)
	isRefundable(secretHash [32]byte) (bool, error)
}

type contractorConstructor func(net dex.Network, addr common.Address, ec *ethclient.Client) (contractor, error)

type contractV0 interface {
	Initiate(opts *bind.TransactOpts, initiations []swapv0.ETHSwapInitiation) (*types.Transaction, error)
	Redeem(opts *bind.TransactOpts, redemptions []swapv0.ETHSwapRedemption) (*types.Transaction, error)
	Swap(opts *bind.CallOpts, secretHash [32]byte) (swapv0.ETHSwapSwap, error)
	Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error)
	IsRedeemable(opts *bind.CallOpts, secretHash [32]byte, secret [32]byte) (bool, error)
	IsRefundable(opts *bind.CallOpts, secretHash [32]byte) (bool, error)
}

var _ contractV0 = (*swapv0.ETHSwap)(nil)

type contractV1 interface {
	Initiate(opts *bind.TransactOpts, contracts []swapv1.ETHSwapContract) (*types.Transaction, error)
	Redeem(opts *bind.TransactOpts, redemptions []swapv1.ETHSwapRedemption) (*types.Transaction, error)
	State(opts *bind.CallOpts, c swapv1.ETHSwapContract) (uint8, error)
	Refund(opts *bind.TransactOpts, c swapv1.ETHSwapContract) (*types.Transaction, error)
	IsRedeemable(opts *bind.CallOpts, c swapv1.ETHSwapContract) (bool, error)
}

var _ contractV1 = (*swapv1.ETHSwap)(nil)

// contractorV0 is the contractor for contract version 0.
// Redeem and Refund methods of swapv0.ETHSwap already have suitable return types.
type contractorV0 struct {
	contractV0   // *swapv0.ETHSwap
	abi          *abi.ABI
	ec           *ethclient.Client
	contractAddr common.Address
	acctAddr     common.Address
}

var _ contractor = (*contractorV0)(nil)

func newV0contractor(net dex.Network, acctAddr common.Address, ec *ethclient.Client) (contractor, error) {
	contractAddr, exists := dexeth.ContractAddresses[0][net]
	if !exists || contractAddr == (common.Address{}) {
		return nil, fmt.Errorf("no contract address for version 0, net %s", net)
	}
	c, err := swapv0.NewETHSwap(contractAddr, ec)
	if err != nil {
		return nil, err
	}
	return &contractorV0{
		contractV0:   c,
		abi:          dexeth.ABIs[0],
		ec:           ec,
		contractAddr: contractAddr,
		acctAddr:     acctAddr,
	}, nil
}

func (c *contractorV0) initiate(txOpts *bind.TransactOpts, contracts []*asset.Contract) (*types.Transaction, error) {
	inits := make([]swapv0.ETHSwapInitiation, 0, len(contracts))
	secrets := make(map[[32]byte]bool, len(contracts))

	for _, contract := range contracts {
		if len(contract.SecretHash) != dexeth.SecretHashSize {
			return nil, fmt.Errorf("wrong secret hash length. wanted %d, got %d", dexeth.SecretHashSize, len(contract.SecretHash))
		}

		var secretHash [32]byte
		copy(secretHash[:], contract.SecretHash)

		if secrets[secretHash] {
			return nil, fmt.Errorf("secret hash %s is a duplicate", contract.SecretHash)
		}
		secrets[secretHash] = true

		bigVal := new(big.Int).SetUint64(contract.Value)

		if !common.IsHexAddress(contract.Address) {
			return nil, fmt.Errorf("%q is not an address", contract.Address)
		}

		inits = append(inits, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(int64(contract.LockTime)),
			SecretHash:      secretHash,
			Participant:     common.HexToAddress(contract.Address),
			Value:           new(big.Int).Mul(bigVal, big.NewInt(dexeth.GweiFactor)),
		})
	}

	return c.contractV0.Initiate(txOpts, inits)
}

func (c *contractorV0) redeem(txOpts *bind.TransactOpts, redemptions []*asset.Redemption) (*types.Transaction, error) {
	redemps := make([]swapv0.ETHSwapRedemption, 0, len(redemptions))
	secretHashes := make(map[[32]byte]bool, len(redemptions))
	for _, r := range redemptions {
		secretB, secretHashB := r.Secret, r.Spends.SecretHash
		if len(secretB) != 32 || len(secretHashB) != 32 {
			return nil, fmt.Errorf("invalid secret and/or secret hash sizes, %d and %d", len(secretB), len(secretHashB))
		}
		var secret, secretHash [32]byte
		copy(secret[:], secretB)
		copy(secretHash[:], secretHashB)
		if secretHashes[secretHash] {
			return nil, fmt.Errorf("duplicate secret hash %x", secretHash[:])
		}
		secretHashes[secretHash] = true

		redemps = append(redemps, swapv0.ETHSwapRedemption{
			Secret:     secret,
			SecretHash: secretHash,
		})
	}
	return c.contractV0.Redeem(txOpts, redemps)
}

func (c *contractorV0) swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    c.acctAddr,
		Context: ctx,
	}
	state, err := c.contractV0.Swap(callOpts, secretHash)
	if err != nil {
		return nil, err
	}

	return &dexeth.SwapState{
		BlockHeight: state.InitBlockNumber.Uint64(),
		LockTime:    time.Unix(state.RefundBlockTimestamp.Int64(), 0),
		Secret:      state.Secret,
		Initiator:   state.Initiator,
		Participant: state.Participant,
		Value:       dexeth.WeiToGwei(state.Value),
		State:       dexeth.SwapStep(state.State),
	}, nil
}

func (c *contractorV0) refund(txOpts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	return c.contractV0.Refund(txOpts, secretHash)
}

func (c *contractorV0) isRedeemable(secretHash, secret [32]byte) (bool, error) {
	return c.contractV0.IsRedeemable(&bind.CallOpts{From: c.acctAddr}, secretHash, secret)
}

func (c *contractorV0) isRefundable(secretHash [32]byte) (bool, error) {
	return c.contractV0.IsRefundable(&bind.CallOpts{From: c.acctAddr}, secretHash)
}

func (c *contractorV0) estimateRedeemGas(ctx context.Context, secrets [][32]byte) (uint64, error) {
	redemps := make([]swapv0.ETHSwapRedemption, 0, len(secrets))
	for _, secret := range secrets {
		redemps = append(redemps, swapv0.ETHSwapRedemption{
			Secret:     secret,
			SecretHash: sha256.Sum256(secret[:]),
		})
	}
	data, err := c.abi.Pack("redeem", redemps)
	if err != nil {
		return 0, err
	}

	return c.ec.EstimateGas(ctx, ethereum.CallMsg{
		From: c.acctAddr,
		To:   &c.contractAddr,
		Data: data,
	})
}

func (c *contractorV0) estimateRefundGas(ctx context.Context, secretHash [32]byte) (uint64, error) {
	data, err := c.abi.Pack("refund", secretHash)
	if err != nil {
		return 0, fmt.Errorf("unexpected error packing abi: %v", err)
	}

	return c.ec.EstimateGas(ctx, ethereum.CallMsg{
		From: c.acctAddr,
		To:   &c.contractAddr,
		Data: data,
	})
}

func (c *contractorV0) estimateInitGas(ctx context.Context, n int) (uint64, error) {
	initiations := make([]swapv0.ETHSwapInitiation, 0, n)
	for j := 0; j < n; j++ {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		initiations = append(initiations, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(1),
			SecretHash:      secretHash,
			Participant:     c.acctAddr,
			Value:           big.NewInt(1),
		})
	}
	data, err := c.abi.Pack("initiate", initiations)
	if err != nil {
		return 0, nil
	}

	return c.ec.EstimateGas(ctx, ethereum.CallMsg{
		From:  c.acctAddr,
		To:    &c.contractAddr,
		Value: big.NewInt(int64(n)),
		Gas:   0,
		Data:  data,
	})
}

func (c *contractorV0) incomingValue(ctx context.Context, tx *types.Transaction) (uint64, error) {
	if *tx.To() != c.contractAddr {
		return 0, nil
	}
	if redeems, err := dexeth.ParseRedeemData(tx.Data(), 0); err == nil {
		var redeemed uint64
		for _, redeem := range redeems {
			swap, err := c.swap(ctx, redeem.SecretHash)
			if err != nil {
				return 0, fmt.Errorf("redeem swap error: %w", err)
			}
			redeemed += swap.Value
		}
		return redeemed, nil
	}
	secretHash, err := dexeth.ParseRefundData(tx.Data(), 0)
	if err != nil {
		return 0, nil
	}
	swap, err := c.swap(ctx, secretHash)
	if err != nil {
		return 0, fmt.Errorf("refund swap error: %w", err)
	}
	return swap.Value, nil
}

type contractorV1 struct {
	contractV1
	abi          *abi.ABI
	net          dex.Network
	contractAddr common.Address
	acctAddr     common.Address
	be           bind.ContractBackend
}

// var _ contractor = (*contractorV1)(nil)

func newV1contractor(net dex.Network, acctAddr common.Address, ec *ethclient.Client) (*contractorV1, error) {
	contractAddr, exists := dexeth.ContractAddresses[1][net]
	if !exists || contractAddr == (common.Address{}) {
		return nil, fmt.Errorf("no contract address for version 0, net %s", net)
	}
	c, err := swapv1.NewETHSwap(contractAddr, ec)
	if err != nil {
		return nil, err
	}
	return &contractorV1{
		contractV1:   c,
		abi:          dexeth.ABIs[1],
		net:          net,
		contractAddr: contractAddr,
		acctAddr:     acctAddr,
		be:           ec,
	}, nil
}

func (c *contractorV1) swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error) {
	return nil, nil
}

func (c *contractorV1) initiate(txOpts *bind.TransactOpts, contracts []*asset.Contract) (*types.Transaction, error) {
	versionedContracts := make([]swapv1.ETHSwapContract, 0, len(contracts))
	for _, contract := range contracts {
		versionedContracts = append(versionedContracts, ContractToV1(c.acctAddr, contract))
	}
	return c.Initiate(txOpts, versionedContracts)
}

func (c *contractorV1) redeem(txOpts *bind.TransactOpts, redeems []*asset.Redemption) (*types.Transaction, error) {
	versionedRedemptions := make([]swapv1.ETHSwapRedemption, 0, len(redeems))
	for _, r := range redeems {
		var initiator common.Address
		copy(initiator[:], r.Spends.Coin.ID())
		versionedRedemptions = append(versionedRedemptions, RedemptionToV1(initiator, c.acctAddr, r))
	}
	return c.Redeem(txOpts, versionedRedemptions)
}

// This one breaks contractor, since we need the entire *asset.Contract, not just
// the secret hash.
func (c *contractorV1) refund(txOpts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	// func (c *contractorV1) refund(txOpts *bind.TransactOpts, contract *asset.Contract) (*types.Transaction, error) {
	// 	return c.Refund(txOpts, ContractToV2(c.acctAddr, contract))
	return nil, nil
}

func (c *contractorV1) estimateInitGas(ctx context.Context, n int) (uint64, error) {
	initiations := make([]swapv1.ETHSwapContract, 0, n)
	for j := 0; j < n; j++ {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		initiations = append(initiations, swapv1.ETHSwapContract{
			RefundTimestamp: 1,
			SecretHash:      secretHash,
			Participant:     c.acctAddr,
			Value:           1,
		})
	}
	data, err := c.abi.Pack("initiate", initiations)
	if err != nil {
		return 0, nil
	}

	return c.be.EstimateGas(ctx, ethereum.CallMsg{
		From:  c.acctAddr,
		To:    &c.contractAddr,
		Value: big.NewInt(int64(n)),
		Gas:   0,
		Data:  data,
	})
}

func (c *contractorV1) estimateRedeemGas(ctx context.Context, secrets [][32]byte) (uint64, error) {
	return 0, nil
}

func (c *contractorV1) estimateRefundGas(ctx context.Context, secretHash [32]byte) (uint64, error) {
	return 0, nil
}

func (c *contractorV1) isRedeemable(secretHash, secret [32]byte) (bool, error) {
	return false, nil
}

func (c *contractorV1) incomingValue(context.Context, *types.Transaction) (uint64, error) {
	return 0, nil
}

func (c *contractorV1) isRefundable(secretHash [32]byte) (bool, error) {
	return false, nil
}

func ContractToV1(initiator common.Address, c *asset.Contract) swapv1.ETHSwapContract {
	var secretHash [32]byte
	copy(secretHash[:], c.SecretHash)
	return swapv1.ETHSwapContract{
		SecretHash:      secretHash,
		Initiator:       initiator,
		RefundTimestamp: c.LockTime,
		Participant:     common.HexToAddress(c.Address),
		Value:           c.Value,
	}
}

func RedemptionToV1(initiator, participant common.Address, r *asset.Redemption) swapv1.ETHSwapRedemption {
	spend := r.Spends
	var secret, secretHash [32]byte
	copy(secret[:], r.Secret)
	copy(secretHash[:], spend.SecretHash)
	return swapv1.ETHSwapRedemption{
		C: swapv1.ETHSwapContract{
			SecretHash:      secretHash,
			Initiator:       initiator,
			RefundTimestamp: uint64(spend.Expiration.Unix()),
			Participant:     participant,
			Value:           spend.Coin.Value(),
		},
		Secret: secret,
	}
}

// readOnlyCallOpts is the CallOpts used for read-only contract method calls.
func readOnlyCallOpts(ctx context.Context) *bind.CallOpts {
	return &bind.CallOpts{
		Pending: true,
		Context: ctx,
	}
}

var contractorConstructors = map[uint32]contractorConstructor{
	0: newV0contractor,
	// 1: newV1contractor,
	// 2: newV2contractor,
}
