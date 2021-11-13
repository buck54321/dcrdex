// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package book

import (
	"sync"

	"decred.org/dcrdex/dex/order"
)

type AccountTracking uint8

const (
	AccountTrackingBase AccountTracking = 1 << iota
	AccountTrackingQuote
)

func (a AccountTracking) Base() bool {
	return (a & AccountTrackingBase) > 0
}

func (a AccountTracking) Quote() bool {
	return (a & AccountTrackingQuote) > 0
}

type accountTracker struct {
	mtx         sync.RWMutex
	tracking    AccountTracking
	base, quote map[string]map[order.OrderID]*order.LimitOrder
}

func newAccountTracker(tracking AccountTracking) *accountTracker {
	var base, quote map[string]map[order.OrderID]*order.LimitOrder
	if tracking.Base() {
		base = make(map[string]map[order.OrderID]*order.LimitOrder)
	}
	if tracking.Quote() {
		quote = make(map[string]map[order.OrderID]*order.LimitOrder)
	}
	return &accountTracker{
		tracking: tracking,
		base:     base,
		quote:    quote,
	}
}

func (a *accountTracker) add(lo *order.LimitOrder) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.base != nil {
		addAccountOrder(lo.BaseAccount(), a.base, lo)
	}
	if a.quote != nil {
		addAccountOrder(lo.QuoteAccount(), a.quote, lo)
	}
}

func (a *accountTracker) remove(lo *order.LimitOrder) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.base != nil {
		removeAccountOrder(lo.BaseAccount(), a.base, lo.ID())
	}
	if a.quote != nil {
		removeAccountOrder(lo.QuoteAccount(), a.quote, lo.ID())
	}
}

func addAccountOrder(addr string, acctOrds map[string]map[order.OrderID]*order.LimitOrder, lo *order.LimitOrder) {
	ords, found := acctOrds[addr]
	if !found {
		ords = make(map[order.OrderID]*order.LimitOrder)
		acctOrds[addr] = ords
	}
	ords[lo.ID()] = lo
}

func removeAccountOrder(addr string, acctOrds map[string]map[order.OrderID]*order.LimitOrder, oid order.OrderID) {
	ords, found := acctOrds[addr]
	if !found {
		return
	}
	delete(ords, oid)
	if len(ords) == 0 {
		delete(acctOrds, addr)
	}
}

func (a *accountTracker) iterateBase(acctAddr string, f func(*order.LimitOrder)) {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	for _, lo := range a.base[acctAddr] {
		f(lo)
	}
}

func (a *accountTracker) iterateQuote(acctAddr string, f func(*order.LimitOrder)) {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	for _, lo := range a.quote[acctAddr] {
		f(lo)
	}
}
