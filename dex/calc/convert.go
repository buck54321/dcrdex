// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package calc

import "math/big"

// RateConversionFactor is used when encoding an exchange rate as an integer.
const RateConversionFactor = 1e8

var (
	bigRateConversionFactor = big.NewInt(RateConversionFactor)
)

// BaseToQuote computes a quote asset amount based on a base asset amount
// and an integer representation of the price rate. That is,
//    quoteAmt = rate * baseAmt / atomsPerCoin
func BaseToQuote(rate uint64, base uint64) (quote uint64) {
	bigRate := big.NewInt(int64(rate))
	bigBase := big.NewInt(int64(base))
	bigBase.Mul(bigBase, bigRate)
	bigBase.Div(bigBase, bigRateConversionFactor)
	return bigBase.Uint64()
}

// QuoteToBase computes a base asset amount based on a quote asset amount
// and an integer representation of the price rate. That is,
//    baseAmt = quoteAmt * atomsPerCoin / rate
func QuoteToBase(rate uint64, quote uint64) (base uint64) {
	if rate == 0 {
		return 0 // caller handle rate==0, but don't panic
	}
	bigRate := big.NewInt(int64(rate))
	bigQuote := big.NewInt(int64(quote))
	bigQuote.Mul(bigQuote, bigRateConversionFactor)
	bigQuote.Div(bigQuote, bigRate)
	return bigQuote.Uint64()
}
