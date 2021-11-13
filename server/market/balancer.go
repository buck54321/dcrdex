// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/server/asset"
)

type BackedBalancer struct {
	tunnels         map[string]MarketTunnel
	assets          map[uint32]*backedBalancer
	matchNegotiator MatchNegotiator
}

func NewBackedBalancer(tunnels map[string]MarketTunnel, assets map[uint32]*asset.BackedAsset, matchNegotiator MatchNegotiator) *BackedBalancer {
	balancers := make(map[uint32]*backedBalancer)
	for assetID, ba := range assets {
		balancer, is := ba.Backend.(asset.AccountBalancer)
		if !is {
			continue
		}
		balancers[assetID] = &backedBalancer{
			balancer:  balancer,
			assetInfo: &ba.Asset,
		}
	}

	return &BackedBalancer{
		tunnels:         tunnels,
		assets:          balancers,
		matchNegotiator: matchNegotiator,
	}
}

func (b *BackedBalancer) CheckBalance(acctAddr string, assetID uint32, qty, lots uint64, redeems int) bool {
	backedAsset, found := b.assets[assetID]
	if !found {
		log.Criticalf("asset ID %d not found in accountBalancer assets map", assetID)
		return false
	}
	bal, err := backedAsset.balancer.AccountBalance(acctAddr)
	if err != nil {
		log.Criticalf("error getting account balance for %q: %v", acctAddr, err)
		return false
	}

	// Add quantity for unfilled orders.
	for _, mt := range b.tunnels {
		newQty, newLots, newRedeems := mt.AccountPending(acctAddr, assetID)
		lots += newLots
		qty += newQty
		redeems += newRedeems
	}

	// Add in-process swaps.
	newQty, newLots, newRedeems := b.matchNegotiator.AccountStats(acctAddr, assetID)
	lots += newLots
	qty += newQty
	redeems += newRedeems

	assetInfo := backedAsset.assetInfo
	redeemCosts := uint64(redeems) * assetInfo.RedeemSize * assetInfo.MaxFeeRate
	reqFunds := calc.RequiredOrderFunds(qty, 0, lots, assetInfo) + redeemCosts
	return bal >= reqFunds
}

type backedBalancer struct {
	balancer  asset.AccountBalancer
	assetInfo *dex.Asset
}
