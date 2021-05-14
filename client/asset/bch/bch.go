// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbch "decred.org/dcrdex/dex/networks/bch"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/gcash/bchd/bchec"
	bchscript "github.com/gcash/bchd/txscript"
	bchwire "github.com/gcash/bchd/wire"
)

const (
	version = 0

	// BipID is the Bip 44 coin ID for Bitcoin Cash.
	BipID = 145
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee        = 100
	minNetworkVersion = 221100
	walletTypeRPC     = "bitcoindRPC"
)

var (
	NetPorts = dexbtc.NetPorts{
		Mainnet: "8332",
		Testnet: "18332",
		Simnet:  "18443",
	}
	fallbackFeeKey = "fallbackfee"
	configOpts     = []*asset.ConfigOption{
		{
			Key:         "walletname",
			DisplayName: "Wallet Name",
			Description: "The wallet name",
		},
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Bitcoin Cash 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Bitcoin Cash 'rpcpassword' setting",
			NoEcho:      true,
		},
		{
			Key:         "rpcbind",
			DisplayName: "JSON-RPC Address",
			Description: "<addr> or <addr>:<port> (default 'localhost')",
		},
		{
			Key:         "rpcport",
			DisplayName: "JSON-RPC Port",
			Description: "Port for RPC connections (if not set in Address)",
		},
		{
			Key:          fallbackFeeKey,
			DisplayName:  "Fallback fee rate",
			Description:  "Bitcoin Cash 'fallbackfee' rate. Units: BCH/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
		},
	}
	// WalletInfo defines some general information about a Bitcoin Cash wallet.
	WalletInfo = &asset.WalletInfo{
		Name:    "Bitcoin Cash",
		Version: version,
		// Same as bitcoin. That's dumb.
		UnitInfo: dexbch.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to bitcoind",
			DefaultConfigPath: dexbtc.SystemConfigPath("bitcoin"), // Same as bitcoin. That's dumb.
			ConfigOpts:        configOpts,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	switch walletType {
	case "", walletTypeRPC:
		_, client, err := btc.ParseRPCWalletConfig(settings, "bch", net, NetPorts)
		if err != nil {
			return false, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		_, err = client.RawRequest(ctx, "getnetworkinfo", nil)
		return err == nil, nil
	}

	return false, fmt.Errorf("no Bitcoin Cash wallet of type %q available", walletType)
}

func (d *Driver) Create(*asset.CreateWalletParams) error {
	return fmt.Errorf("no creatable wallet types")
}

// Open creates the BCH exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Bitcoin Cash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Bitcoin Cash and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the daemon config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexbch.MainNetParams
	case dex.Testnet:
		params = dexbch.TestNet3Params
	case dex.Regtest:
		params = dexbch.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file. Bitcoin Cash uses the same default
	// ports as Bitcoin.
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:          cfg,
		MinNetworkVersion:  minNetworkVersion,
		WalletInfo:         WalletInfo,
		Symbol:             "bch",
		Logger:             logger,
		Network:            network,
		ChainParams:        params,
		Ports:              NetPorts,
		DefaultFallbackFee: defaultFee,
		Segwit:             false,
		LegacyBalance:      true,
		// Bitcoin Cash uses the Cash Address encoding, which is Bech32, but
		// not indicative of segwit. We provide a custom encoder.
		AddressDecoder: dexbch.DecodeCashAddress,
		// Bitcoin Cash has a custom signature hash algorithm. Since they don't
		// have segwit, Bitcoin Cash implemented a variation of the withdrawn
		// BIP0062 that utilizes Shnorr signatures.
		// https://gist.github.com/markblundeberg/a3aba3c9d610e59c3c49199f697bc38b#making-unmalleable-smart-contracts
		// https://github.com/bitcoin/bips/blob/master/bip-0062.mediawiki
		NonSegwitSigner: rawTxInSigner,
		// The old allowHighFees bool argument to sendrawtransaction.
		ArglessChangeAddrRPC: true,
		// Bitcoin Cash uses estimatefee instead of estimatesmartfee, and even
		// then, they modified it from the old Bitcoin Core estimatefee by
		// removing the confirmation target argument.
		FeeEstimator: estimateFee,
	}

	xcWallet, err := btc.BTCCloneWallet(cloneCFG)
	if err != nil {
		return nil, err
	}

	return &BCHWallet{
		ExchangeWallet: xcWallet,
	}, nil
}

// BCHWallet embeds btc.ExchangeWallet, but re-implements a couple of methods to
// perform on-the-fly address translation.
type BCHWallet struct {
	*btc.ExchangeWallet
}

// Address converts the Bitcoin base58-encoded address returned by the embedded
// ExchangeWallet into a Cash Address.
func (bch *BCHWallet) Address() (string, error) {
	btcAddrStr, err := bch.ExchangeWallet.Address()
	if err != nil {
		return "", err
	}
	return dexbch.RecodeCashAddress(btcAddrStr, bch.Net())
}

// AuditContract modifies the *asset.Contract returned by the ExchangeWallet
// AuditContract method by converting the Recipient to the Cash Address
// encoding.
func (bch *BCHWallet) AuditContract(coinID, contract, txData dex.Bytes, matchTime time.Time) (*asset.AuditInfo, error) { // AuditInfo has address
	ai, err := bch.ExchangeWallet.AuditContract(coinID, contract, txData, matchTime)
	if err != nil {
		return nil, err
	}
	ai.Recipient, err = dexbch.RecodeCashAddress(ai.Recipient, bch.Net())
	if err != nil {
		return nil, err
	}
	return ai, nil
}

// RefundAddress extracts and returns the refund address from a contract.
func (bch *BCHWallet) RefundAddress(contract dex.Bytes) (string, error) {
	addr, err := bch.ExchangeWallet.RefundAddress(contract)
	if err != nil {
		return "", err
	}
	return dexbch.RecodeCashAddress(addr, bch.Net())
}

// rawTxSigner signs the transaction using Bitcoin Cash's custom signature
// hash and signing algorithm.
func rawTxInSigner(btcTx *wire.MsgTx, idx int, subScript []byte, hashType txscript.SigHashType, btcKey *btcec.PrivateKey, val uint64) ([]byte, error) {
	bchTx, err := translateTx(btcTx)
	if err != nil {
		return nil, fmt.Errorf("btc->bch wire.MsgTx translation error: %v", err)
	}

	bchKey, _ := bchec.PrivKeyFromBytes(bchec.S256(), btcKey.Serialize())

	return bchscript.RawTxInECDSASignature(bchTx, idx, subScript, bchscript.SigHashType(uint32(hashType)), bchKey, int64(val))
}

// serializeBtcTx serializes the wire.MsgTx.
func serializeBtcTx(msgTx *wire.MsgTx) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, msgTx.SerializeSize()))
	err := msgTx.Serialize(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// estimateFee uses Bitcoin Cash's estimatefee RPC, since estimatesmartfee
// is not implemented.
func estimateFee(node btc.RawRequester, confTarget uint64) (uint64, error) {
	resp, err := node.RawRequest("estimatefee", nil)
	if err != nil {
		return 0, err
	}
	var feeRate float64
	err = json.Unmarshal(resp, &feeRate)
	if err != nil {
		return 0, err
	}
	if feeRate <= 0 {
		return 0, fmt.Errorf("fee could not be estimated")
	}
	return uint64(math.Round(feeRate * 1e5)), nil
}

// translateTx converts the btcd/*wire.MsgTx into a bchd/*wire.MsgTx.
func translateTx(btcTx *wire.MsgTx) (*bchwire.MsgTx, error) {
	txB, err := serializeBtcTx(btcTx)
	if err != nil {
		return nil, err
	}

	bchTx := bchwire.NewMsgTx(bchwire.TxVersion)
	err = bchTx.Deserialize(bytes.NewBuffer(txB))
	if err != nil {
		return nil, err
	}

	return bchTx, nil
}
