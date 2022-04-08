// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

/*
The LoadBot is a load tester for dcrdex. LoadBot works by running one or more
Trader routines, each with their own *core.Core (actually, a *Mantle) and
wallets.

Build with server locktimes in mind.
i.e. -ldflags "-X 'decred.org/dcrdex/dex.testLockTimeTaker=30s' -X 'decred.org/dcrdex/dex.testLockTimeMaker=1m'"

Supported assets are bch, btc, dcr, doge, eth, ltc, and zec.
*/

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/bch"
	_ "decred.org/dcrdex/client/asset/btc"
	_ "decred.org/dcrdex/client/asset/dcr"
	_ "decred.org/dcrdex/client/asset/doge"
	_ "decred.org/dcrdex/client/asset/eth"
	_ "decred.org/dcrdex/client/asset/ltc"
	_ "decred.org/dcrdex/client/asset/zec"
	"decred.org/dcrdex/client/core/simharness"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexsrv "decred.org/dcrdex/server/dex"
	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
)

const (
	rateEncFactor    = calc.RateEncodingFactor
	defaultBtcPerDcr = 0.000878
	alpha            = "alpha"
	beta             = "beta"
	btc              = "btc"
	dcr              = "dcr"
	eth              = "eth"
	ltc              = "ltc"
	doge             = "doge"
	bch              = "bch"
	zec              = "zec"
	maxOrderLots     = 10
	ethFeeRate       = 200 // gwei
)

var (
	dcrID, _    = dex.BipSymbolID(dcr)
	btcID, _    = dex.BipSymbolID(btc)
	ethID, _    = dex.BipSymbolID(eth)
	ltcID, _    = dex.BipSymbolID(ltc)
	dogeID, _   = dex.BipSymbolID(doge)
	bchID, _    = dex.BipSymbolID(bch)
	zecID, _    = dex.BipSymbolID(zec)
	loggerMaker *dex.LoggerMaker
	pass        = []byte("abc")
	log         dex.Logger
	unbip       = dex.BipIDSymbol
	hostAddr    = simharness.Host

	usr, _ = user.Current()
	botDir = filepath.Join(simharness.Dir, "loadbot")

	ctx, quit = context.WithCancel(context.Background())

	alphaAddrBase, betaAddrBase, alphaAddrQuote, betaAddrQuote, market, baseSymbol, quoteSymbol string
	alphaCfgBase, betaCfgBase,
	alphaCfgQuote, betaCfgQuote map[string]string

	baseAssetCfg, quoteAssetCfg                           *dexsrv.AssetConf
	orderCounter, matchCounter, baseID, quoteID, regAsset uint32
	epochDuration                                         uint64
	lotSize                                               uint64
	rateStep                                              uint64
	conversionFactors                                     = make(map[string]uint64)

	ethInitFee                     = (dexeth.InitGas(1, 0) + dexeth.RefundGas(0)) * ethFeeRate
	ethRedeemFee                   = dexeth.RedeemGas(1, 0) * ethFeeRate
	defaultMidGap, marketBuyBuffer float64
	keepMidGap                     bool

	// zecSendMtx prevents sending funds too soon after mining a block and
	// the harness choosing spent outputs for zcash.
	zecSendMtx sync.Mutex
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// rpcAddr is the RPC address needed for creation of a wallet connected to the
// specified asset node. For Decred, this is the dcrwallet 'rpclisten'
// configuration parameter. For Bitcoin, its the 'rpcport' parameter. WARNING:
// with the "singular wallet" assets like zec and doge, this port may not be for
// fresh nodes created with the start-wallet script, just the alpha and beta
// nodes' wallets.
func rpcAddr(symbol, node string) string {
	var key string

	switch symbol {
	case dcr:
		key = "rpclisten"
	case btc, ltc, bch, zec, doge:
		key = "rpcport"
	case eth:
		key = "ListenAddr"
	}

	if symbol == baseSymbol {
		if node == alpha {
			return alphaCfgBase[key]
		}
		return betaCfgBase[key]
	}
	if node == alpha {
		return alphaCfgQuote[key]
	}
	return betaCfgQuote[key]
}

// returnAddress is an address for the specified node's wallet. returnAddress
// is used when a wallet accumulates more than the max allowed for some asset.
func returnAddress(symbol, node string) string {
	if symbol == baseSymbol {
		if node == alpha {
			return alphaAddrBase
		}
		return betaAddrBase
	}
	if node == alpha {
		return alphaAddrQuote
	}
	return betaAddrQuote
}

// nextAddr returns a new address, as well as the selected port, both as
// strings.
func nextAddr() (addrPort, port string, err error) {
	addrs, err := simharness.FindOpenAddrs(1)
	if err != nil {
		return "", "", err
	}
	addrPort = addrs[0].String()
	_, port, err = net.SplitHostPort(addrPort)
	if err != nil {
		return "", "", err
	}
	return addrPort, port, nil
}

func main() {
	// Catch ctrl+c for a clean shutdown.
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		quit()
	}()
	if err := run(); err != nil {
		println(err.Error())
		quit()
		os.Exit(1)
	}
	quit()
	os.Exit(0)
}

func run() error {
	defer simharness.Shutdown()
	var programName string
	var debug, trace, latency500, shaky500, slow100, spotty20, registerWithQuote bool
	var m, n int
	flag.StringVar(&programName, "p", "", "the bot program to run")
	flag.StringVar(&market, "mkt", "dcr_btc", "the market to run on")
	flag.BoolVar(&registerWithQuote, "rwq", false, "pay the register fee with the quote asset (default is base)")
	flag.BoolVar(&keepMidGap, "kmg", false, "disallow mid gap drift")
	flag.BoolVar(&latency500, "latency500", false, "add 500 ms of latency to downstream comms for wallets and server")
	flag.BoolVar(&shaky500, "shaky500", false, "add 500 ms +/- 500ms of latency to downstream comms for wallets and server")
	flag.BoolVar(&slow100, "slow100", false, "limit bandwidth on all server and wallet connections to 100 kB/s")
	flag.BoolVar(&spotty20, "spotty20", false, "drop all connections every 20 seconds")
	flag.BoolVar(&debug, "debug", false, "use debug logging")
	flag.BoolVar(&trace, "trace", false, "use trace logging")
	flag.IntVar(&m, "m", 0, "for compound and sidestacker, m is the number of makers to stack before placing takers")
	flag.IntVar(&n, "n", 0, "for compound and sidestacker, n is the number of orders to place per epoch (default 3)")
	flag.Parse()

	if programName == "" {
		return errors.New("no program set. use `-p program-name` to set the bot's program")
	}

	// Parse the default log level and create initialize logging.
	logLevel := "info"
	if debug {
		logLevel = "debug"
	}
	if trace {
		logLevel = "trace"
	}

	symbols := strings.Split(market, "_")
	if len(symbols) != 2 {
		return fmt.Errorf("invalid market %q", market)
	}
	baseSymbol = strings.ToLower(symbols[0])
	quoteSymbol = strings.ToLower(symbols[1])

	var found bool
	baseID, found = dex.BipSymbolID(baseSymbol)
	if !found {
		return fmt.Errorf("base asset %q not found", baseSymbol)
	}
	quoteID, found = dex.BipSymbolID(quoteSymbol)
	if !found {
		return fmt.Errorf("quote asset %q not found", quoteSymbol)
	}
	regAsset = baseID
	if registerWithQuote {
		regAsset = quoteID
	}
	ui, err := asset.UnitInfo(baseID)
	if err != nil {
		return fmt.Errorf("cannot get base %q unit info: %v", baseSymbol, err)
	}
	conversionFactors[baseSymbol] = ui.Conventional.ConversionFactor
	ui, err = asset.UnitInfo(quoteID)
	if err != nil {
		return fmt.Errorf("cannot get quote %q unit info: %v", quoteSymbol, err)
	}
	conversionFactors[quoteSymbol] = ui.Conventional.ConversionFactor

	mktsCfg, err := simharness.ServerMarketsConfig()
	if err != nil {
		return err
	}

	markets := make([]string, len(mktsCfg.Markets))
	for i, mkt := range mktsCfg.Markets {
		getSymbol := func(name string) (string, error) {
			asset, ok := mktsCfg.Assets[name]
			if !ok {
				return "", fmt.Errorf("config does not have an asset that matches %s", name)
			}
			return strings.ToLower(asset.Symbol), nil
		}
		bs, err := getSymbol(mkt.Base)
		if err != nil {
			return err
		}
		qs, err := getSymbol(mkt.Quote)
		if err != nil {
			return err
		}
		markets[i] = fmt.Sprintf("%s_%s", bs, qs)
		if qs != quoteSymbol || bs != baseSymbol {
			continue
		}
		lotSize = mkt.LotSize
		rateStep = mkt.RateStep
		epochDuration = mkt.Duration
		marketBuyBuffer = mkt.MBBuffer
		break
	}
	// Adjust to be comparable to the dcr_btc market.
	defaultMidGap = defaultBtcPerDcr * float64(rateStep) / 100

	if epochDuration == 0 {
		return fmt.Errorf("failed to find %q market in harness config. Available markets: %s", market, strings.Join(markets, ", "))
	}

	baseAssetCfg = mktsCfg.Assets[fmt.Sprintf("%s_simnet", strings.ToUpper(baseSymbol))]
	quoteAssetCfg = mktsCfg.Assets[fmt.Sprintf("%s_simnet", strings.ToUpper(quoteSymbol))]
	if baseAssetCfg == nil || quoteAssetCfg == nil {
		return errors.New("asset configuration missing from markets.json")
	}

	// Load the asset node configs. We'll need the wallet RPC addresses.
	// Note that the RPC address may be modified if toxiproxy is used below.
	alphaCfgBase = simharness.LoadNodeConfig(baseSymbol, alpha)
	betaCfgBase = simharness.LoadNodeConfig(baseSymbol, beta)
	alphaCfgQuote = simharness.LoadNodeConfig(quoteSymbol, alpha)
	betaCfgQuote = simharness.LoadNodeConfig(quoteSymbol, beta)

	loggerMaker, err = dex.NewLoggerMaker(os.Stdout, logLevel)
	if err != nil {
		return fmt.Errorf("error creating LoggerMaker: %v", err)
	}
	log /* global */ = loggerMaker.NewLogger("LOADBOT", dex.LevelInfo)

	log.Infof("Running program %s", programName)

	getAddress := func(symbol, node string) (string, error) {
		var args []string
		switch symbol {
		case btc, ltc:
			args = []string{"getnewaddress", "''", "bech32"}
		case doge, bch, zec:
			args = []string{"getnewaddress"}
		case dcr:
			args = []string{"getnewaddress", "default", "ignore"}
		case eth:
			args = []string{"attach", `--exec eth.accounts[1]`}
		default:
			return "", fmt.Errorf("getAddress: unknown symbol %q", symbol)
		}
		res := <-simharness.Ctl(ctx, symbol, fmt.Sprintf("./%s", node), args...)
		if res.Err != nil {
			return "", fmt.Errorf("error getting %s address: %v", symbol, res.Err)
		}
		return strings.Trim(res.Output, `"`), nil
	}

	if alphaAddrBase, err = getAddress(baseSymbol, alpha); err != nil {
		return err
	}
	if betaAddrBase, err = getAddress(baseSymbol, beta); err != nil {
		return err
	}
	if alphaAddrQuote, err = getAddress(quoteSymbol, alpha); err != nil {
		return err
	}
	if betaAddrQuote, err = getAddress(quoteSymbol, beta); err != nil {
		return err
	}

	// Unlock wallets, since they may have been locked on a previous shutdown.
	if err = simharness.UnlockWallet(ctx, baseSymbol); err != nil {
		return err
	}
	if err = simharness.UnlockWallet(ctx, quoteSymbol); err != nil {
		return err
	}

	// Clean up the directory.
	os.RemoveAll(botDir)
	err = os.MkdirAll(botDir, 0700)
	if err != nil {
		return fmt.Errorf("error creating loadbot directory: %v", err)
	}

	// Run any specified network conditions.
	var toxics toxiproxy.Toxics
	if latency500 {
		toxics = append(toxics, toxiproxy.Toxic{
			Name:     "latency-500",
			Type:     "latency",
			Toxicity: 1,
			Attributes: toxiproxy.Attributes{
				"latency": 500,
			},
		})
	}
	if shaky500 {
		toxics = append(toxics, toxiproxy.Toxic{
			Name:     "shaky-500",
			Type:     "latency",
			Toxicity: 1,
			Attributes: toxiproxy.Attributes{
				"latency": 500,
				"jitter":  500,
			},
		})
	}

	if slow100 {
		toxics = append(toxics, toxiproxy.Toxic{
			Name:     "slow-100",
			Type:     "bandwidth",
			Toxicity: 1,
			Attributes: toxiproxy.Attributes{
				"rate": 100,
			},
		})
	}

	if spotty20 {
		toxics = append(toxics, toxiproxy.Toxic{
			Name:     "spotty-20",
			Type:     "timeout",
			Toxicity: 1,
			Attributes: toxiproxy.Attributes{
				"timeout": 20_000,
			},
		})
	}

	if len(toxics) > 0 {
		toxiClient := toxiproxy.NewClient(":8474") // Default toxiproxy address

		pairs := []*struct {
			assetID uint32
			cfg     map[string]string
		}{
			{baseID, alphaCfgBase},
			{baseID, betaCfgBase},
			{quoteID, alphaCfgQuote},
			{quoteID, betaCfgQuote},
		}

		for _, pair := range pairs {
			var walletAddr, newAddr string
			newAddr, port, err := nextAddr()
			if err != nil {
				return fmt.Errorf("unable to get a new port: %v", err)
			}
			switch pair.assetID {
			case dcrID:
				walletAddr = pair.cfg["rpclisten"]
				pair.cfg["rpclisten"] = newAddr
			case btcID, ltcID, dogeID, bchID:
				oldPort := pair.cfg["rpcport"]
				walletAddr = "127.0.0.1:" + oldPort
				pair.cfg["rpcport"] = port
			case ethID:
				oldPort := pair.cfg["ListenAddr"]
				walletAddr = fmt.Sprintf("127.0.0.1%s", oldPort)
				pair.cfg["ListenAddr"] = fmt.Sprintf(":%s", port)
			}
			for _, toxic := range toxics {
				name := fmt.Sprintf("%s_%d_%s", toxic.Name, pair.assetID, port)
				proxy, err := toxiClient.CreateProxy(name, newAddr, walletAddr)
				if err != nil {
					return fmt.Errorf("failed to create %s proxy: %v", toxic.Name, err)
				}
				defer proxy.Delete()
			}
		}

		for _, toxic := range toxics {
			log.Infof("Adding network condition %s", toxic.Name)
			newAddr, _, err := nextAddr()
			if err != nil {
				return fmt.Errorf("unable to get a new port: %v", err)
			}
			proxy, err := toxiClient.CreateProxy("dcrdex_"+toxic.Name, newAddr, simharness.Host)
			if err != nil {
				return fmt.Errorf("failed to create %s proxy for host: %v", toxic.Name, err)
			}
			hostAddr = newAddr
			defer proxy.Delete()
		}
	}

	tStart := time.Now()

	// Run the specified program.
	switch programName {
	case "pingpong", "pingpong1":
		runPingPong(1)
	case "pingpong2":
		runPingPong(2)
	case "pingpong3":
		runPingPong(3)
	case "pingpong4":
		runPingPong(4)
	case "sidestacker":
		runSideStacker(5, 3)
	case "compound":
		runCompound()
	case "heavy":
		runHeavy()
	default:
		log.Criticalf("program " + programName + " not known")
	}

	if orderCounter > 0 {
		since := time.Since(tStart)
		rate := float64(matchCounter) / (float64(since) / float64(time.Minute))
		log.Infof("LoadBot ran for %s, during which time %d orders were placed, resulting in %d separate matches, a rate of %.3g matches / minute",
			since, orderCounter, matchCounter, rate)
	}
	return nil
}
