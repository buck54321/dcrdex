// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package firo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
)

// ExternalFeeRate returns a fee rate for the network. If an error is
// encountered fetching the testnet fee rate, we will try to return the
// mainnet fee rate.
func ExternalFeeRate(ctx context.Context, net dex.Network) (uint64, error) {
	const mainnetURI = "https://explorer.firo.org/insight-api-zcoin/utils/estimatefee"
	var uri string
	if net == dex.Testnet {
		uri = "https://testexplorer.firo.org/insight-api-zcoin/utils/estimatefee"
	} else {
		uri = "https://explorer.firo.org/insight-api-zcoin/utils/estimatefee"
	}
	feeRate, err := fetchExternalFee(ctx, uri)
	if err == nil || net != dex.Testnet {
		return feeRate, err
	}
	return fetchExternalFee(ctx, mainnetURI)
}

// fetchExternalFee calls 'estimatefee' API on Firo block explorer for
// the network. API returned float value is converted into sats/byte.
func fetchExternalFee(ctx context.Context, uri string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resp map[string]float64
	if err := dexnet.Get(ctx, uri, &resp); err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, errors.New("null response")
	}

	firoPerKilobyte, ok := resp["2"] // field '2': n.nnnn
	if !ok {
		return 0, errors.New("no fee rate in response")
	}
	if firoPerKilobyte <= 0 {
		return 0, fmt.Errorf("zero or negative fee rate")
	}
	return uint64(math.Round(firoPerKilobyte * 1e5)), nil // FIRO/kB => firo-sat/B
}
