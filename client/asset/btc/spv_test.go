// +build !spv

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/walletdb"
)

type tDcrWallet struct {
	*tRPCClient
}

func (w *tDcrWallet) PublishTransaction(tx *wire.MsgTx, label string) error {
	return nil
}

func (w *tDcrWallet) CalculateAccountBalances(account uint32, confirms int32) (wallet.Balances, error) {
	return wallet.Balances{}, nil
}

func (w *tDcrWallet) ListUnspent(minconf, maxconf int32, addresses map[string]struct{}) ([]*btcjson.ListUnspentResult, error) {
	return nil, nil
}

func (w *tDcrWallet) FetchInputInfo(prevOut *wire.OutPoint) (*wire.MsgTx, *wire.TxOut, int64, error) {
	return nil, nil, 0, nil
}

func (w *tDcrWallet) ResetLockedOutpoints() {}

func (w *tDcrWallet) LockOutpoint(op wire.OutPoint) {}

func (w *tDcrWallet) UnlockOutpoint(op wire.OutPoint) {}

func (w *tDcrWallet) LockedOutpoints() []btcjson.TransactionInput {
	return nil
}

func (w *tDcrWallet) NewChangeAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error) {
	return nil, nil
}

func (w *tDcrWallet) NewAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error) {
	return nil, nil
}

func (w *tDcrWallet) SignTransaction(tx *wire.MsgTx, hashType txscript.SigHashType, additionalPrevScriptsadditionalPrevScripts map[wire.OutPoint][]byte,
	additionalKeysByAddress map[string]*btcutil.WIF, p2shRedeemScriptsByAddress map[string][]byte) ([]wallet.SignatureError, error) {

	return nil, nil
}

func (w *tDcrWallet) PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error) {
	return nil, nil
}

func (w *tDcrWallet) Database() walletdb.DB {
	return nil
}

func (w *tDcrWallet) Unlock(passphrase []byte, lock <-chan time.Time) error {
	return nil
}

func (w *tDcrWallet) Lock() {}

func (w *tDcrWallet) SendOutputs(outputs []*wire.TxOut, account uint32, minconf int32, satPerKb btcutil.Amount, label string) (*wire.MsgTx, error) {
	return nil, nil
}

func (w *tDcrWallet) HaveAddress(a btcutil.Address) (bool, error) {
	return false, nil
}
