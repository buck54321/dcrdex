// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
)

// DCRDataFeeRate gets the fee rate from the external API.
func DCRDataFeeRate(ctx context.Context, net dex.Network, nb uint64) (float64, error) {
	var uri string
	if net == dex.Testnet {
		uri = fmt.Sprintf("https://testnet.dcrdata.org/insight/api/utils/estimatefee?nbBlocks=%d", nb)
	} else { // mainnet and simnet
		uri = fmt.Sprintf("https://explorer.dcrdata.org/insight/api/utils/estimatefee?nbBlocks=%d", nb)
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var resp map[uint64]float64
	if err := dexnet.Get(ctx, uri, &resp); err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, errors.New("null response")
	}
	dcrPerKB, ok := resp[nb]
	if !ok {
		return 0, errors.New("no fee rate for requested number of blocks")
	}
	return dcrPerKB, nil
}
