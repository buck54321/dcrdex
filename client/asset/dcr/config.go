// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
)

const (
	defaultMainnet  = "localhost:9110"
	defaultTestnet3 = "localhost:19110"
	defaultSimnet   = "localhost:19557"

	defaultCSPPMainnet  = "mix.decred.org:5760"
	defaultCSPPTestnet3 = "mix.decred.org:15760"
)

var (
	// A global *chaincfg.Params will be set if loadConfig completes without
	// error.
	dcrwHomeDir       = dcrutil.AppDataDir("dcrwallet", false)
	defaultRPCCert    = filepath.Join(dcrwHomeDir, "rpc.cert")
	defaultConfigPath = filepath.Join(dcrwHomeDir, "dcrwallet.conf")

	// May 26, 2022
	defaultWalletBirthdayUnix = 1653599386
)

type walletConfig struct {
	UseSplitTx       bool    `ini:"txsplit"`
	FallbackFeeRate  float64 `ini:"fallbackfee"`
	FeeRateLimit     float64 `ini:"feeratelimit"`
	RedeemConfTarget uint64  `ini:"redeemconftarget"`
	ActivelyUsed     bool    `ini:"special_activelyUsed"` //injected by core
	ApiFeeFallback   bool    `ini:"apifeefallback"`
	VSPURL           string  `ini:"vspurl"`
}

type rpcConfig struct {
	XCWalletAccounts        //  part of the rpc config options
	RPCUser          string `ini:"username"`
	RPCPass          string `ini:"password"`
	RPCListen        string `ini:"rpclisten"`
	RPCCert          string `ini:"rpccert"`
}

type spvConfig struct {
	MixFunds     bool   `ini:"mixfunds"`
	CSPPServer   string `ini:"csppserver"`
	CSPPServerCA string `ini:"csppserver.ca"`

	dialCSPPServer func(ctx context.Context, network, addr string) (net.Conn, error)
}

func loadRPCConfig(settings map[string]string, network dex.Network) (*rpcConfig, *chaincfg.Params, error) {
	cfg := new(rpcConfig)
	chainParams, err := loadConfig(settings, network, cfg)
	if err != nil {
		return nil, nil, err
	}

	var defaultServer string
	switch network {
	case dex.Simnet:
		defaultServer = defaultSimnet
	case dex.Testnet:
		defaultServer = defaultTestnet3
	case dex.Mainnet:
		defaultServer = defaultMainnet
	default:
		return nil, nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}
	if cfg.RPCListen == "" {
		cfg.RPCListen = defaultServer
	}
	if cfg.RPCCert == "" {
		cfg.RPCCert = defaultRPCCert
	} else {
		cfg.RPCCert = dex.CleanAndExpandPath(cfg.RPCCert)
	}

	// Both UnmixedAccount and TradingAccount must be provided if primary
	// account is a mixed account. Providing one but not the other is bad
	// configuration. If set, the account names will be validated on Connect.
	if (cfg.UnmixedAccount == "") != (cfg.TradingAccount == "") {
		return nil, nil, fmt.Errorf("'Change Account Name' and 'Temporary Trading Account' MUST "+
			"be set to treat %[1]q as a mixed account. If %[1]q is not a mixed account, values "+
			"should NOT be set for 'Change Account Name' and 'Temporary Trading Account'",
			cfg.PrimaryAccount)
	}
	if cfg.UnmixedAccount != "" {
		switch {
		case cfg.PrimaryAccount == cfg.UnmixedAccount:
			return nil, nil, fmt.Errorf("Primary Account should not be the same as Change Account")
		case cfg.PrimaryAccount == cfg.TradingAccount:
			return nil, nil, fmt.Errorf("Primary Account should not be the same as Temporary Trading Account")
		case cfg.TradingAccount == cfg.UnmixedAccount:
			return nil, nil, fmt.Errorf("Temporary Trading Account should not be the same as Change Account")
		}
	}

	return cfg, chainParams, nil
}

func loadSPVConfig(settings map[string]string, network dex.Network) (*spvConfig, *chaincfg.Params, error) {
	cfg := new(spvConfig)
	chainParams, err := loadConfig(settings, network, cfg)
	if err != nil {
		return nil, nil, err
	}

	if cfg.CSPPServer == "" {
		switch network {
		case dex.Simnet:
			cfg.CSPPServer = defaultCSPPMainnet
		case dex.Testnet:
			cfg.CSPPServer = defaultCSPPTestnet3
		default:
			if cfg.MixFunds {
				return nil, nil, fmt.Errorf("cspp funds mixing not supported for network ID %d", uint8(network))
			}
		}
	}

	if cfg.CSPPServer != "" {
		csppTLSConfig := new(tls.Config)

		csppTLSConfig.ServerName, _, err = net.SplitHostPort(cfg.CSPPServer)
		if err != nil {
			return nil, nil, fmt.Errorf("Cannot parse CoinShuffle++ "+
				"server name %q: %v", cfg.CSPPServer, err)
		}

		if cfg.CSPPServerCA != "" {
			cfg.CSPPServerCA = dex.CleanAndExpandPath(cfg.CSPPServerCA)
			ca, err := os.ReadFile(cfg.CSPPServerCA)
			if err != nil {
				return nil, nil, fmt.Errorf("Cannot read CoinShuffle++ "+
					"Certificate Authority file: %v", err)
			}
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(ca)
			csppTLSConfig.RootCAs = pool
		}

		cfg.dialCSPPServer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := new(net.Dialer).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			conn = tls.Client(conn, csppTLSConfig)
			return conn, nil
		}
	}

	return cfg, chainParams, nil
}

// loadConfig loads the Config from a settings map. If no values are found for
// RPCListen or RPCCert in the specified file, default values will be used. If
// there is no error, the module-level chainParams variable will be set
// appropriately for the network.
func loadConfig(settings map[string]string, network dex.Network, cfg interface{}) (*chaincfg.Params, error) {
	if err := config.Unmapify(settings, cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	return parseChainParams(network)
}

func parseChainParams(network dex.Network) (*chaincfg.Params, error) {
	// Get network settings. Zero value is mainnet, but unknown non-zero cfg.Net
	// is an error.
	switch network {
	case dex.Simnet:
		return chaincfg.SimNetParams(), nil
	case dex.Testnet:
		return chaincfg.TestNet3Params(), nil
	case dex.Mainnet:
		return chaincfg.MainNetParams(), nil
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}
}
