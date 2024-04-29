// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/utils"
	"github.com/dgraph-io/badger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// extendedWalletTx is an asset.WalletTransaction extended with additional
// fields used for tracking transactions.
type extendedWalletTx struct {
	*asset.WalletTransaction
	BlockSubmitted uint64         `json:"blockSubmitted"`
	SubmissionTime uint64         `json:"timeStamp"` // seconds
	Nonce          *big.Int       `json:"nonce"`
	Receipt        *types.Receipt `json:"receipt,omitempty"`
	RawTx          dex.Bytes      `json:"rawTx"`
	// NonceReplacement is a transaction with the same nonce that was accepted
	// by the network, meaning this tx was not applied.
	NonceReplacement string `json:"nonceReplacement,omitempty"`
	// FeeReplacement is a transaction that replaced this transaction but with
	// higher fees. The FeeReplacement is always the NonceReplacement too,
	// but the FeeReplacement can be assumed to otherwise be the same tx as
	// the one replaced, just with higher fees.
	FeeReplacement string `json:"feeReplacement,omitempty"`
	// AssumedLost will be set to true if a transaction is assumed to be lost.
	// This typically requires feedback from the user in response to an
	// ActionRequiredNote.
	AssumedLost bool `json:"assumedLost,omitempty"`

	txHash          common.Hash
	lastCheck       uint64
	savedToDB       bool
	lastBroadcast   time.Time
	lastFeeCheck    time.Time
	actionRequested bool
}

func (t *extendedWalletTx) age() time.Duration {
	return time.Since(time.Unix(int64(t.SubmissionTime), 0))
}

func (t *extendedWalletTx) tx() (*types.Transaction, error) {
	tx := new(types.Transaction)
	return tx, tx.UnmarshalBinary(t.RawTx)
}

var (
	// noncePrefix is the prefix for the key used to map a nonce to an
	// extendedWalletTx.
	noncePrefix = []byte("nonce-")
	// txHashPrefix is the prefix for the key used to map a transaction hash
	// to a nonce key.
	txHashPrefix = []byte("txHash-")
	// dbVersionKey is the key used to store the database version.
	dbVersionKey = []byte("dbVersion")
)

func nonceKey(nonce uint64) []byte {
	key := make([]byte, len(noncePrefix)+8)
	copy(key, noncePrefix)
	binary.BigEndian.PutUint64(key[len(noncePrefix):], nonce)
	return key
}

func txKey(txHash common.Hash) []byte {
	key := make([]byte, len(txHashPrefix)+20)
	copy(key, txHashPrefix)
	copy(key[len(txHashPrefix):], txHash[:])
	return key
}

var maxNonceKey = nonceKey(math.MaxUint64)

// initialDBVersion only contained mappings from txHash -> monitoredTx.
// const initialDBVersion = 0

// prefixDBVersion contains two mappings each marked with a prefix:
//
//	nonceKey -> extendedWalletTx (noncePrefix)
//	txHash -> nonceKey (txHashPrefix)
// const prefixDBVersion = 1

// txMappingVersion reverses the semantics so that all txs are accessible
// by txHash.
//
// nonceKey -> best-known txHash
// txHash -> extendedWalletTx, which contains a nonce
const txMappingVersion = 2

const txDBVersion = txMappingVersion

type txDB interface {
	storeTx(wt *extendedWalletTx) error
	getTxs(n int, refID *common.Hash, past bool, tokenID *uint32) ([]*asset.WalletTransaction, error)
	// getTx gets a single transaction. It is not an error if the tx is not known.
	// In that case, a nil tx is returned.
	getTx(txHash common.Hash) (*extendedWalletTx, error)
	// getPendingTxs returns any recent txs that are not confirmed, ordered
	// by nonce lowest-first.
	getPendingTxs() ([]*extendedWalletTx, error)
	close() error
	run(context.Context)
}

type badgerTxDB struct {
	*badger.DB
	log dex.Logger
}

var _ txDB = (*badgerTxDB)(nil)

// badgerLoggerWrapper wraps dex.Logger and translates Warnf to Warningf to
// satisfy badger.Logger. It also lowers the log level of Infof to Debugf
// and Debugf to Tracef.
type badgerLoggerWrapper struct {
	dex.Logger
}

var _ badger.Logger = (*badgerLoggerWrapper)(nil)

// Debugf -> dex.Logger.Tracef
func (log *badgerLoggerWrapper) Debugf(s string, a ...interface{}) {
	log.Tracef(s, a...)
}

// Infof -> dex.Logger.Debugf
func (log *badgerLoggerWrapper) Infof(s string, a ...interface{}) {
	log.Debugf(s, a...)
}

// Warningf -> dex.Logger.Warnf
func (log *badgerLoggerWrapper) Warningf(s string, a ...interface{}) {
	log.Warnf(s, a...)
}

func newBadgerTxDB(filePath string, log dex.Logger) (*badgerTxDB, error) {
	// If memory use is a concern, could try
	//   .WithValueLogLoadingMode(options.FileIO) // default options.MemoryMap
	//   .WithMaxTableSize(sz int64); // bytes, default 6MB
	//   .WithValueLogFileSize(sz int64), bytes, default 1 GB, must be 1MB <= sz <= 1GB
	opts := badger.DefaultOptions(filePath).WithLogger(&badgerLoggerWrapper{log})
	db, err := badger.Open(opts)
	if err == badger.ErrTruncateNeeded {
		// Probably a Windows thing.
		// https://github.com/dgraph-io/badger/issues/744
		log.Warnf("error opening badger db: %v", err)
		// Try again with value log truncation enabled.
		opts.Truncate = true
		log.Warnf("Attempting to reopen badger DB with the Truncate option set...")
		db, err = badger.Open(opts)
	}
	if err != nil {
		return nil, err
	}

	txDB := &badgerTxDB{
		DB:  db,
		log: log,
	}

	err = txDB.updateVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to update db: %w", err)
	}

	return txDB, nil
}

// txForNonce gets the registered for the given nonce.
func txForNonce(txn *badger.Txn, nonce uint64) (tx *extendedWalletTx, err error) {
	nk := nonceKey(nonce)
	txHashi, err := txn.Get(nk)
	if err != nil {
		return nil, err
	}
	return tx, txHashi.Value(func(txHashB []byte) error {
		var txHash common.Hash
		copy(txHash[:], txHashB)
		txi, err := txn.Get(txKey(txHash))
		if err != nil {
			return err
		}
		return txi.Value(func(wtB []byte) error {
			tx, err = unmarshalTx(wtB)
			return err
		})
	})
}

// txForHash get the extendedWalletTx at the given tx hash and checks for any
// unsaved nonce replacement.
func txForHash(txn *badger.Txn, txHash common.Hash) (wt *extendedWalletTx, err error) {
	txi, err := txn.Get(txKey(txHash))
	if err != nil {
		return nil, err
	}
	return wt, txi.Value(func(wtB []byte) error {
		wt, err = unmarshalTx(wtB)
		if err != nil || wt.Confirmed || wt.NonceReplacement != "" {
			return err
		}
		nonceTx, err := txForNonce(txn, wt.Nonce.Uint64())
		if err != nil {
			return err
		}
		if nonceTx.txHash != wt.txHash && nonceTx.Confirmed {
			wt.NonceReplacement = wt.txHash.String()
		}
		return nil
	})
}

// updateVersion updates the DB to the latest version. In version 0,
// only a mapping from txHash to monitoredTx was stored, with no
// prefixes.
func (s *badgerTxDB) updateVersion() error {
	// Check if the database version is stored. If not, the db
	// is version 0.
	var version int
	err := s.View(func(txn *badger.Txn) error {
		item, err := txn.Get(dbVersionKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return err
		}
		return item.Value(func(versionB []byte) error {
			version = int(binary.BigEndian.Uint64(versionB))
			return nil
		})
	})
	if err != nil {
		s.log.Errorf("error retrieving database version: %v", err)
	}

	if version < txMappingVersion {
		if err := s.DB.DropAll(); err != nil {
			return fmt.Errorf("error deleting DB entries for version upgrade: %w", err)
		}
		versionB := make([]byte, 8)
		binary.BigEndian.PutUint64(versionB, txMappingVersion)
		if err = s.Update(func(txn *badger.Txn) error {
			return txn.Set(dbVersionKey, versionB)
		}); err != nil {
			return err
		}
		s.log.Infof("Upgraded DB to version %d by deleting everything and starting from scratch.", txMappingVersion)
	} else if version > txDBVersion {
		return fmt.Errorf("database version %d is not supported", version)
	}

	return nil
}

func (s *badgerTxDB) close() error {
	return s.Close()
}

func (s *badgerTxDB) run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			err := s.RunValueLogGC(0.5)
			if err != nil && !errors.Is(err, badger.ErrNoRewrite) {
				s.log.Errorf("garbage collection error: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// storeTx stores a mapping from txHash to extendedWalletTx. If this tx is
// suitable, it will be marked as the best-known tx for the nonce.
func (s *badgerTxDB) storeTx(wt *extendedWalletTx) error {
	wtB, err := json.Marshal(wt)
	if err != nil {
		return err
	}
	nonce := wt.Nonce.Uint64()
	nk := nonceKey(nonce)
	tk := txKey(wt.txHash)

	return s.Update(func(txn *badger.Txn) error {
		// If there is not a confirmed tx at this tx's nonce, map the nonce
		// to this tx.
		nonceTx, err := txForNonce(txn, wt.Nonce.Uint64())
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("error reading nonce tx: %w", err)
		}
		// If we don't have a tx stored at the nonce or the tx stored at the
		// nonce is not confirmed, put this one there instead, unless this one
		// has been marked as nonce-replaced.
		if (nonceTx == nil || !nonceTx.Confirmed) && wt.NonceReplacement == "" {
			if err := txn.Set(nk, wt.txHash[:]); err != nil {
				return fmt.Errorf("error mapping nonce to tx hash: %w", err)
			}
		}
		// Store the tx at its hash.
		return txn.Set(tk, wtB)
	})
}

// getTx gets a single transaction. It is not an error if the tx is not known.
// In that case, a nil tx is returned.
func (s *badgerTxDB) getTx(txHash common.Hash) (tx *extendedWalletTx, err error) {
	return tx, s.View(func(txn *badger.Txn) error {
		tx, err = txForHash(txn, txHash)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		return err
	})
}

// unmarshalTx attempts to decode the binary tx and sets some unexported fields.
func unmarshalTx(wtB []byte) (wt *extendedWalletTx, err error) {
	if err = json.Unmarshal(wtB, &wt); err != nil {
		return nil, err
	}
	wt.txHash = common.HexToHash(wt.ID)
	wt.lastBroadcast = time.Unix(int64(wt.SubmissionTime), 0)
	wt.savedToDB = true
	return
}

// getTxs returns the n more recent transaction if refID is nil, or the
// n transactions before/after refID depending on the value of past. The
// transactions are returned in reverse chronological order.
// If a non-nil refID is not found, asset.CoinNotFoundError is returned.
func (s *badgerTxDB) getTxs(n int, refID *common.Hash, past bool, tokenID *uint32) ([]*asset.WalletTransaction, error) {
	txs := make([]*asset.WalletTransaction, 0, n)

	return txs, s.View(func(txn *badger.Txn) error {
		startNonceKey := maxNonceKey
		if refID != nil {
			// Get the nonce for the provided tx hash.
			wt, err := txForHash(txn, *refID)
			if err != nil {
				if errors.Is(err, badger.ErrKeyNotFound) {
					return asset.CoinNotFoundError
				}
				return err
			}
			startNonceKey = nonceKey(wt.Nonce.Uint64())
		} else {
			past = true
		}

		opts := badger.DefaultIteratorOptions
		opts.Reverse = past
		opts.Prefix = noncePrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(startNonceKey); it.Valid() && (n <= 0 || len(txs) < n); it.Next() {
			txHashi := it.Item()
			if err := txHashi.Value(func(txHashB []byte) error {
				var txHash common.Hash
				copy(txHash[:], txHashB)
				wt, err := txForHash(txn, txHash)
				if err != nil {
					return err
				}
				if tokenID != nil && (wt.TokenID == nil || *tokenID != *wt.TokenID) {
					return nil
				}
				txs = append(txs, wt.WalletTransaction)
				return nil
			}); err != nil {
				return err
			}
		}
		if !past {
			utils.ReverseSlice(txs)
		}
		return nil
	})
}

// getPendingTxs returns a map of nonce to extendedWalletTx for all
// pending transactions.
func (s *badgerTxDB) getPendingTxs() ([]*extendedWalletTx, error) {
	// We will be iterating backwards from the most recent nonce.
	// If we find numConfirmedTxsToCheck consecutive confirmed transactions,
	// we can stop iterating.
	const numConfirmedTxsToCheck = 20

	txs := make([]*extendedWalletTx, 0, 4)

	err := s.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true
		opts.Prefix = noncePrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		var numConfirmedTxs int
		for it.Seek(maxNonceKey); it.Valid(); it.Next() {
			txHashi := it.Item()
			err := txHashi.Value(func(txHashB []byte) error {
				var txHash common.Hash
				copy(txHash[:], txHashB)
				txi, err := txn.Get(txKey(txHash))
				if err != nil {
					return err
				}
				return txi.Value(func(wtB []byte) error {
					wt, err := unmarshalTx(wtB)
					if err != nil {
						s.log.Errorf("unable to unmarhsal wallet transaction: %s: %v", string(wtB), err)
						return err
					}
					if !wt.Confirmed {
						numConfirmedTxs = 0
						txs = append(txs, wt)
					} else {
						numConfirmedTxs++
						if numConfirmedTxs >= numConfirmedTxsToCheck {
							return nil
						}
					}
					return nil
				})

			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	utils.ReverseSlice(txs)

	return txs, err
}
