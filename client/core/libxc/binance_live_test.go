//go:build live

package libxc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
)

var (
	log           = dex.StdOutLogger("T", dex.LevelTrace)
	u, _          = user.Current()
	testCredsPath = filepath.Join(u.HomeDir, ".dexc", "binance-creds.json")
	credsPathUS   = filepath.Join(u.HomeDir, ".dexc", "binance-us-creds.json")
)

func tNewBinance(t *testing.T, credsPath string) *Binance {
	b, err := os.ReadFile(credsPath)
	if err != nil {
		t.Fatalf("no credentials file found at %q", credsPath)
	}

	var creds struct {
		Key    string `json:"key"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(b, &creds); err != nil {
		t.Fatalf("error decoding credentials: %v", err)
	}

	bnc := NewBinance(creds.Key, creds.Secret, log, dex.Simnet)
	bnc.knownAssets = map[uint32]bool{
		0:   true,
		42:  true,
		2:   true,
		3:   true,
		60:  true,
		133: true,
		145: true,
	}
	return bnc
}

func TestBinanceAPI(t *testing.T) {
	bnc := tNewBinance(t, testCredsPath)

	// Go to the test server directly. Can't be used for sapi endpoints
	bnc.url = "https://testnet.binance.vision"

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	ob, err := bnc.GetOrderBook(ctx, "btc", "usd")
	if err != nil {
		t.Fatalf("getOrderBook error: %v", err)
	}

	b, _ := json.MarshalIndent(ob, "", "    ")
	fmt.Println(string(b))
}

// func TestBinanceSimnetAPI(t *testing.T) {
// 	bnc := tNewBinance(t)

// }

// func TestBinanceUserStream(t *testing.T) {
// 	bnc := tNewBinance(t, testCredsPath)
// 	bnc.url = "https://testnet.binance.vision"
// 	bnc.wsURL = "wss://testnet.binance.vision"

// 	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*10)
// 	defer cancel()

// 	wg, err := bnc.Connect(ctx)
// 	if err != nil {
// 		t.Fatalf("Connect error: %v", err)
// 	}

// 	wg.Wait()
// }

type spoofDriver struct {
	cFactor uint64
}

func (drv *spoofDriver) Open(*asset.WalletConfig, dex.Logger, dex.Network) (asset.Wallet, error) {
	return nil, nil
}

func (drv *spoofDriver) DecodeCoinID(coinID []byte) (string, error) {
	return "", nil
}

func (drv *spoofDriver) Info() *asset.WalletInfo {
	return &asset.WalletInfo{
		UnitInfo: dex.UnitInfo{
			Conventional: dex.Denomination{
				ConversionFactor: drv.cFactor,
			},
		},
	}
}

func TestBinanceUSConnect(t *testing.T) {
	bnc := tNewBinance(t, credsPathUS)
	bnc.url = "https://api.binance.us"
	bnc.wsURL = "wss://stream.binance.us:9443"

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	wg, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	asset.Register(93637357, &spoofDriver{cFactor: 1e2})
	asset.Register(60, &spoofDriver{cFactor: 1e9})  // eth
	asset.Register(966, &spoofDriver{cFactor: 1e9}) // matic
	asset.Register(0, &spoofDriver{cFactor: 1e8})   // btc

	if err := bnc.startMarketDataStream(ctx, "eth", "usd", 1e8); err != nil {
		t.Fatalf("Error starting market ETHUSD stream: %v", err)
	}

	if err := bnc.startMarketDataStream(ctx, "matic", "btc", 1e2); err != nil {
		t.Fatalf("Error starting market MATICBTC stream: %v", err)
	}

	wg.Wait()
}
