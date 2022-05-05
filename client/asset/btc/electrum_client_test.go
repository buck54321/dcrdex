// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build electrumlive

package btc

import (
	"bytes"
	"context"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc/electrum"
	"decred.org/dcrdex/dex"
	dexltc "decred.org/dcrdex/dex/networks/ltc"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
)

func Test_electrumWallet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	const walletPass = "walletpass" // set me
	ewc := electrum.NewWalletClient("user", "pass", "http://127.0.0.1:5678")
	ew := newElectrumWallet(ewc, &electrumWalletConfig{
		params: dexltc.TestNet4Params,
		log:    dex.StdOutLogger("ELECTRUM-TEST", dex.LevelTrace),
		segwit: true,
	})
	var wg sync.WaitGroup
	err := ew.connect(ctx, &wg)
	if err != nil {
		t.Fatal(err)
	}

	medianTime, err := ew.calcMedianTime(2319765) // ltc testnet
	if err != nil {
		t.Fatal(err)
	}
	t.Log(medianTime) // 1651543832 / 2022-05-02 21:10:32 -0500 CDT

	// Output:
	// https://tltc.bitaps.com/8994e6c1c0762e0c5f17242f49bd418b2bb9ee457df142cef5d0e21fa4160505/output/0
	// Spending input:
	// https://tltc.bitaps.com/4cc3aaf37906fb693a613e0e50c02e5244ea4a7abc601eefb3d9e4958bba57b1/input/0

	// findOutputSpender
	fundHash, _ := chainhash.NewHashFromStr("8994e6c1c0762e0c5f17242f49bd418b2bb9ee457df142cef5d0e21fa4160505")
	fundVout := uint32(0)
	fundVal := int64(1609000)
	fundPkScript, _ := hex.DecodeString("76a914cdf2674eb7b3ba13df58b8f50035df4a49abafc288ac")
	fundAddr := "mzHuCsaSBFdCPDenNomXbhpD2vkk7ABCo8"
	spendMsgTx, spendVin, err := ew.findOutputSpender(fundHash, fundVout)
	if err != nil {
		t.Fatal(err)
	}
	spendHash := spendMsgTx.TxHash()
	wantSpendTxID := "4cc3aaf37906fb693a613e0e50c02e5244ea4a7abc601eefb3d9e4958bba57b1"
	if spendHash.String() != wantSpendTxID {
		t.Errorf("Incorrect spending tx hash %v, want %v", spendHash, wantSpendTxID)
	}
	wantSpendVin := uint32(0)
	if spendVin != wantSpendVin {
		t.Errorf("Incorrect spending tx input index %d, want %d", spendVin, wantSpendVin)
	}

	// gettxout - first the spent output
	fundTxOut, fundConfs, err := ew.getTxOut(fundHash, fundVout, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if fundTxOut != nil {
		t.Errorf("expected a nil TxOut for a spent output, but got one")
	}

	// gettxout - next an old unspent output that's likely to stay unspent
	fundHash, _ = chainhash.NewHashFromStr("0f3bfa3509f14bc95a8d6307a35da49b2e63cabb87b8e08baa52c68b9b58dd1d")
	fundVout = 0
	fundVal = 1784274
	fundPkScript, _ = hex.DecodeString("a9143bd6bf891433cc8faaead5833e85eb58d3d8749c87")
	fundAddr = "QS4PEajQEzuyLH1dSp1KFop5gVPU2x9aWg"
	fundTxOut, fundConfs, err = ew.getTxOut(fundHash, fundVout, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v confs = %d", fundHash, fundConfs)
	if fundTxOut.Value != fundVal {
		t.Errorf("wrong output value %d, wanted %d", fundTxOut.Value, fundVal)
	}
	if !bytes.Equal(fundTxOut.PkScript, fundPkScript) {
		t.Errorf("wanted pkScript %x, got %x", fundPkScript, fundTxOut.PkScript)
	}
	scriptClass, addrs, reqSigs, err := txscript.ExtractPkScriptAddrs(fundTxOut.PkScript, ew.chainParams)
	if err != nil {
		t.Fatal(err)
	}
	if scriptClass != txscript.ScriptHashTy {
		t.Errorf("got script class %v, wanted %v", scriptClass, txscript.ScriptHashTy)
	}
	if len(addrs) != 1 {
		t.Fatalf("got %d addresses, wanted 1", len(addrs))
	}
	if addrs[0].String() != fundAddr {
		t.Errorf("got address %v, wanted %v", addrs[0], fundAddr)
	}
	if reqSigs != 1 {
		t.Fatalf("requires %d sigs, expected 1", reqSigs)
	}

	addr, err := ew.wallet.GetUnusedAddress() // or ew.externalAddress(), but let's not blow up the gap limit testing
	if err != nil {
		t.Fatal(err)
	}
	t.Log(addr)

	err = ew.walletUnlock([]byte(walletPass))
	if err != nil {
		t.Fatal(err)
	}

	_, err = ew.privKeyForAddress(addr)
	if err != nil {
		t.Fatal(err)
	}

	// Try the following with your own addresses!
	// sent1, err := ew.sendToAddress("tltc1qn7dsn7glncdgf078pauerlqum6ma8peyvfa7gz", toSatoshi(0.2345), 4, false)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(sent1)
	// sent2, err := ew.sendToAddress("tltc1qpth226vjw2vp3mk8fvjfnrc4ygy8vtnx8p2q78", toSatoshi(0.2345), 5, true)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(sent2)

	// addr, err := ew.externalAddress()
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// sweepTx, err := ew.sweep(addr.EncodeAddress(), 6)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(sweepTx)

	// Hang out until the context times out. Maybe see a new block. Think about
	// other tests...
	wg.Wait()
}
