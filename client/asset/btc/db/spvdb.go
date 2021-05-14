// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"go.etcd.io/bbolt"
)

const (
	DBVersion = 0
)

// getCopy returns a copy of the value for the given key and provided bucket. If
// the key is not found or if an empty slice was loaded, nil is returned. Thus,
// use bkt.Get(key) == nil directly to test for existence of the key. This
// function should be used instead of bbolt.(*Bucket).Get when the read value
// needs to be kept after the transaction, at which time the buffer from Get is
// no longer safe to use.
func getCopy(bkt *bbolt.Bucket, key []byte) []byte {
	b := bkt.Get(key)
	if len(b) == 0 {
		return nil
	}
	val := make([]byte, len(b))
	copy(val, b)
	return val
}

// Short names for some commonly used imported functions.
var (
	intCoder    = encode.IntCoder
	bEqual      = bytes.Equal
	uint16Bytes = encode.Uint16Bytes
	uint32Bytes = encode.Uint32Bytes
	uint64Bytes = encode.Uint64Bytes
)

// Bolt works on []byte keys and values. These are some commonly used key and
// value encodings.
var (
	appBucket      = []byte("appBucket")
	txBlockBucket  = []byte("txBlock")
	spendsBucket   = []byte("spends")
	pkScriptBucket = []byte("pkScript")
	// redeemScriptBucket = []byte("redeemScript")
	versionKey = []byte("version")
	byteTrue   = encode.ByteTrue
	byteFalse  = encode.ByteFalse
)

// BoltDB is a bbolt-based database backend for a DEX client. BoltDB satisfies
// the db.DB interface defined at decred.org/dcrdex/client/db.
type SPVDB struct {
	*bbolt.DB
	log dex.Logger
}

// Check that BoltDB satisfies the db.DB interface.
// var _ dexdb.DB = (*BoltDB)(nil)

// NewDB is a constructor for a *BoltDB.
func NewDB(dbPath string, logger dex.Logger) (*SPVDB, error) {
	_, err := os.Stat(dbPath)
	isNew := os.IsNotExist(err)

	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	// Release the file lock on exit.
	bdb := &SPVDB{
		DB:  db,
		log: logger,
	}

	err = bdb.makeTopLevelBuckets([][]byte{appBucket, txBlockBucket, spendsBucket, pkScriptBucket})
	if err != nil {
		return nil, err
	}

	// If the db is a new one, initialize it with the current DB version.
	if isNew {
		err := bdb.DB.Update(func(dbTx *bbolt.Tx) error {
			bkt := dbTx.Bucket(appBucket)
			if bkt == nil {
				return fmt.Errorf("app bucket not found")
			}

			versionB := encode.Uint32Bytes(DBVersion)
			err := bkt.Put(versionKey, versionB)
			if err != nil {
				return err
			}

			bdb.log.Infof("creating new version %d database", DBVersion)

			return nil
		})
		if err != nil {
			return nil, err
		}

		return bdb, nil
	}

	return bdb, nil
}

// Run waits for context cancellation and closes the database.
func (db *SPVDB) Run(ctx context.Context) {
	<-ctx.Done()
	// err := db.Backup()
	// if err != nil {
	// 	db.log.Errorf("unable to backup database: %v", err)
	// }
	db.Close()
}

// makeTopLevelBuckets creates a top-level bucket for each of the provided keys,
// if the bucket doesn't already exist.
func (db *SPVDB) makeTopLevelBuckets(buckets [][]byte) error {
	return db.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range buckets {
			_, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

func (db *SPVDB) storeKeyAtValueInBucket(bucket, key, value []byte) error {
	return db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(bucket)
		if bkt == nil {
			return fmt.Errorf("no tx-block bucket")
		}
		return bkt.Put(key[:], value[:])
	})
}

func (db *SPVDB) getValueAtKeyInBucket(bucket, key []byte) (value []byte, err error) {
	return value, db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(bucket)
		if bkt == nil {
			return fmt.Errorf("no %s bucket", string(bucket))
		}
		value = bkt.Get(key)
		return nil
	})
}

func (db *SPVDB) getHashAtKey(bucket []byte, keyHash []byte) (valueHash *chainhash.Hash, err error) {
	b, err := db.getValueAtKeyInBucket(bucket, keyHash[:])
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, nil
	}
	var h chainhash.Hash
	copy(h[:], b)
	return &h, nil
}

func (db *SPVDB) SaveTransactionBlock(txHash, blockHash chainhash.Hash) error {
	return db.storeKeyAtValueInBucket(txBlockBucket, txHash[:], blockHash[:])
}

func (db *SPVDB) GetTransactionBlock(txHash chainhash.Hash) (*chainhash.Hash, error) {
	return db.getHashAtKey(txBlockBucket, txHash[:])
}

func (db *SPVDB) SaveSpend(spendee, spender chainhash.Hash) error {
	return db.storeKeyAtValueInBucket(spendsBucket, spendee[:], spender[:])
}

func (db *SPVDB) GetSpend(spendee chainhash.Hash) (*chainhash.Hash, error) {
	return db.getHashAtKey(spendsBucket, spendee[:])
}

func (db *SPVDB) SavePkScript(op *wire.OutPoint, script []byte) error {
	return db.storeKeyAtValueInBucket(pkScriptBucket, outpointKey(op), script[:])
}

func (db *SPVDB) GetPkScript(op *wire.OutPoint) ([]byte, error) {
	return db.getValueAtKeyInBucket(pkScriptBucket, outpointKey(op))
}

func outpointKey(op *wire.OutPoint) []byte {
	b := make([]byte, 36)
	copy(b[:32], op.Hash[:])
	copy(b[32:36], uint32Bytes(op.Index))
	return b
}
