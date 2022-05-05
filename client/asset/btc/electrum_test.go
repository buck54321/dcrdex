// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build electrumlive

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

const walletPass = "walletpass"

func TestPSBT(t *testing.T) {
	// an unsigned swap txn funded by a single p2wpkh
	txRaw, _ := hex.DecodeString("010000000118f36e05994f8e69dace16f4a3d4c9ff8db6f58bf48d498b1c0d6daf63c6f8690100000000ffffffff02408e2c0000000000220020e0133024bb27f510f6959467df91ed5daa791f598ba1c09ef1f1ac4540bfdda16e7710000000000016001460a8dbedd538501e2ad9e9f5a4d8a11e35a6c4e700000000")
	msgTx := wire.NewMsgTx(wire.TxVersion)
	err := msgTx.Deserialize(bytes.NewReader(txRaw))
	if err != nil {
		t.Fatal(err)
	}

	packet, err := psbt.NewFromUnsignedTx(msgTx)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := packet.B64Encode()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(enc)
}

func TestElectrumExchangeWallet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tipChanged := make(chan struct{}, 1)
	walletCfg := &asset.WalletConfig{
		Type: walletTypeElectrum,
		Settings: map[string]string{
			"rpcuser":     "user",
			"rpcpassword": "pass",
			"rpcbind":     "127.0.0.1:5678",
		},
		TipChange: func(error) {
			select {
			case tipChanged <- struct{}{}:
				t.Log("tip change, that's enough testing")
				time.Sleep(300 * time.Millisecond)
				cancel()
			default:
			}
		},
		PeersChange: func(num uint32) {
			t.Log("peer count: ", num)
		},
	}
	cfg := &BTCCloneCFG{
		WalletCFG:           walletCfg,
		Symbol:              "ltc",
		Logger:              tLogger,
		ChainParams:         dexltc.TestNet4Params,
		WalletInfo:          WalletInfo,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		Segwit:              true,
	}
	eew, err := ElectrumWallet(cfg)
	if err != nil {
		t.Fatal(err)
	}

	wg, err := eew.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	addr, err := eew.DepositAddress()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(addr)

	feeRate := eew.FeeRate()
	if feeRate == 0 {
		t.Fatal("zero fee rate")
	}
	t.Log("feeRate:", feeRate)

	// findRedemption
	swapTxHash, _ := chainhash.NewHashFromStr("328f94b433694e74852c3379d572a306df0c15b2bee4debc4f34bd5f5aebdebe")
	swapVout := uint32(0)
	redeemTxHash, _ := chainhash.NewHashFromStr("9ab09caaa6a9c0c70d9f827b97cdef708040d4ebcf5812c1a16949baf0f1f922")
	redeemVin := uint32(0)
	// P2WSH: tltc1qtxpdv806hs0rxw8ec33ntvz38vad66c9l0dv3g7rxqkrs0t92dmsmu7r6t / 00205982d61dfabc1e3338f9c46335b0513b3add6b05fbdac8a3c3302c383d655377
	// contract: 6382012088a820e05306e8c1540a38600aabfd05a4fdfbbfd60895a4f39df9e244e64cce40b2b98876a91409dea791bd76ae8fcfeaf9f39a5ac4663d92a0df67045cc15762b17576a91411228f788080cf96643c6c1cad9840685e51bac26888ac
	contract, _ := hex.DecodeString("6382012088a820e05306e8c1540a38600aabfd05a4fdfbbfd60895a4f39df9e244e64cce40b2b98876a91409dea791bd76ae8fcfeaf9f39a5ac4663d92a0df67045cc15762b17576a91411228f788080cf96643c6c1cad9840685e51bac26888ac")
	// contractHash, _ := hex.DecodeString("5982d61dfabc1e3338f9c46335b0513b3add6b05fbdac8a3c3302c383d655377")
	contractHash := sha256.Sum256(contract)
	wantSecret, _ := hex.DecodeString("0bc9ed634d04c1d4b3cb1c042937132748e4ecc57bdb617acfe6ca74eccb1b02")
	foundTxHash, foundVin, secret, err := eew.findRedemption(newOutPoint(swapTxHash, swapVout), contractHash[:])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secret, wantSecret) {
		t.Errorf("incorrect secret %x, wanted %x", secret, wantSecret)
	}
	if !foundTxHash.IsEqual(redeemTxHash) {
		t.Errorf("incorrect redeem tx hash %v, wanted %v", foundTxHash, redeemTxHash)
	}
	if redeemVin != foundVin {
		t.Errorf("incorrect redeem tx input %d, wanted %d", foundVin, redeemVin)
	}

	// FindRedemption
	redeemCoin, secretBytes, err := eew.FindRedemption(ctx, toCoinID(swapTxHash, swapVout), contract)
	if err != nil {
		t.Fatal(err)
	}
	foundTxHash, foundVin, err = decodeCoinID(redeemCoin)
	if err != nil {
		t.Fatal(err)
	}
	if !foundTxHash.IsEqual(redeemTxHash) {
		t.Errorf("incorrect redeem tx hash %v, wanted %v", foundTxHash, redeemTxHash)
	}
	if redeemVin != foundVin {
		t.Errorf("incorrect redeem tx input %d, wanted %d", foundVin, redeemVin)
	}
	if !secretBytes.Equal(wantSecret) {
		t.Errorf("incorrect secret %v, wanted %x", secretBytes, wantSecret)
	}

	// err = eew.Unlock([]byte(walletPass))
	// if err != nil {
	// 	t.Fatal(err)
	// }

	// wdCoin, err := eew.Withdraw(addr, toSatoshi(1.2435), feeRate)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(wdCoin.String())

	select {
	case <-time.After(10 * time.Second): // a bit of best block polling
		cancel()
	case <-ctx.Done(): // or until TipChange cancels the context
	}

	wg.Wait()
}
