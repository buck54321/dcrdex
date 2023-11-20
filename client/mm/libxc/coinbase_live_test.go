// //go:build cblive

package libxc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"

	_ "decred.org/dcrdex/client/asset/bch"     // register bch asset
	_ "decred.org/dcrdex/client/asset/btc"     // register btc asset
	_ "decred.org/dcrdex/client/asset/dash"    // register dash asset
	_ "decred.org/dcrdex/client/asset/dcr"     // register dcr asset
	_ "decred.org/dcrdex/client/asset/dgb"     // register dgb asset
	_ "decred.org/dcrdex/client/asset/doge"    // register doge asset
	_ "decred.org/dcrdex/client/asset/eth"     // register eth asset
	_ "decred.org/dcrdex/client/asset/firo"    // register firo asset
	_ "decred.org/dcrdex/client/asset/ltc"     // register ltc asset
	_ "decred.org/dcrdex/client/asset/polygon" // register polygon asset
	_ "decred.org/dcrdex/client/asset/zcl"     // register zcl asset
	_ "decred.org/dcrdex/client/asset/zec"     // register zec asset
)

var tCtx context.Context

func TestMain(m *testing.M) {
	asset.SetNetwork(dex.Mainnet)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func tNewCoinbase(t *testing.T) (*coinbase, func()) {
	credsPath := os.Getenv("CBCREDS")
	if credsPath == "" {
		t.Fatalf("No CBCREDS environmental variable found")
	}
	b, err := os.ReadFile(dex.CleanAndExpandPath(credsPath))
	if err != nil {
		t.Fatalf("error reading crede")
	}

	var creds struct {
		APIKey string `json:"apiKey"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(b, &creds); err != nil {
		t.Fatalf("error unmarshaling credentials: %v", err)
	}

	if creds.APIKey == "" || creds.Secret == "" {
		t.Fatalf("Incomplete credentials")
	}

	c, err := newCoinbase(creds.APIKey, creds.Secret, dex.StdOutLogger("T", dex.LevelInfo), dex.Mainnet)
	if err != nil {
		t.Fatalf("Error constructing coinbase: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	return c, cancel
}

func TestAssets(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	err := c.updateAssets()
	if err != nil {
		t.Fatalf("updateAssets error: %v", err)
	}
	for _, a := range c.assets {
		fmt.Printf("%s Balance = %f \n", a.AvailableBalance.Currency, a.AvailableBalance.Value)
	}
	if _, err = c.getAsset("BTC"); err != nil {
		t.Fatalf("error fetching bitcoin account: %v", err)
	}
}

func TestMarkets(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	err := c.updateMarkets()
	if err != nil {
		t.Fatalf("Error fetching products: %v", err)
	}

	for _, m := range c.markets {
		fmt.Printf("Market %s price %f has moved %s%% in the last 24 hours\n", m.ProductID, m.Price, m.DayPriceChangePctStr)
	}

	p, err := c.getMarket("BTC-USD")
	if err != nil {
		t.Fatalf("Error fetching product BTC-USD: %v", err)
	}
	if p.ProductID != "BTC-USD" {
		t.Fatalf("Wrong product ID %q", p.ProductID)
	}

	mkts, err := c.Markets()
	if err != nil {
		t.Fatalf("Markets error: %v", err)
	}

	for _, mkt := range mkts {
		fmt.Println("Market:", mkt.BaseID, mkt.QuoteID)
	}
}

func TestOrderbookSubscription(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	if err := c.subscribeOrderbook(0 /* btc */, 60001 /* usdc.eth */); err != nil {
		t.Fatalf("Error subscribing to BTC-USDC order book: %v", err)
	}

	select {
	case <-time.After(time.Second * 20):
	case <-c.ctx.Done():
	}

	c.refreshLevel2()
	time.Sleep(time.Second * 10)
}

func TestBalances(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	if err := c.updateAssets(); err != nil {
		t.Fatalf("updateAssets error: %v", err)
	}

	balances, err := c.Balances()
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	for assetID, b := range balances {
		ui, _ := asset.UnitInfo(assetID)
		cFactor := float64(ui.Conventional.ConversionFactor)
		ticker := ui.Conventional.Unit
		fmt.Printf("%s balance: %.8f available, %.8f locked \n", ticker, float64(b.Available)/cFactor, float64(b.Locked)/cFactor)
	}
}
