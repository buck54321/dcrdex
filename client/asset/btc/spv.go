// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// spvWallet implements a Wallet backed by a built-in btcwallet + Neutrino.
// There are two major challenges presented in using an SPV wallet for DEX.
// 1. Finding non-wallet related blockchain data requires posession of the
//    pubkey script, not just transction hash and output index
// 2. Finding non-wallet related blockchain data can often entail extensive
//    scanning of compact filters. We can limit these scans with more
//    information, but every new scan-limiting filter added entails some
//    substantial logic.
//
// The two most challenging Wallet methods to implement are ..
//
// Confirmations: A new pkScript argument is added. DEX only uses Confirmations
//   in the case of a) registration fee checks (not relevant to BTC yet), or
//   b) confirmation checks on swap outputs, for which we will always have a
//   redeem script to generate the p2sh pubkey script.
// GetTxOut: The GetTxOut interface is re-worked to add two new arguments,
//   pkScript and startTime. In current usage, GetTxOut is only used to look
//   for swap outputs, so the pkScript should always be available. When looking
//   for a swap contract, setting a startTime is critical to limit the search
//   depth.
//
// There is now also a new DB to store come auxiliary data to reduce search
// times on repeated asks. For instance, every time a Neutrino GetUtxo search
// returns a result, we'll store any txHash -> blockHash or txOut ->
// spendingHash associations so that we can possibly short-circut the GetUtxo
// scan the next time info for that output is requested, e.g. in Confirmations.
//
// Confirmations, GetTxOut, and other methods typically follow a three-part
// search. First, check for info in the dcrwallet DB, then the new BTCSPV DB,
// and finally, if we still haven't found it, use a filter scan.

package btc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc/db"
	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/gcs"
	"github.com/btcsuite/btcwallet/chain"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/walletdb"
	_ "github.com/btcsuite/btcwallet/walletdb/bdb"
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
)

const (
	spvAcctName               = "dexspv"
	WalletTransactionNotFound = dex.ErrorKind("wallet transaction not found")
)

// type AddressDecoder func(addr string, net *chaincfg.Params) (btcutil.Address, error)

type btcWallet interface {
	PublishTransaction(tx *wire.MsgTx, label string) error
	CalculateAccountBalances(account uint32, confirms int32) (wallet.Balances, error)
	ListUnspent(minconf, maxconf int32, addresses map[string]struct{}) ([]*btcjson.ListUnspentResult, error)
	FetchInputInfo(prevOut *wire.OutPoint) (*wire.MsgTx, *wire.TxOut, int64, error)
	ResetLockedOutpoints()
	LockOutpoint(op wire.OutPoint)
	UnlockOutpoint(op wire.OutPoint)
	LockedOutpoints() []btcjson.TransactionInput
	NewChangeAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
	NewAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
	SignTransaction(tx *wire.MsgTx, hashType txscript.SigHashType, additionalPrevScriptsadditionalPrevScripts map[wire.OutPoint][]byte,
		additionalKeysByAddress map[string]*btcutil.WIF, p2shRedeemScriptsByAddress map[string][]byte) ([]wallet.SignatureError, error)
	PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error)
	Database() walletdb.DB
	Unlock(passphrase []byte, lock <-chan time.Time) error
	Lock()
	Locked() bool
	SendOutputs(outputs []*wire.TxOut, account uint32, minconf int32, satPerKb btcutil.Amount, label string) (*wire.MsgTx, error)
	HaveAddress(a btcutil.Address) (bool, error)
	Stop()
	WaitForShutdown()
	ChainSynced() bool
	SynchronizeRPC(chainClient chain.Interface)
	walletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error)
	getTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error)
	confirmations(txHash *chainhash.Hash, vout uint32) (confs uint32, spent bool, err error)
}

type neutrinoClient interface {
	GetBlockHash(int64) (*chainhash.Hash, error)
	BestBlock() (*headerfs.BlockStamp, error)
	Peers() []*neutrino.ServerPeer
	GetUtxo(options ...neutrino.RescanOption) (*neutrino.SpendReport, error)
	GetBlockHeight(hash *chainhash.Hash) (int32, error)
	GetBlockHeader(*chainhash.Hash) (*wire.BlockHeader, error)
	GetCFilter(blockHash chainhash.Hash, filterType wire.FilterType, options ...neutrino.QueryOption) (*gcs.Filter, error)
	GetBlock(blockHash chainhash.Hash, options ...neutrino.QueryOption) (*btcutil.Block, error)
}

type spvWallet struct {
	ctx    context.Context
	net    *chaincfg.Params
	wallet btcWallet
	// cl     *neutrino.ChainService
	cl          neutrinoClient
	chainClient *chain.NeutrinoClient
	acctNum     uint32
	walletDB    walletdb.DB
	db          *db.SPVDB
	log         dex.Logger
	loader      *wallet.Loader
}

var _ Wallet = (*spvWallet)(nil)

func newSPVWallet(dbDir string, logger dex.Logger, peers []string, net *chaincfg.Params) (*spvWallet, error) {
	loader, chainService, chainClient, walletDB, err := loadChainClient(&walletConfig{
		DBDir:        dbDir,
		Net:          net,
		ConnectPeers: peers,
	})
	if err != nil {
		return nil, err
	}

	// Could subscribe to block notifications here with a NewRescan -> *Rescan
	// supplied with a QuitChan-type RescanOption.
	// Actually, should use dcrwallet.Wallet.NtfnServer

	w, err := startWallet(loader, chainClient)
	if err != nil {
		return nil, err
	}

	cache, err := db.NewDB(filepath.Join(dbDir, "dexspv.db"), logger.SubLogger("SPVDB"))
	if err != nil {
		return nil, err
	}

	// acctNum, err := w.AccountNumber(waddrmgr.KeyScopeBIP0044, spvAcctName)
	// if err != nil {
	// 	return nil, err
	// }

	return &spvWallet{
		net:         net,
		wallet:      &btcWalletUsher{w},
		cl:          chainService,
		chainClient: chainClient,
		acctNum:     0,
		walletDB:    walletDB,
		db:          cache,
		log:         logger,
		loader:      loader,
	}, nil
}

func (wc *spvWallet) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	// Not needed for spv wallet.
	return nil, fmt.Errorf("unimplemented")
}

func (w *spvWallet) estimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	return nil, fmt.Errorf("EstimateSmartFee not available on spv")
}

func (w *spvWallet) ownsAddress(addr btcutil.Address) (bool, error) {
	return w.wallet.HaveAddress(addr)
}

func (w *spvWallet) sendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	// Fee sanity check?
	err := w.wallet.PublishTransaction(tx, "")
	if err != nil {
		return nil, err
	}
	txHash := tx.TxHash()
	return &txHash, nil
}

func (w *spvWallet) getBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	return w.cl.GetBlockHash(blockHeight)
}

func (w *spvWallet) getBlockHeight(h *chainhash.Hash) (int32, error) {
	return w.cl.GetBlockHeight(h)
}

func (w *spvWallet) getBestBlockHash() (*chainhash.Hash, error) {
	blk, err := w.cl.BestBlock()
	if err != nil {
		return nil, err
	}
	return &blk.Hash, err
}

func (w *spvWallet) getBestBlockHeight() (int32, error) {
	blk, err := w.cl.BestBlock()
	if err != nil {
		return -1, err
	}
	return blk.Height, err
}

// syncHeight is the best known sync height among peers, defined here as the
// most commonly reported tip height among peers, with ties going to the higher
// height.
func (w *spvWallet) syncHeight() int32 {
	var maxHeight int32
	for _, p := range w.cl.Peers() {
		tipHeight := p.StartingHeight()
		lastBlockHeight := p.LastBlock()
		if lastBlockHeight > tipHeight {
			tipHeight = lastBlockHeight
		}
		if tipHeight > maxHeight {
			maxHeight = tipHeight
		}
	}
	return maxHeight
}

func (w *spvWallet) syncStatus() (*syncStatus, error) {
	blk, err := w.cl.BestBlock()
	if err != nil {
		return nil, err
	}

	return &syncStatus{
		Target:  w.syncHeight(),
		Height:  blk.Height,
		Syncing: !w.wallet.ChainSynced(),
	}, nil
}

// Balances retrieves a wallet's balance details.
func (w *spvWallet) balances() (*GetBalancesResult, error) {
	bals, err := w.wallet.CalculateAccountBalances(w.acctNum, 0 /* confs */)
	if err != nil {
		return nil, err
	}

	return &GetBalancesResult{
		Mine: Balances{
			Trusted:   bals.Spendable.ToBTC(),
			Untrusted: 0, // ? do we need to scan utxos instead ?
			Immature:  bals.ImmatureReward.ToBTC(),
		},
	}, nil
}

// ListUnspent retrieves list of the wallet's UTXOs.
func (w *spvWallet) listUnspent() ([]*ListUnspentResult, error) {
	unspents, err := w.wallet.ListUnspent(0, math.MaxInt32, nil)
	if err != nil {
		return nil, err
	}

	res := make([]*ListUnspentResult, 0, len(unspents))
	for _, utxo := range unspents {

		// If the utxo is unconfirmed, we should determine whether it's "safe"
		// by seeing if we control the inputs of it's transaction.
		var safe bool
		if utxo.Confirmations > 1 {
			safe = true
		} else {
			txHash, err := chainhash.NewHashFromStr(utxo.TxID)
			if err != nil {
				return nil, fmt.Errorf("Error decoding txid %q: %v", utxo.TxID, err)
			}
			tx, _, _, err := w.wallet.FetchInputInfo(wire.NewOutPoint(txHash, utxo.Vout))
			if err != nil {
				return nil, fmt.Errorf("FetchInputInfo error: %v", err)
			}
			// To be "safe", we need to show that we own the inputs for the
			// utxo's transaction. We'll just try to find one.
			for _, txIn := range tx.TxIn {
				_, _, _, err := w.wallet.FetchInputInfo(&txIn.PreviousOutPoint)
				if err != nil {
					safe = true
					break
				}
			}
		}

		pkScript, err := hex.DecodeString(utxo.ScriptPubKey)
		if err != nil {
			return nil, err
		}

		redeemScript, err := hex.DecodeString(utxo.RedeemScript)
		if err != nil {
			return nil, err
		}

		res = append(res, &ListUnspentResult{
			TxID:    utxo.TxID,
			Vout:    utxo.Vout,
			Address: utxo.Address,
			// Label: ,
			ScriptPubKey:  pkScript,
			Amount:        utxo.Amount,
			Confirmations: uint32(utxo.Confirmations),
			RedeemScript:  redeemScript,
			Spendable:     utxo.Spendable,
			// Solvable: ,
			Safe: safe,
		})
	}
	return res, nil
}

// lockUnspent locks and unlocks outputs for spending. An output that is part of
// an order, but not yet spent, should be locked until spent or until the order
// is  canceled or fails.
func (w *spvWallet) lockUnspent(unlock bool, ops []*output) error {
	switch {
	case unlock && len(ops) == 0:
		w.wallet.ResetLockedOutpoints()
	default:
		for _, op := range ops {
			op := wire.OutPoint{Hash: op.pt.txHash, Index: op.pt.vout}
			if unlock {
				w.wallet.UnlockOutpoint(op)
			} else {
				w.wallet.LockOutpoint(op)
			}
		}
	}
	return nil
}

// listLockUnspent returns a slice of outpoints for all unspent outputs marked
// as locked by a wallet.
func (w *spvWallet) listLockUnspent() ([]*RPCOutpoint, error) {
	outpoints := w.wallet.LockedOutpoints()
	pts := make([]*RPCOutpoint, 0, len(outpoints))
	for _, pt := range outpoints {
		pts = append(pts, &RPCOutpoint{
			TxID: pt.Txid,
			Vout: pt.Vout,
		})
	}
	return pts, nil
}

// changeAddress gets a new internal address from the wallet. The address will
// be bech32-encoded (P2WPKH).
func (w *spvWallet) changeAddress() (btcutil.Address, error) {
	return w.wallet.NewChangeAddress(w.acctNum, waddrmgr.KeyScopeBIP0044)
}

// AddressPKH gets a new base58-encoded (P2PKH) external address from the
// wallet.
func (w *spvWallet) addressPKH() (btcutil.Address, error) {
	return nil, fmt.Errorf("unimplemented")
}

// addressWPKH gets a new bech32-encoded (P2WPKH) external address from the
// wallet.
func (w *spvWallet) addressWPKH() (btcutil.Address, error) {
	return w.wallet.NewAddress(w.acctNum, waddrmgr.KeyScopeBIP0084)
}

// signTx attempts to have the wallet sign the transaction inputs.
func (w *spvWallet) signTx(tx *wire.MsgTx) (*wire.MsgTx, error) {
	// Make a copy?
	sigErrs, err := w.wallet.SignTransaction(tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if len(sigErrs) > 0 {
		return nil, fmt.Errorf("Signature errors: %+v", sigErrs)
	}
	return tx, nil
}

// privKeyForAddress retrieves the private key associated with the specified
// address.
func (w *spvWallet) privKeyForAddress(addr string) (*btcec.PrivateKey, error) {
	a, err := btcutil.DecodeAddress(addr, w.net)
	if err != nil {
		return nil, err
	}
	return w.wallet.PrivKeyForAddress(a)
}

// Unlock unlocks the wallet.
func (w *spvWallet) Unlock(pass string) error {
	return w.wallet.Unlock([]byte(pass), time.After(time.Duration(math.MaxInt64)))
}

// Lock locks the wallet.
func (w *spvWallet) Lock() error {
	w.wallet.Lock()
	return nil
}

// sendToAddress sends the amount to the address. feeRate is in units of
// atoms/byte.
func (w *spvWallet) sendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error) {
	addr, err := btcutil.DecodeAddress(address, w.net)
	if err != nil {
		return nil, err
	}

	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, err
	}

	wireOP := wire.NewTxOut(int64(value), pkScript)

	feeRateAmt, err := btcutil.NewAmount(float64(feeRate) / 1e5)
	if err != nil {
		return nil, err
	}

	// Could try with minconf 1 first.
	tx, err := w.wallet.SendOutputs([]*wire.TxOut{wireOP}, w.acctNum, 0, feeRateAmt, "")
	if err != nil {
		return nil, err
	}

	txHash := tx.TxHash()

	return &txHash, nil
}

func (w *spvWallet) OwnsAddress(address string) (bool, error) {
	addr, err := btcutil.DecodeAddress(address, w.net)
	if err != nil {
		return false, err
	}
	return w.wallet.HaveAddress(addr)
}

func (w *spvWallet) swapConfirmations(txHash *chainhash.Hash, vout uint32, pkScript []byte, startTime time.Time) (confs uint32, err error) {
	// First, check if it's a wallet transaction.

	//

	//

	//

	confs, spent, err := w.wallet.confirmations(txHash, vout)
	if err != nil && !errors.Is(err, WalletTransactionNotFound) {
		return 0, err
	}
	if err == nil {
		if spent {
			return 0, asset.ErrSpentSwap
		}
		return confs, nil
	}

	// Get the current best block.
	bestBlock, err := w.cl.BestBlock()
	if err != nil {
		return
	}

	// We may have the tx's block hash linked, so check the dex database first.
	blockHash, height := w.mainchainBlockForStoredTx(txHash)

	// If we know it's spent, we can short circuit a scan.
	spender, err := w.db.GetSpend(*txHash)
	if err != nil {
		w.log.Errorf("Database error: %v", err)
	}
	if !w.blockIsMainchain(spender, -1) {
		spender = nil
		// Should delete from database?
	}

	// If we have both a mainchain block and a spender, we can short-circuit the
	// spender and further search.
	if blockHash != nil && spender != nil {
		return uint32(confirms(height, bestBlock.Height)), asset.ErrSpentSwap
	}

	// Our last option is neutrino, and we have to have a pk script for that.
	if len(pkScript) == 0 {
		pkScript, err = w.db.GetPkScript(wire.NewOutPoint(txHash, vout))
		// GetPkScript returns nil, nil for not found, which is fine.
		if err != nil {
			return
		}
		// If we don't have a pubkey script, we're done here.
		if len(pkScript) == 0 {
			return 0, fmt.Errorf("cannot locate a non-wallet transaction without a pubkey a pubkey script in spv mode")
		}
	}

	utxo, err := w.neutrinoTxOut(txHash, vout, pkScript, startTime)
	if err != nil {
		return 0, err
	}

	if utxo.SpendingTx == nil && utxo.Output == nil {
		return 0, fmt.Errorf("output %s:%v not found with search parameters startTime = %s, pkScript = %x",
			txHash, vout, startTime, pkScript)
	}

	// If unspent
	if utxo.Output != nil {
		bestHeight, err := w.getBestBlockHeight()
		if err != nil {
			return 0, fmt.Errorf("getBestBlockHeight error: %v", err)
		}
		return utxo.BlockHeight - uint32(bestHeight), nil
	}

	// If spent
	// This is a sticky situation. Neutrino will only tell us about the spend
	// OR the utxo, not both. So if we get a spending transaction, it would
	// take additional work to get the confirmation on the original output.
	// But do we actually care at that point? I'm guessing that's Neutrino's
	// philosophy, and maybe it's one we should adopt.
	return 0, asset.ErrSpentSwap
}

// func walletCofirmations

func (w *spvWallet) locked() bool {
	return w.wallet.Locked()
}

func (w *spvWallet) SyncStatus(tipAtConnect int64) (bool, float32, error) {
	panic("unimplemented")
}

func (w *spvWallet) walletLock() error {
	w.wallet.Lock()
	return nil
}

func (w *spvWallet) walletUnlock(pass string) error {
	return w.Unlock(pass)
}

func (w *spvWallet) getBlockHeader(hashStr string) (*blockHeader, error) {
	blockHash, err := chainhash.NewHashFromStr(hashStr)
	if err != nil {
		return nil, err
	}
	hdr, err := w.cl.GetBlockHeader(blockHash)
	if err != nil {
		return nil, err
	}

	medianTime, err := w.calcMedianTime(blockHash)
	if err != nil {
		return nil, err
	}

	tip, err := w.cl.BestBlock()
	if err != nil {
		return nil, fmt.Errorf("BestBlock error: %v", err)
	}

	blockHeight, err := w.cl.GetBlockHeight(blockHash)
	if err != nil {
		return nil, err
	}

	confs := uint32(tip.Height - blockHeight)

	return &blockHeader{
		Hash:          hdr.BlockHash().String(),
		Confirmations: int64(confs),
		Height:        int64(blockHeight),
		Time:          hdr.Timestamp.Unix(),
		MedianTime:    medianTime.Unix(),
	}, nil
}

const medianTimeBlocks = 11

func (w *spvWallet) calcMedianTime(blockHash *chainhash.Hash) (time.Time, error) {
	timestamps := make([]int64, 0, medianTimeBlocks)

	zeroHash := chainhash.Hash{}

	h := blockHash
	for i := 0; i < medianTimeBlocks; i++ {
		hdr, err := w.cl.GetBlockHeader(h)
		if err != nil {
			return time.Time{}, fmt.Errorf("BlockHeader error for hash %q: %v", h, err)
		}
		timestamps = append(timestamps, hdr.Timestamp.Unix())

		if hdr.PrevBlock == zeroHash {
			break
		}
		h = &hdr.PrevBlock
	}

	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})

	// See notes in btcd/blockchain (*blockNode).CalcPastMedianTime() regarding
	// incorrectly calculated median time for blocks 1, 3, 5, 7, and 9.
	medianTimestamp := timestamps[len(timestamps)/2]
	return time.Unix(medianTimestamp, 0), nil
}

// func (w *spvWallet) getVerboseBlockTxs(blockID string) (*verboseBlockTxs, error) {
// 	panic("unimplemented")
// }

func (w *spvWallet) connect(ctx context.Context) error {
	w.ctx = ctx
	err := w.chainClient.Start()
	if err != nil {
		return fmt.Errorf("Couldn't start Neutrino client: %s", err)
	}

	w.wallet.SynchronizeRPC(w.chainClient)
	return nil
}

func (w *spvWallet) stop() {
	w.loader.UnloadWallet()
	w.walletDB.Close()
}

func (w *spvWallet) blockForStoredTx(txHash *chainhash.Hash) (*chainhash.Hash, int32, error) {
	// Check if we know the block hash for the tx.
	blockHash, err := w.db.GetTransactionBlock(*txHash)
	if err != nil {
		w.log.Errorf("GetTransactionBlock error: %v", err)
		return nil, 0, err
	}
	if blockHash == nil {
		return nil, 0, nil
	}
	// Check that the block is still mainchain.
	blockHeight, err := w.cl.GetBlockHeight(blockHash)
	if err != nil {
		w.log.Errorf("Error retrieving block height for hash %s", blockHash)
		return nil, 0, err
	}
	return blockHash, blockHeight, nil
}

func (w *spvWallet) blockIsMainchain(blockHash *chainhash.Hash, blockHeight int32) bool {
	if blockHeight < 0 {
		var err error
		blockHeight, err = w.cl.GetBlockHeight(blockHash)
		if err != nil {
			w.log.Errorf("Error getting block height for hash %s", blockHash)
			return false
		}
	}
	checkHash, err := w.cl.GetBlockHash(int64(blockHeight))
	if err != nil {
		w.log.Errorf("Error retriving block hash for height %d", blockHeight)
		return false
	}
	return *checkHash == *blockHash
}

func (w *spvWallet) mainchainBlockForStoredTx(txHash *chainhash.Hash) (*chainhash.Hash, int32) {
	// Check that the block is still mainchain.
	blockHash, blockHeight, err := w.blockForStoredTx(txHash)
	if err != nil {
		w.log.Errorf("Error retrieving block height for hash %s", blockHash)
		return nil, 0
	}
	if blockHash == nil {
		return nil, 0
	}
	if !w.blockIsMainchain(blockHash, blockHeight) {
		return nil, 0
	}
	return blockHash, blockHeight
}

func (w *spvWallet) findBlockForTime(t time.Time) (*chainhash.Hash, int32, error) {
	tStamp := t.Unix()
	estBlocksBack := time.Since(t) / w.net.TargetTimePerBlock
	// To mimic Bitcoin's median time rules, we'll find the first block in
	// the first rolling group of 6 in which all block times are after the time
	// specified.

	// First a binary search to find a starting point.
	bestHeight, err := w.getBestBlockHeight()
	if err != nil {
		return nil, 0, fmt.Errorf("getBestBlockHeight error: %v", err)
	}
	height := bestHeight - int32(estBlocksBack)
	if height > bestHeight {
		height = bestHeight
	}

	getBlockTimeForHeight := func(height int32) (int64, error) {
		hash, err := w.cl.GetBlockHash(int64(height))
		if err != nil {
			return 0, err
		}
		header, err := w.cl.GetBlockHeader(hash)
		if err != nil {
			return 0, err
		}
		return header.Timestamp.Unix(), nil
	}

	var count int
	blockTimeSeconds := int64(w.net.TargetTimePerBlock.Seconds())
	for {
		t, err := getBlockTimeForHeight(height)
		if err != nil {
			return nil, 0, err
		}
		if t < tStamp {
			count++
			if count == 6 {
				break
			}
			height--
			if height <= 0 {
				// genesis block.
				return w.net.GenesisHash, 0, nil
			}
			continue
		}
		count = 0
		tDiff := t - tStamp
		height -= int32(tDiff/blockTimeSeconds + 1)
	}

	// Now we have 6 sequential blocks that are below the stamp. March forward
	// until we find a block that that is after.
	height += 6

	for {
		if height >= bestHeight {
			height = bestHeight
			break
		}
		t, err := getBlockTimeForHeight(height)
		if err != nil {
			return nil, 0, err
		}
		if t > tStamp {
			break
		}
		tDiff := tStamp - t
		height++

		estBlocks := tDiff/blockTimeSeconds + 1

		// Go faster if we're way off.
		if estBlocks > 16 {
			height += 4
		}
	}

	// So the current height is the last height in a six block group with
	// no stamps after the specified stamp. We want the first block in that
	// group.
	height -= 5
	if height <= 0 {
		return w.net.GenesisHash, 0, nil
	}
	blockHash, err := w.getBlockHash(int64(height))
	if err != nil {
		return nil, 0, fmt.Errorf("Error getting genesis block")
	}
	return blockHash, height, nil
}

func (w *spvWallet) neutrinoTxOut(txHash *chainhash.Hash, vout uint32, pkScript []byte, startTime time.Time) (*neutrino.SpendReport, error) {
	// Check if we know the block hash for the tx.
	blockHash, _ := w.mainchainBlockForStoredTx(txHash)

	var options []neutrino.RescanOption
	if blockHash != nil {
		options = append(options, neutrino.StartBlock(&headerfs.BlockStamp{Hash: *blockHash}))
	} else {
		// We could provide a start time directly with the neutrino.StartTime
		// RescanOptions, but we need to provide a neutrino.StartBlock anyway,
		// or else neutrino will scan nothing and just monitor for new blocks.
		// We also have to deal with blockchain time keeping, where a miner's
		// clock can vary by roughly +/- 2 hours from from the rest of the
		// network.
		blockHash, _, err := w.findBlockForTime(startTime)
		if err != nil {
			return nil, err
		}
		options = append(options, neutrino.StartBlock(&headerfs.BlockStamp{Hash: *blockHash}))

		// We also have to provide an end block, else the rescan will run forever
		// waiting on new blocks.
		bestHeight, err := w.getBestBlockHeight()
		if err != nil {
			return nil, fmt.Errorf("error retrieving best block height: %w", err)
		}
		options = append(options, neutrino.EndBlock(&headerfs.BlockStamp{Height: bestHeight}))
	}

	options = append(options, neutrino.WatchInputs(neutrino.InputWithScript{
		PkScript: pkScript,
		OutPoint: *wire.NewOutPoint(txHash, vout),
	}))

	utxo, err := w.cl.GetUtxo(options...)
	if err != nil {
		return nil, fmt.Errorf("GetUtxo error for %s:%v: %v", txHash, vout, err)
	}

	if utxo.BlockHash != nil {
		err := w.db.SaveTransactionBlock(*txHash, *utxo.BlockHash)
		if err != nil {
			w.log.Errorf("error saving transaction block: %v", err)
		}
	}

	if utxo.SpendingTx != nil {
		spendBlockHash, err := w.cl.GetBlockHash(int64(utxo.SpendingTxHeight))
		if err != nil {
			w.log.Errorf("error getting spending tx hash")
		} else {
			spendTxHash := utxo.SpendingTx.TxHash()
			err := w.db.SaveTransactionBlock(spendTxHash, *spendBlockHash)
			if err != nil {
				w.log.Errorf("error saving transaction block: %v", err)
			}
			err = w.db.SaveSpend(*txHash, spendTxHash)
			if err != nil {
				w.log.Errorf("error saving transaction block: %v", err)
			}
		}
	}

	return utxo, nil
}

// GetTxOut
//
// NOTE: startTime must be supplied
func (w *spvWallet) getTxOut(txHash *chainhash.Hash, index uint32, pkScript []byte, startTime time.Time) (*wire.TxOut, uint32, error) {
	// Check for a wallet transaction first
	txDetails, err := w.wallet.walletTransaction(txHash)
	if err == nil {
		tip, err := w.cl.BestBlock()
		if err != nil {
			return nil, 0, fmt.Errorf("BestBlock error: %v", err)
		}

		var confs uint32
		if txDetails.Block.Height > 0 {
			confs = uint32(txDetails.Block.Height - tip.Height)
		}
		msgTx := &txDetails.MsgTx
		if len(msgTx.TxOut) < int(index+1) {
			return nil, 0, fmt.Errorf("wallet transaction %s found, but not enough outputs for index %d", txHash, index)
		}
		return msgTx.TxOut[index], confs, nil
	}

	// First, check the dex db and look for record of a spending tx. If we have
	// one, and it's still mainchain, we can error
	spendHash, err := w.db.GetSpend(*txHash)
	if err != nil {
		w.log.Errorf("error looking for spending transaction in dex database: %v", err)
	} else if spendHash != nil {
		// Check that the spending tx's block is still mainchain.
		spendBlockHash, err := w.db.GetTransactionBlock(*spendHash)
		if err != nil {
			w.log.Errorf("error finding block hashf or spending tx %s: %v", spendHash, err)
		}
		if w.blockIsMainchain(spendBlockHash, -1) {
			// return nil, 0, fmt.Errorf("tx %s is spent by tx %s in block %s", txHash, spendHash, spendBlockHash)
			// Match the behavior of rpcClient
			return nil, 0, nil
		}
	}

	// We don't really know if it's spent, so we'll need to scan.
	utxo, err := w.neutrinoTxOut(txHash, index, pkScript, startTime)
	if err != nil {
		return nil, 0, err
	}

	op := utxo.Output
	if op == nil {
		if utxo.SpendingTx != nil {
			return nil, 0, fmt.Errorf("output %s:%v is spent in transaction %s", txHash, index, utxo.SpendingTx.TxHash())
		}
		return nil, 0, fmt.Errorf("output %s:%v not found", txHash, index)
	}

	tip, err := w.cl.BestBlock()
	if err != nil {
		return nil, 0, fmt.Errorf("BestBlock error: %v", err)
	}

	confs := uint32(tip.Height) - utxo.BlockHeight + 1

	return op, confs, nil
}

func (w *spvWallet) getTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	return w.wallet.getTransaction(txHash)
}

func (w *spvWallet) searchBlockForRedemptions(reqs map[outPoint]*findRedemptionReq, blockHash chainhash.Hash) (discovered map[outPoint]*findRedemptionResult) {
	scripts := make([][]byte, 0, len(reqs))
	for _, req := range reqs {
		scripts = append(scripts, req.pkScript)
	}

	discovered = make(map[outPoint]*findRedemptionResult, len(reqs))

	filter, err := w.cl.GetCFilter(blockHash, wire.GCSFilterRegular)
	if err != nil {
		w.log.Errorf("GetCFilter error: %w", err)
		return
	}
	var filterKey [gcs.KeySize]byte
	copy(filterKey[:], blockHash[:])

	matchFound, err := filter.MatchAny(filterKey, scripts)
	if err != nil {
		w.log.Errorf("MatchAny error: %w", err)
		return
	}

	if !matchFound {
		return
	}

	// There is at least one match. Pull the block.
	block, err := w.cl.GetBlock(blockHash)
	if err != nil {
		w.log.Errorf("neutrino GetBlock error: %v", err)
		return
	}

	for _, msgTx := range block.MsgBlock().Transactions {
		newlyDiscovered := findRedemptionsInTx(true, reqs, msgTx, w.net)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
		}
	}
	return
}

func (w *spvWallet) findRedemptionsInMempool(reqs map[outPoint]*findRedemptionReq) (discovered map[outPoint]*findRedemptionResult) {
	return
}

type btcWalletUsher struct {
	*wallet.Wallet
}

func (w *btcWalletUsher) walletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error) {
	details, err := wallet.UnstableAPI(w.Wallet).TxDetails(txHash)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, fmt.Errorf("transaction not found")
	}

	return details, nil
}

// GetTransaction retrieves the specified wallet-related transaction.
func (w *btcWalletUsher) confirmations(txHash *chainhash.Hash, vout uint32) (confs uint32, spent bool, err error) {
	details, err := wallet.UnstableAPI(w.Wallet).TxDetails(txHash)
	if err != nil {
		return 0, false, err
	}
	if details == nil {
		return 0, false, WalletTransactionNotFound
	}
	for _, credit := range details.Credits {
		if credit.Index == vout {
			syncBlock := w.Manager.SyncedTo() // Better than chainClient.GetBestBlockHeight() ?
			return uint32(confirms(details.Block.Height, syncBlock.Height)), credit.Spent, nil
		}
	}
	return 0, false, fmt.Errorf("transaction %s found in the wallet, but output %d not reported", txHash, vout)
}

// GetTransaction retrieves the specified wallet-related transaction.
func (w *btcWalletUsher) getTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	// Option # 1 just copies from UnstableAPI.TxDetails. Duplicating the
	// unexported bucket key feels dirty.
	//
	// var details *wtxmgr.TxDetails
	// err := walletdb.View(w.Database(), func(dbtx walletdb.ReadTx) error {
	// 	txKey := []byte("wtxmgr")
	// 	txmgrNs := dbtx.ReadBucket(txKey)
	// 	var err error
	// 	details, err = w.TxStore.TxDetails(txmgrNs, txHash)
	// 	return err
	// })

	// Option #2
	// This is what the JSON-RPC does (and has since at least May 2018).
	details, err := wallet.UnstableAPI(w.Wallet).TxDetails(txHash)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, WalletTransactionNotFound
	}

	syncBlock := w.Manager.SyncedTo()

	// TODO: The serialized transaction is already in the DB, so
	// reserializing can be avoided here.
	var txBuf bytes.Buffer
	txBuf.Grow(details.MsgTx.SerializeSize())
	err = details.MsgTx.Serialize(&txBuf)
	if err != nil {
		return nil, err
	}

	// TODO: Add a "generated" field to this result type.  "generated":true
	// is only added if the transaction is a coinbase.
	ret := &GetTransactionResult{
		TxID:         txHash.String(),
		Hex:          txBuf.Bytes(), // 'Hex' field name is a lie, kinda
		Time:         uint64(details.Received.Unix()),
		TimeReceived: uint64(details.Received.Unix()),
	}

	if details.Block.Height != -1 {
		ret.BlockHash = details.Block.Hash.String()
		ret.BlockTime = uint64(details.Block.Time.Unix())
		ret.Confirmations = uint64(confirms(details.Block.Height, syncBlock.Height))
	}

	var (
		debitTotal  btcutil.Amount
		creditTotal btcutil.Amount // Excludes change
		fee         btcutil.Amount
		feeF64      float64
	)
	for _, deb := range details.Debits {
		debitTotal += deb.Amount
	}
	for _, cred := range details.Credits {
		if !cred.Change {
			creditTotal += cred.Amount
		}
	}
	// Fee can only be determined if every input is a debit.
	if len(details.Debits) == len(details.MsgTx.TxIn) {
		var outputTotal btcutil.Amount
		for _, output := range details.MsgTx.TxOut {
			outputTotal += btcutil.Amount(output.Value)
		}
		fee = debitTotal - outputTotal
		feeF64 = fee.ToBTC()
	}

	if len(details.Debits) == 0 {
		// Credits must be set later, but since we know the full length
		// of the details slice, allocate it with the correct cap.
		ret.Details = make([]*WalletTxDetails, 0, len(details.Credits))
	} else {
		ret.Details = make([]*WalletTxDetails, 1, len(details.Credits)+1)

		ret.Details[0] = &WalletTxDetails{
			Category: "send",
			Amount:   (-debitTotal).ToBTC(), // negative since it is a send
			Fee:      feeF64,
		}
		ret.Fee = feeF64
	}

	credCat := wallet.RecvCategory(details, syncBlock.Height, w.ChainParams()).String()
	for _, cred := range details.Credits {
		// Change is ignored.
		if cred.Change {
			continue
		}

		var address string
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(
			details.MsgTx.TxOut[cred.Index].PkScript, w.ChainParams())
		if err == nil && len(addrs) == 1 {
			addr := addrs[0]
			address = addr.EncodeAddress()
		}

		ret.Details = append(ret.Details, &WalletTxDetails{
			Address:  address,
			Category: WalletTxCategory(credCat),
			Amount:   cred.Amount.ToBTC(),
			Vout:     cred.Index,
		})
	}

	ret.Amount = creditTotal.ToBTC()
	return ret, nil
}

type walletConfig struct {
	DBDir        string
	Net          *chaincfg.Params
	ConnectPeers []string
}

func loadChainClient(cfg *walletConfig) (*wallet.Loader, *neutrino.ChainService, *chain.NeutrinoClient, walletdb.DB, error) {
	netDir := filepath.Join(cfg.DBDir, cfg.Net.Name)
	// timeout and recoverWindow arguments borrowed from btcwallet directly.
	loader := wallet.NewLoader(cfg.Net, netDir, true, 60*time.Second, 250)

	exists, err := loader.WalletExists()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error verifying wallet existence: %v", err)
	}
	if !exists {
		return nil, nil, nil, nil, fmt.Errorf("wallet not found")
	}

	nuetrinoDBPath := filepath.Join(netDir, "neutrino.db")
	spvdb, err := walletdb.Create("bdb", nuetrinoDBPath, true, 60*time.Second)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("Unable to create wallet: %s", err)
	}
	// defer spvdb.Close()
	chainService, err := neutrino.NewChainService(neutrino.Config{
		DataDir:      netDir,
		Database:     spvdb,
		ChainParams:  *cfg.Net,
		ConnectPeers: cfg.ConnectPeers,
		// AddPeers:     cfg.AddPeers,
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("Couldn't create Neutrino ChainService: %s", err)
	}

	chainClient := chain.NewNeutrinoClient(cfg.Net, chainService)
	return loader, chainService, chainClient, spvdb, nil
}

func startWallet(loader *wallet.Loader, chainClient *chain.NeutrinoClient) (*wallet.Wallet, error) {
	w, err := loader.OpenExistingWallet([]byte(wallet.InsecurePubPassphrase), false)
	if err != nil {
		return nil, err
	}

	return w, nil
}

func nativeWalletExists(dbDir string, net *chaincfg.Params) (bool, error) {
	netDir := filepath.Join(dbDir, net.Name)
	return wallet.NewLoader(net, netDir, true, 60*time.Second, 250).WalletExists()
}

func createWallet(privPass []byte, seed []byte, dbDir string, net *chaincfg.Params) error {
	netDir := filepath.Join(dbDir, net.Name)
	err := os.MkdirAll(netDir, 0777)
	if err != nil {
		return fmt.Errorf("error creating wallet directories: %v", err)
	}

	loader := wallet.NewLoader(net, netDir, true, 60*time.Second, 250)
	defer loader.UnloadWallet()

	// Ascertain the public passphrase.  This will either be a value
	// specified by the user or the default hard-coded public passphrase if
	// the user does not want the additional public data encryption.
	pubPass := []byte(wallet.InsecurePubPassphrase)

	fmt.Println("Creating the wallet...")
	_, err = loader.CreateNewWallet(pubPass, privPass, seed, time.Now())
	if err != nil {
		return err
	}

	// fmt.Println("Creating the base '" + spvAcctName + "' account")
	// err = w.Unlock(privPass, time.After(time.Minute))
	// if err != nil {
	// 	return nil, err
	// }
	// _, err = w.NextAccount(waddrmgr.KeyScopeBIP0044, spvAcctName)
	// if err != nil {
	// 	return nil, err
	// }
	// w.Lock()

	return nil
}

// func loadWallet(dbDir string, net *chaincfg.Params) (*wallet.Wallet, *neutrino.ChainService, error) {
// 	netDir := filepath.Join(dbDir, net.Name)
// 	// timeout and recoverWindow arguments borrowed from btcwallet directly.
// 	loader := wallet.NewLoader(net, netDir, true, 60*time.Second, 250)

// 	nuetrinoDBPath := filepath.Join(netDir, "neutrino.db")
// 	spvdb, err := walletdb.Create("bdb", nuetrinoDBPath, true, 60*time.Second)
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("Unable to create wallet: %s", err)
// 	}
// 	// defer spvdb.Close()
// 	chainService, err := neutrino.NewChainService(neutrino.Config{
// 		DataDir:     netDir,
// 		Database:    spvdb,
// 		ChainParams: *net,
// 		// ConnectPeers: cfg.ConnectPeers,
// 		// AddPeers:     cfg.AddPeers,
// 	})
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("Couldn't create Neutrino ChainService: %s", err)
// 	}

// 	chainClient := chain.NewNeutrinoClient(net, chainService)
// 	err = chainClient.Start()
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("Couldn't start Neutrino client: %s", err)
// 	}

// 	loader.RunAfterLoad(func(w *wallet.Wallet) {
// 		w.SynchronizeRPC(chainClient)
// 	})

// 	w, err := loader.OpenExistingWallet([]byte(wallet.InsecurePubPassphrase), false)
// 	if err != nil {
// 		return nil, nil, err
// 	}

// 	return w, chainService, nil
// }

// func walletExists(dbDir string, net *chaincfg.Params) (bool, error) {
// 	netDir := filepath.Join(dbDir, net.Name)
// 	// timeout and recoverWindow arguments borrowed from btcwallet directly.
// 	return wallet.NewLoader(net, netDir, true, 60*time.Second, 250).WalletExists()
// }

// func createWallet(privPass []byte, dbDir string, net *chaincfg.Params) error {
// 	netDir := filepath.Join(dbDir, net.Name)
// 	err := os.MkdirAll(netDir, 0777)
// 	if err != nil {
// 		return fmt.Errorf("error creating wallet directories: %v", err)
// 	}

// 	loader := wallet.NewLoader(net, netDir, true, 60*time.Second, 250)
// 	defer loader.UnloadWallet()

// 	// Ascertain the public passphrase.  This will either be a value
// 	// specified by the user or the default hard-coded public passphrase if
// 	// the user does not want the additional public data encryption.
// 	pubPass := []byte(wallet.InsecurePubPassphrase)

// 	seed := encode.RandomBytes(32)

// 	fmt.Println("Creating the wallet...")
// 	w, err := loader.CreateNewWallet(pubPass, privPass, seed, time.Now())
// 	if err != nil {
// 		return err
// 	}

// 	fmt.Println("Creating the base '" + spvAcctName + "' account")
// 	err = w.Unlock(privPass, time.After(time.Minute))
// 	if err != nil {
// 		return err
// 	}
// 	_, err = w.NextAccount(waddrmgr.KeyScopeBIP0044, spvAcctName)
// 	if err != nil {
// 		return err
// 	}
// 	w.Lock()

// 	fmt.Println("The wallet has been created successfully.")
// 	return nil
// }

func confirms(txHeight, curHeight int32) int32 {
	switch {
	case txHeight == -1, txHeight > curHeight:
		return 0
	default:
		return curHeight - txHeight + 1
	}
}

// chain.Interface
// type Interface interface {
// 	Start() error
// 	Stop()
// 	WaitForShutdown()
// 	GetBestBlock() (*chainhash.Hash, int32, error)
// 	GetBlock(*chainhash.Hash) (*wire.MsgBlock, error)
// 	GetBlockHash(int64) (*chainhash.Hash, error)
// 	GetBlockHeader(*chainhash.Hash) (*wire.BlockHeader, error)
// 	IsCurrent() bool
// 	FilterBlocks(*FilterBlocksRequest) (*FilterBlocksResponse, error)
// 	BlockStamp() (*waddrmgr.BlockStamp, error)
// 	SendRawTransaction(*wire.MsgTx, bool) (*chainhash.Hash, error)
// 	Rescan(*chainhash.Hash, []btcutil.Address, map[wire.OutPoint]btcutil.Address) error
// 	NotifyReceived([]btcutil.Address) error
// 	NotifyBlocks() error
// 	Notifications() <-chan interface{}
// 	BackEnd() string
// }

// func (w *Wallet) MakeMultiSigScript(addrs []btcutil.Address, nRequired int) ([]byte, error)
// func (w *Wallet) ImportP2SHRedeemScript(script []byte) (*btcutil.AddressScriptHash, error)
// func (w *Wallet) SubmitRescan(job *RescanJob) <-chan error
// func (w *Wallet) Rescan(addrs []btcutil.Address, unspent []wtxmgr.Credit) error
// func (w *Wallet) UnspentOutputs(policy OutputSelectionPolicy) ([]*TransactionOutput, error)
// func (w *Wallet) Start() // called by loader.OpenExistingWallet
// func (w *Wallet) SynchronizeRPC(chainClient chain.Interface)
// func (w *Wallet) ChainClient() chain.Interface
// func (w *Wallet) Stop()
// func (w *Wallet) ShuttingDown() bool
// func (w *Wallet) WaitForShutdown()
// func (w *Wallet) SynchronizingToNetwork() bool
// func (w *Wallet) ChainSynced() bool
// func (w *Wallet) SetChainSynced(synced bool)
// func (w *Wallet) CreateSimpleTx(account uint32, outputs []*wire.TxOut, minconf int32, satPerKb btcutil.Amount, dryRun bool)
// func (w *Wallet) Unlock(passphrase []byte, lock <-chan time.Time) error
// func (w *Wallet) Lock()
// func (w *Wallet) Locked() bool
// func (w *Wallet) ChangePrivatePassphrase(old, new []byte) error
// func (w *Wallet) ChangePublicPassphrase(old, new []byte) error
// func (w *Wallet) ChangePassphrases(publicOld, publicNew, privateOld, privateNew []byte) error
// func (w *Wallet) ChangePassphrases(publicOld, publicNew, privateOld, privateNew []byte) error
// func (w *Wallet) AccountAddresses(account uint32) (addrs []btcutil.Address, err error)
// func (w *Wallet) CalculateBalance(confirms int32) (btcutil.Amount, error)
// func (w *Wallet) CalculateAccountBalances(account uint32, confirms int32) (Balances, error)
// func (w *Wallet) CurrentAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
// func (w *Wallet) PubKeyForAddress(a btcutil.Address) (*btcec.PublicKey, error)
// func (w *Wallet) LabelTransaction(hash chainhash.Hash, label string, overwrite bool) error
// func (w *Wallet) PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error)
// func (w *Wallet) HaveAddress(a btcutil.Address) (bool, error)
// func (w *Wallet) AccountOfAddress(a btcutil.Address) (uint32, error)
// func (w *Wallet) AddressInfo(a btcutil.Address) (waddrmgr.ManagedAddress, error)
// func (w *Wallet) AccountNumber(scope waddrmgr.KeyScope, accountName string) (uint32, error)
// func (w *Wallet) AccountName(scope waddrmgr.KeyScope, accountNumber uint32) (string, error)
// func (w *Wallet) AccountProperties(scope waddrmgr.KeyScope, acct uint32) (*waddrmgr.AccountProperties, error)
// func (w *Wallet) RenameAccount(scope waddrmgr.KeyScope, account uint32, newName string) error
// func (w *Wallet) NextAccount(scope waddrmgr.KeyScope, name string) (uint32, error)
// func (w *Wallet) ListSinceBlock(start, end, syncHeight int32) ([]btcjson.ListTransactionsResult, error)
// func (w *Wallet) ListTransactions(from, count int) ([]btcjson.ListTransactionsResult, error)
// func (w *Wallet) ListAddressTransactions(pkHashes map[string]struct{}) ([]btcjson.ListTransactionsResult, error)
// func (w *Wallet) ListAllTransactions() ([]btcjson.ListTransactionsResult, error)
// func (w *Wallet) GetTransactions(startBlock, endBlock *BlockIdentifier, cancel <-chan struct{}) (*GetTransactionsResult, error)
// func (w *Wallet) Accounts(scope waddrmgr.KeyScope) (*AccountsResult, error)
// func (w *Wallet) AccountBalances(scope waddrmgr.KeyScope, requiredConfs int32) ([]AccountBalanceResult, error)
// func (w *Wallet) ListUnspent(minconf, maxconf int32, addresses map[string]struct{}) ([]*btcjson.ListUnspentResult, error)
// func (w *Wallet) DumpPrivKeys() ([]string, error)
// func (w *Wallet) DumpWIFPrivateKey(addr btcutil.Address) (string, error)
// func (w *Wallet) ImportPrivateKey(scope waddrmgr.KeyScope, wif *btcutil.WIF, bs *waddrmgr.BlockStamp, rescan bool) (string, error)
// func (w *Wallet) LockedOutpoint(op wire.OutPoint) bool
// func (w *Wallet) LockOutpoint(op wire.OutPoint)
// func (w *Wallet) UnlockOutpoint(op wire.OutPoint)
// func (w *Wallet) ResetLockedOutpoints()
// func (w *Wallet) LockedOutpoints() []btcjson.TransactionInput
// func (w *Wallet) LeaseOutput(id wtxmgr.LockID, op wire.OutPoint) (time.Time, error)
// func (w *Wallet) ReleaseOutput(id wtxmgr.LockID, op wire.OutPoint) error
// func (w *Wallet) SortedActivePaymentAddresses() ([]string, error)
// func (w *Wallet) NewAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
// func (w *Wallet) NewChangeAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
// func (w *Wallet) TotalReceivedForAccounts(scope waddrmgr.KeyScope, minConf int32) ([]AccountTotalReceivedResult, error)
// func (w *Wallet) TotalReceivedForAddr(addr btcutil.Address, minConf int32) (btcutil.Amount, error)
// func (w *Wallet) SendOutputs(outputs []*wire.TxOut, account uint32, minconf int32, satPerKb btcutil.Amount, label string) (*wire.MsgTx, error)
// func (w *Wallet) SignTransaction(tx *wire.MsgTx, hashType txscript.SigHashType,
// 	                            additionalPrevScriptsadditionalPrevScripts map[wire.OutPoint][]byte,
// 	                            additionalKeysByAddress map[string]*btcutil.WIF,
// 	                            p2shRedeemScriptsByAddress map[string][]byte) ([]SignatureError, error)
// func (w *Wallet) PublishTransaction(tx *wire.MsgTx, label string) error
// func (w *Wallet) ChainParams() *chaincfg.Params
// func (w *Wallet) Database() walletdb.DB
// FetchInputInfo(prevOut *wire.OutPoint) (*wire.MsgTx, *wire.TxOut, int64, error)
