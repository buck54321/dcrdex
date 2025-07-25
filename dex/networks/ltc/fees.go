// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package ltc

import (
	"context"
	"errors"
	"math"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

func ExternalFeeRate(ctx context.Context, net dex.Network) (uint64, error) {
	switch net {
	case dex.Mainnet, dex.Simnet:
		return fetchBlockCypherFees(ctx)
	}
	return bitcoreFeeRate(ctx, net)
}

var bitcoreFeeRate = dexbtc.BitcoreRateFetcher("LTC")

func fetchBlockCypherFees(ctx context.Context) (uint64, error) {
	// Posted rate limits for blockcypher are 3/sec, 100/hr, 1000/day. 1000/day
	// is once per 86.4 seconds, but I've seen unexpected metering before.
	// Once per 5 minutes is a good rate, and that's the default.
	// Can't find a source for testnet. Just using mainnet rate for everything.
	const uri = "https://api.blockcypher.com/v1/ltc/main"
	var res struct {
		Low    float64 `json:"low_fee_per_kb"`
		Medium float64 `json:"medium_fee_per_kb"`
		High   float64 `json:"high_fee_per_kb"`
	}
	if err := dexnet.Get(ctx, uri, &res); err != nil {
		return 0, err
	}
	if res.Low == 0 {
		return 0, errors.New("no fee rate in result")
	}
	return uint64(math.Round(res.Low / 1000)), nil
}
