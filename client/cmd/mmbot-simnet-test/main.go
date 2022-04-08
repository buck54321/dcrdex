package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/core/simharness"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"

	_ "decred.org/dcrdex/client/asset/btc"
	_ "decred.org/dcrdex/client/asset/dcr"
)

const (
	dcrID = 42
	btcID = 0
)

var (
	pgmID   uint64
	c       *core.Core
	appPass = simharness.Pass
	log     = dex.StdOutLogger("TBOT", dex.LevelDebug)
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		for range killChan {
			if c != nil && pgmID > 0 {
				u := c.User()
			out:
				for _, r := range u.Bots {
					for _, ord := range r.Orders {
						if ord.Status <= order.OrderStatusBooked {
							if err := c.RetireBot(pgmID); err != nil {
								fmt.Fprintf(os.Stderr, "error retiring bot: %v \n", err)
							}
							epochLen := time.Millisecond * time.Duration(u.Exchanges[simharness.Host].Markets["dcr_btc"].EpochLen)
							log.Infof("Cancelling orders. Waiting one epoch.")
							time.Sleep(epochLen)
							break out
						}
					}
				}

			}
			log.Infof("Shutdown signal received")
			cancel()
		}
	}()

	if err := mainErr(ctx); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	dir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	// Unlock wallets, since they may have been locked on a previous shutdown.
	if err = simharness.UnlockWallet(ctx, "dcr"); err != nil {
		return err
	}
	if err = simharness.UnlockWallet(ctx, "btc"); err != nil {
		return err
	}

	dcrName, _, err := simharness.CreateWallet(ctx, "dcr", "alpha")
	if err != nil {
		return fmt.Errorf("decred create account error: %v", err)
	}

	btcName, _, err := simharness.CreateWallet(ctx, "btc", "alpha")
	if err != nil {
		return fmt.Errorf("bitcoin create wallet error: %v", err)
	}
	<-time.After(time.Second)

	c, err = core.New(&core.Config{
		DBPath: filepath.Join(dir, "dex.db"),
		Net:    dex.Simnet,
		Logger: dex.StdOutLogger("CORE", dex.LevelTrace),
	})
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.Run(ctx)
	}()

	<-c.Ready()

	if err = c.InitializeClient(appPass, nil); err != nil {
		return err
	}

	// Create wallets
	alphaDcrCfg := simharness.LoadNodeConfig("dcr", "alpha")
	form := simharness.RPCWalletForm("dcr", "alpha", dcrName, alphaDcrCfg["rpclisten"])
	if err = c.CreateWallet(appPass, []byte("abc"), form); err != nil {
		return err
	}

	alphaBtcCfg := simharness.LoadNodeConfig("btc", "alpha")
	form = simharness.RPCWalletForm("btc", "alpha", btcName, alphaBtcCfg["rpcport"])
	if err = c.CreateWallet(appPass, nil, form); err != nil {
		return err
	}

	// Fund wallets
	dcrAddr, err := c.NewDepositAddress(dcrID)
	if err != nil {
		return err
	}

	log.Infof(":::: Decred address: %s", dcrAddr)

	if err = simharness.Send(ctx, "dcr", "alpha", dcrAddr, 1000e8); err != nil {
		return err
	}

	if err = (<-simharness.Mine(ctx, "dcr", "alpha")).Err; err != nil {
		return err
	}

	btcAddr, err := c.NewDepositAddress(btcID)
	if err != nil {
		return err
	}

	log.Infof(":::: Bitcoin address: %s", btcAddr)

	if err = simharness.Send(ctx, "btc", "alpha", btcAddr, 10e8); err != nil {
		return err
	}

	if err = (<-simharness.Mine(ctx, "btc", "alpha")).Err; err != nil {
		return err
	}

	// Register DEX
	if err = simharness.RegisterCore(c, "dcr"); err != nil {
		return err
	}

	<-simharness.Mine(ctx, "dcr", "alpha")

	var oracleWeighting float64 = 0.5

	if pgmID, err = c.CreateBot(appPass, core.MakerBotV0, &core.MakerProgram{
		Host:             simharness.Host,
		BaseID:           dcrID,
		QuoteID:          btcID,
		Lots:             5,
		OracleWeighting:  &oracleWeighting,
		DriftTolerance:   0.001,
		SpreadMultiplier: 5,
	}); err != nil {
		return err
	}

	wg.Wait()

	return nil
}
