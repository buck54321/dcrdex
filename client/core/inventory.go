// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"sync"
)

type inventory struct {
	sync.RWMutex
	reservations map[*invReservation]struct{}
}

type invAsset struct {
	bookLots   uint64
	settleLots uint64

	mtx      sync.RWMutex
	booked   int64
	settling int64
}

type invRequest struct {
	market *Market
	base   *invAsset
	quote  *invAsset
}

func (inv *inventory) reserve(req *invRequest) *invReservation {
	res := &invReservation{req}
	inv.Lock()
	inv.reservations[res] = struct{}{}
	inv.Unlock()
	return res
}

func (inv *inventory) unreserve(res *invReservation) {
	inv.Lock()
	delete(inv.reservations, res)
	inv.Unlock()
}

func (inv *inventory) reserved(assetID uint32) (qty uint64) {
	inv.RLock()
	defer inv.RUnlock()
	for res := range inv.reservations {
		if res.market.BaseID == assetID {
			qty += (res.base.bookLots + res.base.settleLots) * res.market.LotSize
		} else if res.market.QuoteID == assetID {
			qty += (res.quote.bookLots + res.quote.settleLots) * res.market.LotSize
		}
	}
	return
}

type invReservation struct {
	*invRequest
}

func (r *invReservation) bookableBase() uint64 {
	return bookableLots(r.base)
}

func (r *invReservation) bookableQuote() uint64 {
	return bookableLots(r.quote)
}

func (r *invReservation) bookBase(lots uint64) {
	bookLots(r.base, lots)
}

func (r *invReservation) bookQuote(lots uint64) {
	bookLots(r.quote, lots)
}

func (r *invReservation) matchBase(lots uint64) {
	matchLots(r.base, lots)
}

func (r *invReservation) matchQuote(lots uint64) {
	matchLots(r.quote, lots)
}

func (r *invReservation) settleBase(lots uint64) {
	redeemLots(r.base, lots)
}

func (r *invReservation) settleQuote(lots uint64) {
	redeemLots(r.quote, lots)
}

func bookLots(a *invAsset, lots uint64) {
	a.mtx.Lock()
	a.booked += int64(lots)
	a.mtx.Unlock()
}

func matchLots(a *invAsset, lots uint64) {
	a.mtx.Lock()
	l := int64(lots)
	if l > a.booked {
		a.booked = 0
	} else {
		a.booked -= l
	}
	a.settling += l
	a.mtx.Unlock()
}

func redeemLots(a *invAsset, lots uint64) {
	a.mtx.Lock()
	l := int64(lots)
	if l > a.settling {
		a.settling = 0
	} else {
		a.settling -= l
	}
	a.mtx.Unlock()
}

func bookableLots(a *invAsset) uint64 {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	var bookable uint64
	if int64(a.bookLots) > a.booked {
		bookable = uint64(int64(a.bookLots) - a.booked)
	}
	var dec uint64
	if a.settling > int64(a.settleLots) {
		dec = uint64(a.settling - int64(a.settleLots))
		if dec > bookable {
			dec = bookable
		}
	}
	return bookable - dec
}
