// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package zec

import (
	"math"

	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/wire"
)

// https://zips.z.cash/zip-0317

// TransparentTxFeesZIP317 calculates the ZIP-0317 fees for a fully transparent
// Zcash transaction, which only depends on the size of the tx_in and tx_out
// fields.
func TransparentTxFeesZIP317(txInSize, txOutSize uint64) uint64 {
	return TxFeesZIP317(txInSize, txOutSize, 0, 0, 0, 0)
}

// TxFeexZIP317 calculates fees for a transaction. The caller must sum up the
// txin and txout, which is the entire serialization size associated with the
// respective field, including the size of the count varint.
func TxFeesZIP317(transparentTxInsSize, transparentTxOutsSize uint64, nSpendsSapling, nOutputsSapling, nJoinSplit, nActionsOrchard uint64) uint64 {
	const (
		marginalFee           = 5000
		graceActions          = 2
		pkhStandardInputSize  = 150
		pkhStandardOutputSize = 34
	)

	nIn := math.Ceil(float64(transparentTxInsSize) / pkhStandardInputSize)
	nOut := math.Ceil(float64(transparentTxOutsSize) / pkhStandardOutputSize)

	nSapling := uint64(math.Max(float64(nSpendsSapling), float64(nOutputsSapling)))
	logicalActions := uint64(math.Max(nIn, nOut)) + 2*nJoinSplit + nSapling + nActionsOrchard

	return marginalFee * uint64(math.Max(graceActions, float64(logicalActions)))
}

// RequiredOrderFunds is the ZIP-0317 compliant version of
// calc.RequiredOrderFunds.
func RequiredOrderFunds(swapVal, inputCount, inputsSize, maxSwaps uint64) uint64 {
	// One p2sh output for the contract, 1 change output.
	const txOutsSize = dexbtc.P2PKHOutputSize + dexbtc.P2SHOutputSize + 1 /* wire.VarIntSerializeSize(2) */
	txInsSize := inputsSize + uint64(wire.VarIntSerializeSize(inputCount))
	firstTxFees := TransparentTxFeesZIP317(txInsSize, txOutsSize)
	if maxSwaps == 1 {
		return swapVal + firstTxFees
	}

	otherTxsFees := TransparentTxFeesZIP317(dexbtc.RedeemP2PKHInputSize+1, txOutsSize)
	fees := firstTxFees + (maxSwaps-1)*otherTxsFees
	return swapVal + fees
}
