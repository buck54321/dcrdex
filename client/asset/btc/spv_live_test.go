// +build spv

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/chain"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/lightninglabs/neutrino"
)

// var tLogger btclog.Logger

type logWriter struct{}

func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	return len(p), nil
}

func initializeLogging(lvl btclog.Level) {
	backendLog := btclog.NewBackend(logWriter{})
	logger := func(name string) btclog.Logger {
		lggr := backendLog.Logger(name)
		lggr.SetLevel(lvl)
		return lggr
	}
	// tLogger = logger("TESTSPV")

	wallet.UseLogger(logger("WLLT"))
	chain.UseLogger(logger("CHAIN"))
	wtxmgr.UseLogger(logger("TXMGR"))
	neutrino.UseLogger(logger("NTRNO"))
}

func TestSync(t *testing.T) {
	initializeLogging(btclog.LevelWarn)

	dbDir, _ := ioutil.TempDir("", "")
	defer os.RemoveAll(dbDir)

	pw := []byte("pass")
	chainParams := &chaincfg.MainNetParams
	seed, _ := hex.DecodeString("aa3474b9403d6a872d51ba6c7e3c6964df016330412c101a948f74c523925c35ab9eac627cdd322061b011e63c5eb3adb0b939b7ec8e8c81a1ab801d928e0f71")

	err := createWallet(pw, seed, dbDir, chainParams)
	if err != nil {
		t.Fatalf("error creating wallet: %v", err)
	}

	w, err := newSPVWallet(dbDir, dex.StdOutLogger("SPVTEST", dex.LevelDebug), []string{"localhost:8333"}, chainParams)
	if err != nil {
		t.Fatalf("error loading wallet: %v", err)
	}
	defer func() {
		w.stop()
		w.wallet.WaitForShutdown()
	}()

	tStart := time.Now()
	for time.Since(tStart) < time.Minute*5 {
		syncStatus, err := w.syncStatus()
		if err != nil {
			t.Fatalf("syncStatus error: %v", err)
		}
		fmt.Printf("syncStatus: %+v \n", syncStatus)

		// syncing := w.wallet.ChainSynced()
		// bestLocalHeight, err := w.GetBestBlockHeight()
		// if err != nil {
		// 	t.Fatalf("error getting best block height: %v", err)
		// }
		// syncHeight := w.SyncHeight()
		// sync := 0.0
		// if syncHeight > 0 {
		// 	sync = float64(bestLocalHeight) / float64(syncHeight)
		// }
		// fmt.Printf("syncing: %t, sync status: %.1f%% \n", syncing, sync*100)
		time.Sleep(5 * time.Second)
	}
}
