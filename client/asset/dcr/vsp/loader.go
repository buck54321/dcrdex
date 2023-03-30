package vsp

import (
	"context"
	"net"

	"github.com/decred/dcrd/dcrutil/v4"
)

func NewVSPClient(wf WalletFetcher, vspHost, vspPubKey string, feeAcct, changeAcct uint32,
	maxFee dcrutil.Amount, dialer func(ctx context.Context, network, addr string) (net.Conn, error)) (*Client, error) {

	return New(Config{
		URL:    vspHost,
		PubKey: vspPubKey,
		Dialer: dialer,
		Wallet: wf,
		Policy: Policy{
			MaxFee:     maxFee,
			FeeAcct:    feeAcct,
			ChangeAcct: changeAcct,
		},
	})
}
