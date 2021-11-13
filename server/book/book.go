// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package book defines the order book used by each Market.
package book

import (
	"sync"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

const (
	// initBookHalfCapacity is the default capacity of one side (buy or sell) of
	// the order book. It is set to 2^16 orders (65536 orders) per book side.
	initBookHalfCapacity uint32 = 1 << 16
)

// Book is a market's order book. The Book uses a configurable lot size, of
// which all inserted orders must have a quantity that is a multiple. The buy
// and sell sides of the order book are contained in separate priority queues to
// allow constant time access to the best orders, and log time insertion and
// removal of orders.
type Book struct {
	mtx     sync.RWMutex
	lotSize uint64
	buys    *OrderPQ
	sells   *OrderPQ
}

// New creates a new order book with the given lot size.
func New(lotSize uint64) *Book {
	return &Book{
		lotSize: lotSize,
		buys:    NewMaxOrderPQ(initBookHalfCapacity, 0),
		sells:   NewMinOrderPQ(initBookHalfCapacity, 0),
	}
}

// Clear reset the order book with configured capacity.
func (b *Book) Clear() {
	b.mtx.Lock()
	b.buys, b.sells = nil, nil
	b.buys = NewMaxOrderPQ(initBookHalfCapacity, 0)
	b.sells = NewMinOrderPQ(initBookHalfCapacity, 0)
	b.mtx.Unlock()
}

// LotSize returns the Book's configured lot size in atoms of the base asset.
func (b *Book) LotSize() uint64 {
	return b.lotSize
}

// BuyCount returns the number of buy orders.
func (b *Book) BuyCount() int {
	return b.buys.Count()
}

// SellCount returns the number of sell orders.
func (b *Book) SellCount() int {
	return b.sells.Count()
}

// BestSell returns a pointer to the best sell order in the order book. The
// order is NOT removed from the book.
func (b *Book) BestSell() *order.LimitOrder {
	return b.sells.PeekBest()
}

// BestBuy returns a pointer to the best buy order in the order book. The
// order is NOT removed from the book.
func (b *Book) BestBuy() *order.LimitOrder {
	return b.buys.PeekBest()
}

// Best returns pointers to the best buy and sell order in the order book. The
// orders are NOT removed from the book.
func (b *Book) Best() (bestBuy, bestSell *order.LimitOrder) {
	b.mtx.RLock()
	bestBuy = b.buys.PeekBest()
	bestSell = b.sells.PeekBest()
	b.mtx.RUnlock()
	return
}

// Insert attempts to insert the provided order into the order book, returning a
// boolean indicating if the insertion was successful. If the order is not an
// integer multiple of the Book's lot size, the order will not be inserted.
func (b *Book) Insert(o *order.LimitOrder) bool {
	if o.Quantity%b.lotSize != 0 {
		log.Warnf("(*Book).Insert: Refusing to insert an order with a quantity that is not a multiple of lot size.")
		return false
	}
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if o.Sell {
		return b.sells.Insert(o)
	}
	return b.buys.Insert(o)
}

// Remove attempts to remove the order with the given OrderID from the book.
func (b *Book) Remove(oid order.OrderID) (*order.LimitOrder, bool) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if removed, ok := b.sells.RemoveOrderID(oid); ok {
		return removed, true
	}
	if removed, ok := b.buys.RemoveOrderID(oid); ok {
		return removed, true
	}
	return nil, false
}

// RemoveUserOrders removes all orders from the book that belong to a user. The
// removed buy and sell orders are returned.
func (b *Book) RemoveUserOrders(user account.AccountID) (removedBuys, removedSells []*order.LimitOrder) {
	return b.buys.RemoveUserOrders(user), b.sells.RemoveUserOrders(user)
}

// HaveOrder checks if an order is in either the buy or sell side of the book.
func (b *Book) HaveOrder(oid order.OrderID) bool {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.buys.HaveOrder(oid) || b.sells.HaveOrder(oid)
}

// Order attempts to retrieve an order from either the buy or sell side of the
// book. If the order data is not required, consider using HaveOrder.
func (b *Book) Order(oid order.OrderID) *order.LimitOrder {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	if lo := b.buys.Order(oid); lo != nil {
		return lo
	}
	return b.sells.Order(oid)
}

// UserOrderTotals returns the total amount in booked orders and the number of
// booked orders, for both the buy and sell sides of the book. Both amounts are
// in units of the base asset, and should be multiples of the market's lot size.
func (b *Book) UserOrderTotals(user account.AccountID) (buyAmt, sellAmt, buyCount, sellCount uint64) {
	b.mtx.RLock()
	buyAmt, buyCount = b.buys.UserOrderTotals(user)
	sellAmt, sellCount = b.sells.UserOrderTotals(user)
	b.mtx.RUnlock()
	return
}

// SellOrders copies out all sell orders in the book, sorted.
func (b *Book) SellOrders() []*order.LimitOrder {
	return b.sells.Orders()
}

// SellOrdersN copies out the N best sell orders in the book, sorted.
func (b *Book) SellOrdersN(N int) []*order.LimitOrder {
	return b.sells.OrdersN(N)
}

// BuyOrders copies out all buy orders in the book, sorted.
func (b *Book) BuyOrders() []*order.LimitOrder {
	return b.buys.Orders()
}

// BuyOrdersN copies out the N best buy orders in the book, sorted.
func (b *Book) BuyOrdersN(N int) []*order.LimitOrder {
	return b.buys.OrdersN(N)
}

// UnfilledUserBuys retrieves all buy orders belonging to a given user that are
// completely unfilled.
func (b *Book) UnfilledUserBuys(user account.AccountID) []*order.LimitOrder {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.buys.UnfilledForUser(user)
}

// UnfilledUserSells retrieves all sell orders belonging to a given user that
// are completely unfilled.
func (b *Book) UnfilledUserSells(user account.AccountID) []*order.LimitOrder {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.sells.UnfilledForUser(user)
}

func (b *Book) IterateBaseAccount(acctAddr string, f func(lo *order.LimitOrder)) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	b.sells.IterateBaseAccount(acctAddr, f)
	b.buys.IterateBaseAccount(acctAddr, f)
}

func (b *Book) IterateQuoteAccount(acctAddr string, f func(lo *order.LimitOrder)) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	b.sells.IterateQuoteAccount(acctAddr, f)
	b.buys.IterateQuoteAccount(acctAddr, f)
}

// func (b *Book) RemainingUserSellQty(user account.AccountID) uint64 {
// 	var qty uint64
// 	b.sells.IterateUser(user, func(lo *order.LimitOrder) {
// 		qty += lo.Quantity - lo.Filled()
// 	})
// 	return qty
// }

// func (b *Book) RemainingUserBuyQty(user account.AccountID) (baseQty, quoteQty uint64) {
// 	var base, quote uint64
// 	b.sells.IterateUser(user, func(lo *order.LimitOrder) {
// 		baseQty := lo.Quantity - lo.Filled()
// 		base += baseQty
// 		quote += calc.BaseToQuote(lo.Rate, baseQty)
// 	})
// 	return base, quote
// }
