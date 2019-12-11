// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package boltdb

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	dexdb "decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	bolt "go.etcd.io/bbolt"
)

// Short names for some commonly used imported functions.
var (
	intCoder    = encode.IntCoder
	bEqual      = bytes.Equal
	uint16Bytes = encode.Uint16Bytes
	uint32Bytes = encode.Uint32Bytes
	uint64Bytes = encode.Uint64Bytes
	bCopy       = encode.CopySlice
)

// Bolt works on []byte keys and values. These are some commonly used key and
// value encodings.
var (
	accountsBucket = []byte("accounts")
	ordersBucket   = []byte("orders")
	matchesBucket  = []byte("matches")
	statusKey      = []byte("status")
	baseKey        = []byte("base")
	quoteKey       = []byte("quote")
	orderKey       = []byte("order")
	matchKey       = []byte("match")
	proofKey       = []byte("proof")
	activeKey      = []byte("active")
	dexKey         = []byte("dex")
	updateTimeKey  = []byte("utime")
	accountKey     = []byte("account")
	byteTrue       = encode.ByteTrue
	byteFalse      = encode.ByteFalse
	byteEpoch      = uint16Bytes(uint16(order.OrderStatusEpoch))
	byteBooked     = uint16Bytes(uint16(order.OrderStatusBooked))
)

// boltDB is a bbolt-based database backend for a DEX client. boltDB satisfies
// the db.DB interface defined at decred.org/dcrdex/client/db.
type boltDB struct {
	*bolt.DB
}

// Check that boltDB satisfies the db.DB interface.
var _ dexdb.DB = (*boltDB)(nil)

// NewDB is a constructor for a *boltDB.
func NewDB(ctx context.Context, dbPath string) (dexdb.DB, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	// Release the file lock on exit.
	go func() {
		<-ctx.Done()
		db.Close()
	}()

	bdb := &boltDB{
		DB: db,
	}

	return bdb, bdb.makeTopLevelBuckets([][]byte{accountsBucket, ordersBucket, matchesBucket})
}

// ListAccounts returns a list of DEX URLs. The DB is designed to have a single
// account per DEX, so the account itself is identified by the DEX URL.
func (db *boltDB) ListAccounts() []string {
	var urls []string
	db.acctsView(func(accts *bolt.Bucket) error {
		c := accts.Cursor()
		// key, _ := c.First()
		for acct, _ := c.First(); acct != nil; acct, _ = c.Next() {
			acctBkt := accts.Bucket(acct)
			if acctBkt == nil {
				return fmt.Errorf("account bucket %s value not a nested bucket", string(acct))
			}
			if bEqual(acctBkt.Get(activeKey), byteTrue) {
				urls = append(urls, string(acct))
			}
		}
		return nil
	})
	return urls
}

// Account gets the AccountInfo associated with the specified DEX address.
func (db *boltDB) Account(url string) (*dexdb.AccountInfo, error) {
	var err error
	var acctInfo *dexdb.AccountInfo
	acctKey := []byte(url)
	db.acctsView(func(accts *bolt.Bucket) error {
		acct := accts.Bucket(acctKey)
		if acct == nil {
			return fmt.Errorf("account not found for %s", url)
		}
		acctB := acct.Get(accountKey)
		if acct == nil {
			return fmt.Errorf("empty account found for %s", url)
		}
		acctInfo, err = dexdb.DecodeAccountInfo(bCopy(acctB))
		if err != nil {
			return err
		}
		return err
	})
	return acctInfo, err
}

// CreateAccount saves the AccountInfo. If an account already exists for this
// DEX, it will be overwritten without indication.
func (db *boltDB) CreateAccount(ai *dexdb.AccountInfo) error {
	if ai.URL == "" {
		return fmt.Errorf("empty URL not allowed")
	}
	if ai.DEXPubKey == nil {
		return fmt.Errorf("nil DEXPubKey not allowed")
	}
	if len(ai.EncKey) == 0 {
		return fmt.Errorf("zero-length EncKey not allowed")
	}
	var err error
	db.acctsUpdate(func(accts *bolt.Bucket) error {
		acct, err := accts.CreateBucketIfNotExists([]byte(ai.URL))
		if err != nil {
			return fmt.Errorf("failed to create account bucket")
		}
		err = acct.Put(accountKey, ai.Encode())
		if err != nil {
			return fmt.Errorf("accountKey put error: %v", err)
		}
		err = acct.Put(activeKey, byteTrue)
		if err != nil {
			return fmt.Errorf("activeKey put error: %v", err)
		}
		return err
	})
	return err
}

// acctsView is a convenience function for reading from the account bucket.
func (db *boltDB) acctsView(f bucketFunc) error {
	return db.withBucket(accountsBucket, db.View, f)
}

// acctsUpdate is a convenience function for updating the account bucket.
func (db *boltDB) acctsUpdate(f bucketFunc) error {
	return db.withBucket(accountsBucket, db.Update, f)
}

// UpdateOrder saves the order information in the database. Any existing order
// info for the same order ID will be overwritten without indication.
func (db *boltDB) UpdateOrder(m *dexdb.MetaOrder) error {
	ord, md := m.Order, m.MetaData
	if md.DEX == "" {
		return fmt.Errorf("empty DEX not allowed")
	}
	if len(md.Proof.DEXSig) == 0 {
		return fmt.Errorf("cannot save order without DEX signature")
	}
	var err error
	db.ordersUpdate(func(master *bolt.Bucket) error {
		oid := ord.ID()
		oBkt, err := master.CreateBucketIfNotExists(oid[:])
		if err != nil {
			return fmt.Errorf("order bucket error: %v", err)
		}
		oBkt.Put(baseKey, uint32Bytes(ord.Base()))
		oBkt.Put(quoteKey, uint32Bytes(ord.Quote()))
		oBkt.Put(statusKey, uint16Bytes(uint16(md.Status)))
		oBkt.Put(dexKey, []byte(md.DEX))
		oBkt.Put(updateTimeKey, uint64Bytes(timeNow()))
		oBkt.Put(proofKey, md.Proof.Encode())
		oBkt.Put(orderKey, encode.EncodeOrder(ord))
		return err
	})
	return err
}

// ActiveOrders retrieves all orders which appear to be in an active state,
// which is either in the epoch queue or in the order book.
func (db *boltDB) ActiveOrders() ([]*dexdb.MetaOrder, error) {
	return db.filteredOrders(func(oBkt *bolt.Bucket) bool {
		status := oBkt.Get(statusKey)
		return bEqual(status, byteEpoch) || bEqual(status, byteBooked)
	})
}

// AccountOrders retrieves all orders associated with the specified DEX.
func (db *boltDB) AccountOrders(dex string, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	dexB := []byte(dex)
	if n == 0 && since == 0 {
		return db.filteredOrders(func(oBkt *bolt.Bucket) bool {
			return bEqual(dexB, oBkt.Get(dexKey))
		})
	}
	sinceB := uint64Bytes(since)
	return db.newestOrders(n, func(oBkt *bolt.Bucket) bool {
		timeB := oBkt.Get(updateTimeKey)
		return bEqual(dexB, oBkt.Get(dexKey)) && bytes.Compare(timeB, sinceB) >= 0
	})
}

// MarketOrders retrieves all orders for the specified DEX and market.
func (db *boltDB) MarketOrders(dex string, base, quote uint32, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	dexB := []byte(dex)
	baseB := uint32Bytes(base)
	quoteB := uint32Bytes(quote)
	if n == 0 && since == 0 {
		return db.marketOrdersAll(dexB, baseB, quoteB)
	}
	return db.marketOrdersSince(dexB, baseB, quoteB, n, since)
}

// marketOrdersAll retrieves all orders for the specified DEX and market.
func (db *boltDB) marketOrdersAll(dexB, baseB, quoteB []byte) ([]*dexdb.MetaOrder, error) {
	return db.filteredOrders(func(oBkt *bolt.Bucket) bool {
		return bEqual(dexB, oBkt.Get(dexKey)) && bEqual(baseB, oBkt.Get(baseKey)) &&
			bEqual(quoteB, oBkt.Get(quoteKey))
	})
}

// marketOrdersSince grabs market orders with optional filters for maximum count
// and age. The sort order is always newest first. If n is 0, there is no limit
// to the number of orders returned. If using with both n = 0, and since = 0,
// use marketOrdersAll instead.
func (db *boltDB) marketOrdersSince(dexB, baseB, quoteB []byte, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	sinceB := uint64Bytes(since)
	return db.newestOrders(n, func(oBkt *bolt.Bucket) bool {
		timeB := oBkt.Get(updateTimeKey)
		return bEqual(dexB, oBkt.Get(dexKey)) && bEqual(baseB, oBkt.Get(baseKey)) &&
			bEqual(quoteB, oBkt.Get(quoteKey)) && bytes.Compare(timeB, sinceB) >= 0
	})
}

// orderTimePair is used to build an on-the-fly index to sort orders by time.
type orderTimePair struct {
	oid []byte
	t   uint64
}

// newestOrders returns the n newest orders, filtered with a supplied filter
// function. Each order's bucket is provided to the filter, and a boolean true
// return value indicates the order should is eligible to be decoded and
// returned.
func (db *boltDB) newestOrders(n int, filter func(*bolt.Bucket) bool) ([]*dexdb.MetaOrder, error) {
	var orders []*dexdb.MetaOrder
	db.ordersView(func(master *bolt.Bucket) error {
		pairs := make([]*orderTimePair, 0, n)
		master.ForEach(func(oid, _ []byte) error {
			oBkt := master.Bucket(oid)
			timeB := oBkt.Get(updateTimeKey)
			if filter(oBkt) {
				pairs = append(pairs, &orderTimePair{
					oid: oid,
					t:   intCoder.Uint64(timeB),
				})
			}
			return nil
		})
		// Sort the pairs newest first.
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].t > pairs[j].t })
		if n > 0 && len(pairs) > n {
			pairs = pairs[:n]
		}

		for _, pair := range pairs {
			oBkt := master.Bucket(pair.oid)
			o, err := decodeOrderBucket(pair.oid, oBkt)
			if err != nil {
				return err
			}
			orders = append(orders, o)
		}
		return nil
	})
	return orders, nil
}

// filteredOrders gets all orders that pass the provided filter function. Each
// order's bucket is provided to the filter, and a boolean true return value
// indicates the order should be decoded and returned.
func (db *boltDB) filteredOrders(filter func(*bolt.Bucket) bool) ([]*dexdb.MetaOrder, error) {
	var orders []*dexdb.MetaOrder
	var err error
	db.ordersView(func(master *bolt.Bucket) error {
		master.ForEach(func(oid, _ []byte) error {
			oBkt := master.Bucket(oid)
			if oBkt == nil {
				return fmt.Errorf("order %x bucket is not a bucket", oid)
			}
			if filter(oBkt) {
				o, err := decodeOrderBucket(oid, oBkt)
				if err != nil {
					return err
				}
				orders = append(orders, o)
			}
			return nil
		})
		return nil
	})
	return orders, err
}

// Order fetches a MetaOrder by order ID.
func (db *boltDB) Order(oid order.OrderID) (mord *dexdb.MetaOrder, err error) {
	oidB := oid[:]
	err = db.ordersView(func(master *bolt.Bucket) error {
		oBkt := master.Bucket(oidB)
		if oBkt == nil {
			return fmt.Errorf("order %s not found", oid)
		}
		var err error
		mord, err = decodeOrderBucket(oidB, oBkt)
		return err
	})
	return mord, err
}

// decodeOrderBucket decodes the order's *bolt.Bucket into a *MetaOrder.
func decodeOrderBucket(oid []byte, oBkt *bolt.Bucket) (*dexdb.MetaOrder, error) {
	var ord order.Order
	orderB := oBkt.Get(orderKey)
	if orderB == nil {
		return nil, fmt.Errorf("nil order bytes for order %x", oid)
	}
	ord, err := encode.DecodeOrder(bCopy(orderB))
	if err != nil {
		return nil, fmt.Errorf("error decoding order %x: %v", oid, err)
	}
	proofB := oBkt.Get(proofKey)
	if proofB == nil {
		return nil, fmt.Errorf("nil proof for order %x", oid)
	}
	proof, err := dexdb.DecodeOrderProof(bCopy(proofB))
	if err != nil {
		return nil, fmt.Errorf("error decoding order proof for %x: %v", oid, err)
	}
	return &dexdb.MetaOrder{
		MetaData: &dexdb.OrderMetaData{
			Proof:  *proof,
			Status: order.OrderStatus(intCoder.Uint16(oBkt.Get(statusKey))),
			DEX:    string(oBkt.Get(dexKey)),
		},
		Order: ord,
	}, nil
}

// ordersView is a convenience function for reading from the order bucket.
func (db *boltDB) ordersView(f bucketFunc) error {
	return db.withBucket(ordersBucket, db.View, f)
}

// ordersUpdate is a convenience function for updating the order bucket.
func (db *boltDB) ordersUpdate(f bucketFunc) error {
	return db.withBucket(ordersBucket, db.Update, f)
}

// UpdateMatch updates the match information in the database. Any existing
// entry for the same match ID will be overwritten without indication.
func (db *boltDB) UpdateMatch(m *dexdb.MetaMatch) error {
	match, md := m.Match, m.MetaData
	if md.Quote == 0 && md.Base == 0 {
		return fmt.Errorf("only one of base or quote asset can be asset ID 0 = bitcoin")
	}
	if md.DEX == "" {
		return fmt.Errorf("empty DEX not allowed")
	}
	var err error
	db.matchesUpdate(func(master *bolt.Bucket) error {
		mBkt, err := master.CreateBucketIfNotExists(match.MatchID[:])
		if err != nil {
			return fmt.Errorf("order bucket error: %v", err)
		}
		mBkt.Put(baseKey, uint32Bytes(md.Base))
		mBkt.Put(quoteKey, uint32Bytes(md.Quote))
		mBkt.Put(statusKey, []byte{byte(md.Status)})
		mBkt.Put(dexKey, []byte(md.DEX))
		mBkt.Put(updateTimeKey, uint64Bytes(timeNow()))
		mBkt.Put(proofKey, md.Proof.Encode())
		mBkt.Put(matchKey, encode.EncodeMatch(match))
		return err
	})
	return err
}

// ActiveMatches retrieves the matches that are in an active state, which is
// any state except order.MatchComplete.
func (db *boltDB) ActiveMatches() ([]*dexdb.MetaMatch, error) {
	return db.filteredMatches(func(mBkt *bolt.Bucket) bool {
		status := mBkt.Get(statusKey)
		return len(status) != 1 || status[0] != uint8(order.MatchComplete)
	})
}

// filteredMatches gets all matches that pass the provided filter function. Each
// match's bucket is provided to the filter, and a boolean true return value
// indicates the match should be decoded and returned.
func (db *boltDB) filteredMatches(filter func(*bolt.Bucket) bool) ([]*dexdb.MetaMatch, error) {
	var matches []*dexdb.MetaMatch
	var err error
	db.matchesView(func(master *bolt.Bucket) error {
		master.ForEach(func(k, _ []byte) error {
			mBkt := master.Bucket(k)
			if mBkt == nil {
				return fmt.Errorf("match %x bucket is not a bucket", k)
			}
			if filter(mBkt) {
				var match *order.UserMatch
				var proof *dexdb.MatchProof
				matchB := mBkt.Get(matchKey)
				if matchB == nil {
					return fmt.Errorf("nil match bytes for %x", k)
				}
				match, err = encode.DecodeMatch(bCopy(matchB))
				if err != nil {
					return fmt.Errorf("error decoding match %x: %v", k, err)
				}
				proofB := mBkt.Get(proofKey)
				if len(proofB) == 0 {
					return fmt.Errorf("empty proof")
				}
				proof, err = dexdb.DecodeMatchProof(bCopy(proofB))
				if err != nil {
					return fmt.Errorf("error decoding proof: %v", err)
				}
				statusB := mBkt.Get(statusKey)
				if len(statusB) != 1 {
					return fmt.Errorf("expected status length 1, got %d", len(statusB))
				}
				matches = append(matches, &dexdb.MetaMatch{
					MetaData: &dexdb.MatchMetaData{
						Proof:  *proof,
						Status: order.MatchStatus(statusB[0]),
						DEX:    string(mBkt.Get(dexKey)),
						Base:   intCoder.Uint32(mBkt.Get(baseKey)),
						Quote:  intCoder.Uint32(mBkt.Get(quoteKey)),
					},
					Match: match,
				})
			}
			return nil
		})
		return nil
	})
	return matches, err
}

// matchesView is a convenience function for reading from the match bucket.
func (db *boltDB) matchesView(f bucketFunc) error {
	return db.withBucket(matchesBucket, db.View, f)
}

// matchesUpdate is a convenience function for updating the match bucket.
func (db *boltDB) matchesUpdate(f bucketFunc) error {
	return db.withBucket(matchesBucket, db.Update, f)
}

// makeTopLevelBuckets creates a top-level bucket for each of the provided keys,
// if the bucket doesn't already exist.
func (db *boltDB) makeTopLevelBuckets(buckets [][]byte) error {
	var err error
	db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range buckets {
			_, err = tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// withBucket is a creates a view into a (probably nested) bucket. The viewer
// can be read-only (db.View), or read-write (db.Update). The provided
// bucketFunc will be called with the requested bucket as its only argument.
func (db *boltDB) withBucket(bkt []byte, viewer txFunc, f bucketFunc) error {
	var err error
	viewer(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bkt)
		if bucket == nil {
			return fmt.Errorf("failed to open %s bucket", string(bkt))
		}
		err = f(bucket)
		return err
	})
	return err
}

// timeNow is the current unix timestamp in milliseconds.
func timeNow() uint64 {
	t := time.Now()
	return uint64(t.Unix()*1e3) + uint64(t.Nanosecond())/1e6
}

// A couple of common bbolt functions.
type bucketFunc func(*bolt.Bucket) error
type txFunc func(func(*bolt.Tx) error) error
