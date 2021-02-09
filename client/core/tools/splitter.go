package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"github.com/decred/dcrd/rpcclient/v6"
)

const (
	minOutputDenom     = 4
	lotSizeBufferNum   = 12
	lotSizeBufferDenom = 10
)

// UTXOSplitter is a utility to monitor a Decred or Bitcoin(-clone) wallet relay
// account, and when a balance is detected, transfer the balance to the dex
// account in N-lot sized outputs. The output value spectrum is dependent on the
// amount being transferred. The algorithm is implemented in splitUp.
// UTXOSplitter polls the wallet backend for available balance every minute.
type UTXOSplitter struct {
	host     string
	symbol   string
	assetID  uint32
	mktID    string
	core     *core.Core
	bookFeed *core.BookFeed // Only used for quote assets.
	cl       *rpcclient.Client
	acct     string
	log      dex.Logger
	// feeBank is the minimum amount the splitter will maintain in the
	// relay account to cover fees.
	feeBank uint64
	feeMult float64
}

// SplitterConfig is the configuration settings for a UTXOSplitter.
type SplitterConfig struct {
	Host        string
	AssetSymbol string
	// MarketName is required. If the asset is only ever used as a base asset
	// the specific market is not important, since the lot size is a dex-wide
	// setting (Note: that might change soon). If the asset is the quote asset
	// the market is important so that quote-converted lot sizes can be
	// estimated.
	MarketName string
	// AccountName is the name of the relay account/wallet. This account should
	// be set up manually by the user ahead of time. The relay account will be
	// monitored, and when funds are detected, they are sent to the DEX wallet.
	// The account or wallet specified by AccountName is assumed to be unlocked.
	AccountName string
	// FeeBank is the minimum amount the splitter will maintain in the
	// relay account to cover fees.
	FeeBank uint64
	// FeeCoefficient is a fee rate multiplier. Fee rate is taken from the
	// estimatesmartfee RPC with a confirmation target of 1. The rate is then
	// multiplied by the FeeCoefficient. Bumping fees for the pre-split should
	// result in the subsequently funded swap transaction being mined faster.
	FeeCoefficient float64
	Core           *core.Core
	Logger         dex.Logger
}

// NewUTXOSplitter is the constructor for a *UTXOSPlitter.
func NewUTXOSplitter(ctx context.Context, cfg *SplitterConfig) (*UTXOSplitter, error) {
	assetID, found := dex.BipSymbolID(cfg.AssetSymbol)
	if !found {
		return nil, fmt.Errorf("asset %s not known", cfg.AssetSymbol)
	}

	settings, err := cfg.Core.WalletSettings(assetID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving wallet settings for %s", cfg.AssetSymbol)
	}

	var rpcConf *rpcclient.ConnConfig
	if cfg.AssetSymbol == "dcr" {
		rpcConf, err = dcrClientConfig(settings)
		if err != nil {
			return nil, fmt.Errorf("dcrdClientConfig error: %v", err)
		}

	} else {
		rpcConf = btcClientConfig(settings)
	}

	cl, err := rpcclient.New(rpcConf, nil)
	if err != nil {
		return nil, err
	}

	s := &UTXOSplitter{
		host:    cfg.Host,
		symbol:  cfg.AssetSymbol,
		assetID: assetID,
		mktID:   cfg.MarketName,
		core:    cfg.Core,
		cl:      cl,
		acct:    cfg.AccountName,
		log:     cfg.Logger,
		feeBank: cfg.FeeBank,
		feeMult: cfg.FeeCoefficient,
	}

	go s.run(ctx)

	return s, nil
}

// run is the main splitter goroutine. Periodicallys checks the balance and
// initializes withdraw.
func (s *UTXOSplitter) run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.checkBalance(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// req sends a RawRequest to the wallet RPC.
func (s *UTXOSplitter) req(ctx context.Context, method string, args []interface{}, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := s.cl.RawRequest(ctx, method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %w", err)
	}
	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}

// userExchangeMarket gets the *core.User, *core.Exchange, and *core.Market for
// the splitter configuration.
func (s *UTXOSplitter) userExchangeMarket() (*core.User, *core.Exchange, *core.Market, error) {
	u := s.core.User()
	if u == nil {
		return nil, nil, nil, fmt.Errorf("no core user found")
	}

	xc := u.Exchanges[s.host]
	if xc == nil {
		return nil, nil, nil, fmt.Errorf("no core.Exchange for %s", s.host)
	}

	mkt := xc.Markets[s.mktID]
	if mkt == nil {
		return nil, nil, nil, fmt.Errorf("no core.Market for %s", s.mktID)
	}

	return u, xc, mkt, nil
}

// checkBalance gets checks the available relay account balance, and if it is
// big enough, initializes the split relay.
func (s *UTXOSplitter) checkBalance(ctx context.Context) {
	var bal float64
	var err error
	if s.symbol == "dcr" {
		resp := &struct {
			Available float64 `json:"totalspendable"`
		}{}
		// args acct_name, min_confs
		err = s.req(ctx, "getbalance", []interface{}{s.acct, 0}, resp)
		bal = resp.Available
	} else {
		// arg 1 must be "*", arg 2 is min confs
		err = s.req(ctx, "getbalance", []interface{}{"*", 0}, &bal)
	}
	if err != nil {
		s.log.Errorf("error retrieving balance for %s: %v", s.symbol, err)
		return
	}

	u, xc, mkt, err := s.userExchangeMarket()
	if err != nil {
		s.log.Errorf(err.Error())
		return
	}

	baseCfg := xc.Assets[mkt.BaseID]
	quoteCfg := xc.Assets[mkt.QuoteID]
	if baseCfg == nil || quoteCfg == nil {
		s.log.Errorf("missing asset config. base found: %t, quote found: %t", baseCfg != nil, quoteCfg != nil)
		return
	}

	// Get the lot size.
	lotSize := baseCfg.LotSize
	// If our asset is the market's quote asset, we need to estimate the lot
	// size based on the current market conditions.
	if s.assetID == mkt.QuoteID {
		if s.bookFeed == nil {
			feed, err := s.core.SyncBook(s.host, mkt.BaseID, mkt.QuoteID)
			if err != nil {
				s.log.Errorf("failed to get a book feed for %s at %s", s.mktID, s.host)
				return
			}
			s.bookFeed = feed
			go func() {
				<-ctx.Done()
				s.bookFeed.Close()
			}()
		}
		book, err := s.core.Book(s.host, mkt.BaseID, mkt.QuoteID)
		if err != nil {
			s.log.Errorf("error getting %s order book for: %v", s.mktID, err)
			return
		}
		if len(book.Sells) == 0 {
			if len(book.Buys) == 0 {
				// Just an estimate, I guess
				lotSize = quoteCfg.LotSize
			} else {
				lotSize = calc.BaseToQuote(toAtoms(book.Buys[0].Rate*1e8), baseCfg.LotSize)
			}
		} else {
			if len(book.Buys) == 0 {
				lotSize = calc.BaseToQuote(toAtoms(book.Sells[0].Rate*1e8), baseCfg.LotSize)
			} else {
				lotSize = calc.BaseToQuote(toAtoms((book.Sells[0].Rate+book.Buys[0].Rate)*1e8/2), baseCfg.LotSize)
			}
		}
	}

	// Make sure we can get a fee estimate.
	var feeRate float64
	if s.symbol == "dcr" {
		err = s.req(ctx, "estimatesmartfee", []interface{}{1}, &feeRate)
	} else {
		res := &struct {
			FeeRate float64  `json:"feerate"`
			Errors  []string `json:"errors"`
		}{}
		err = s.req(ctx, "estimatesmartfee", []interface{}{1}, res)
		feeRate = res.FeeRate
	}
	if err != nil {
		s.log.Errorf("error estimating %s fee rate: %v", s.symbol, err)
		return
	}
	feeRate *= s.feeMult

	atoms := toAtoms(bal)

	if atoms > lotSize/minOutputDenom {
		s.transfer(ctx, atoms, lotSize, feeRate, u)
	}
}

// transfer sends the relay account balance to the DEX account as split outputs.
func (s *UTXOSplitter) transfer(ctx context.Context, bal, lotSize uint64, feeRate float64, u *core.User) {
	addr, err := s.core.NewDepositAddress(s.assetID)
	if err != nil {
		s.log.Errorf("NewDepositAddress error: %v", err)
		return
	}

	splits := split(bal-s.feeBank, lotSize)

	if len(splits) == 0 {
		s.log.Errorf("no splits. this shouldn't happen.")
		return
	}

	type send struct {
		Address float64 `json:"address"`
	}

	sends := make([]map[string]float64, 0, len(splits))
	for _, v := range splits {
		sends = append(sends, map[string]float64{addr: float64(v) / 1e8})
	}

	var txHex string
	err = s.req(ctx, "createrawtransaction", []interface{}{[]struct{}{}, sends}, &txHex)
	if err != nil {
		s.log.Errorf("createrawtransaction error: %v", err)
		return
	}

	fundRes := &struct {
		Hex string `json:"hex"`
	}{}
	if s.symbol == "dcr" {
		err = s.req(ctx, "fundrawtransaction", []interface{}{txHex, s.acct, map[string]interface{}{"feerate": feeRate}}, fundRes)
	} else {
		err = s.req(ctx, "fundrawtransaction", []interface{}{txHex, map[string]interface{}{"feeRate": feeRate}}, fundRes)
	}
	if err != nil {
		s.log.Errorf("fundrawtransaction error: %v", err)
		return
	}
	txHex = fundRes.Hex

	err = s.req(ctx, "signrawtransaction", []interface{}{txHex}, &txHex)
	if err != nil {
		s.log.Errorf("signrawtransaction error: %v", err)
		return
	}

	var txid string
	err = s.req(ctx, "sendrawtransaction", []interface{}{txHex}, &txid)
	if err != nil {
		s.log.Errorf("sendrawtransaction error: %v", err)
		return
	}
	s.log.Infof("Sent %s splits %v", s.symbol, splits)
}

// split creates a set of split transactions based on a base lot size. The sizes
// are calculated based on the algorithm in splitUp. After the initial split
// algorithm, any remainder bigger than lotSize / minOutputDenom will be added
// as an additional output.
func split(amt, lotSize uint64) (splits []uint64) {
	var newSplits []uint64
	remainder := amt
	for {
		newSplits, remainder = splitUp(remainder, lotSize)
		if len(newSplits) == 0 {
			break
		}
		splits = append(splits, newSplits...)
	}
	// If there's mroe than a quarter lot left, add a split for that too.
	if remainder > lotSize/minOutputDenom {
		splits = append(splits, remainder)
	}
	return
}

// splitUp generates a series of split sizes based on an amount available and
// a lotSize. Splits will be in integer multiples of a buffered lot size.
// Lot size is buffered by (lotSizeBufferNum / lotSizeBufferDenom).
// Algo is 2x1 lot + 2x2 lots + 2x3 lots... until exhausted, then return the
// remainder. The remainder may be more than a mega-lot. splitUp should be
// run in a loop until no further splits are available.
func splitUp(amt, lotSize uint64) (splits []uint64, remainder uint64) {
	megaLotSize := lotSize * lotSizeBufferNum / lotSizeBufferNum
	i := 0
	for {
		if amt < megaLotSize {
			return splits, amt
		}
		coefficient := uint64(i/2 + 1)
		split := coefficient * megaLotSize
		if amt < split {
			return splits, amt
		}
		splits = append(splits, split)
		amt -= split
		i++
	}
}

// For BTC-based assets, parsed the rpcbind parameter based on the wallet
// settings.
func parseRPCBind(settings map[string]string) string {
	// RPCPort overrides network default
	var host, port string
	if settings["rpcport"] != "0" {
		port = settings["rpcport"]
	}

	// if RPCBind includes a port, it takes precedence over RPCPort
	if settings["rpcbind"] != "" {
		h, p, err := net.SplitHostPort(settings["rpcbind"])
		if err != nil {
			// Will error for i.e. "localhost", but not for "localhost:" or ":1234"
			host = settings["rpcbind"]
		} else {
			if h != "" {
				host = h
			}
			if p != "" {
				port = p
			}
		}
	}

	// overwrite rpcbind to use for rpcclient connection
	return net.JoinHostPort(host, port)
}

// dcrdClientConfig parses the rpcclient.Client configuration from the Decred
// wallet settings.
func dcrClientConfig(settings map[string]string) (*rpcclient.ConnConfig, error) {
	certs, err := ioutil.ReadFile(settings["rpccert"])
	if err != nil {
		return nil, fmt.Errorf("TLS certificate read error: %w", err)
	}

	return &rpcclient.ConnConfig{
		Host:         settings["rpclisten"],
		HTTPPostMode: true,
		User:         settings["username"],
		Pass:         settings["password"],
		Certificates: certs,
	}, nil
}

// btcClientConfig parses the rpcclient.Client configuration from the
// Bitcoin(-clone) wallet settings.
func btcClientConfig(settings map[string]string) *rpcclient.ConnConfig {
	endpoint := parseRPCBind(settings) + "/wallet/" + settings["walletname"]
	return &rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         endpoint,
		User:         settings["rpcuser"],
		Pass:         settings["rpcpassword"],
	}
}

// toAtoms converts a float value to integer via a factor of 1e8.
func toAtoms(amt float64) uint64 {
	return uint64(math.Round(amt * 1e8))
}
