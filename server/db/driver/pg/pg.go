// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrdex/server/asset"
	"github.com/decred/dcrdex/server/market/types"
)

// type Driver struct{}

// func (d *Driver) Open(ctx context.Context, cfg interface{}) (db.DEXArchivist, error) {
// 	switch cfg.(type) {
// 	case *Config:
// 		return NewArchiver(ctx, cfg)
// 	case Config:
// 		return NewArchiver(ctx, &cfg)
// 	default:
// 		return nil, fmt.Errorf("invalid config type %t", cfg)
// 	}
// }

// func init() {
// 	db.Register("pg", &Driver{})
// }

const (
	defaultQueryTimeout = 20 * time.Minute
)

// Config holds the Archiver's configuration.
type Config struct {
	Host, Port, User, Pass, DBName string
	HidePGConfig                   bool
	QueryTimeout                   time.Duration

	// MarketCfg specifies all of the markets that the Archiver should prepare.
	MarketCfg []*types.MarketInfo

	// CheckedStores checks the tables for existing identical entires before
	// attempting to store new data. This will should not be needed if there are
	// no bugs...
	CheckedStores bool

	// Net is the current network, and can be one of mainnet, testnet, or simnet.
	Net asset.Network

	// FeeKey is base58-encoded extended public key that will be used for
	// generating fee payment addresses.
	FeeKey string
}

// Some frequently used long-form table names.
type archiverTables struct {
	feeKeys  string
	accounts string
}

// Archiver must implement server/db.DEXArchivist.
// So far: OrderArchiver, AccountArchiver.
type Archiver struct {
	ctx           context.Context
	queryTimeout  time.Duration
	db            *sql.DB
	dbName        string
	checkedStores bool
	markets       map[string]*types.MarketInfo
	feeKeyBranch  *hdkeychain.ExtendedKey
	keyHash       []byte // Store the hash to ref the counter table.
	keyParams     *chaincfg.Params
	tables        archiverTables
}

// NewArchiver constructs a new Archiver. Use Close when done with the Archiver.
func NewArchiver(ctx context.Context, cfg *Config) (*Archiver, error) {
	// Connect to the PostgreSQL daemon and return the *sql.DB.
	db, err := connect(cfg.Host, cfg.Port, cfg.User, cfg.Pass, cfg.DBName)
	if err != nil {
		return nil, err
	}

	// Put the PostgreSQL time zone in UTC.
	var initTZ string
	initTZ, err = checkCurrentTimeZone(db)
	if err != nil {
		return nil, err
	}
	if initTZ != "UTC" {
		log.Infof("Switching PostgreSQL time zone to UTC for this session.")
		if _, err = db.Exec(`SET TIME ZONE UTC`); err != nil {
			return nil, fmt.Errorf("Failed to set time zone to UTC: %v", err)
		}
	}

	// Display the postgres version.
	pgVersion, err := retrievePGVersion(db)
	if err != nil {
		return nil, err
	}
	log.Info(pgVersion)

	queryTimeout := cfg.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}

	mktMap := make(map[string]*types.MarketInfo, len(cfg.MarketCfg))
	for _, mkt := range cfg.MarketCfg {
		mktMap[mkt.Name] = mkt
	}

	archiver := &Archiver{
		ctx:           ctx,
		db:            db,
		dbName:        cfg.DBName,
		queryTimeout:  queryTimeout,
		markets:       mktMap,
		checkedStores: cfg.CheckedStores,
		tables: archiverTables{
			feeKeys:  fullTableName(cfg.DBName, publicSchema, feeKeysTableName),
			accounts: fullTableName(cfg.DBName, publicSchema, accountsTableName),
		},
	}

	// Check critical performance-related settings.
	if err = archiver.checkPerfSettings(cfg.HidePGConfig); err != nil {
		return nil, err
	}

	// Ensure all tables required by the current market configuration are ready.
	if err = PrepareTables(db, cfg.MarketCfg); err != nil {
		return nil, err
	}

	switch cfg.Net {
	case asset.Mainnet:
		archiver.keyParams = chaincfg.MainNetParams()
	case asset.Testnet:
		archiver.keyParams = chaincfg.TestNet3Params()
	case asset.Simnet:
		archiver.keyParams = chaincfg.SimNetParams()
	default:
		return nil, fmt.Errorf("unkown network %d", cfg.Net)
	}

	// Get the master extended public key.
	masterKey, err := hdkeychain.NewKeyFromString(cfg.FeeKey, archiver.keyParams)
	if err != nil {
		return nil, fmt.Errorf("error parsing  master pubkey: %v", err)
	}

	// Get the external branch (= child 0) of the extended pubkey.
	archiver.feeKeyBranch, err = masterKey.Child(0)
	if err != nil {
		return nil, fmt.Errorf("error creating external branch: %v", err)
	}

	// Get a hash of the serialized secp256k1.PublicKey to create an identifying
	// hash of this key for the child counter DB table.
	pubKey, err := archiver.feeKeyBranch.ECPubKey()
	if err != nil {
		return nil, fmt.Errorf("error getting pubkey from extended key: %v", err)
	}
	archiver.keyHash = dcrutil.Hash160(pubKey.Serialize())
	if err = archiver.CreateKeyEntry(archiver.keyHash); err != nil {
		return nil, err
	}

	return archiver, nil
}

// Close closes the underlying DB connection.
func (a *Archiver) Close() error {
	return a.db.Close()
}
