// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset/btc/electrum"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

type electrumWalletClient interface {
	FeeRate(confTarget int64) (int64, error)
	Broadcast(tx []byte) (string, error)
	RemoveLocalTx(txid string) error
	Commands() ([]string, error)
	GetInfo() (*electrum.GetInfoResult, error)
	GetServers() ([]*electrum.GetServersResult, error)
	GetBalance() (*electrum.Balance, error)
	ListUnspent() ([]*electrum.ListUnspentResult, error)
	FreezeAddress(addr string) error
	UnfreezeAddress(addr string) error
	FreezeUTXO(txid string, out uint32) error
	UnfreezeUTXO(txid string, out uint32) error
	CreateNewAddress() (string, error)
	GetUnusedAddress() (string, error)
	CheckAddress(addr string) (valid, mine bool, err error)
	SignTx(walletPass string, psbtB64 string) ([]byte, error)
	GetPrivateKeys(walletPass, addr string) (string, error)
	PayTo(walletPass string, addr string, amtBTC float64, feeRate float64) ([]byte, error)
	PayToFromCoinsAbsFee(walletPass string, fromCoins []string, addr string, amtBTC float64, absFee float64) ([]byte, error)
	Sweep(walletPass string, addr string, feeRate float64) ([]byte, error)
	GetWalletTxConfs(txid string) (int, error)     // shortcut if owned
	GetRawTransaction(txid string) ([]byte, error) // wallet method
	GetAddressHistory(addr string) ([]*electrum.GetAddressHistoryResult, error)
	GetAddressUnspent(addr string) ([]*electrum.GetAddressUnspentResult, error)
}

type electrumNetworkClient interface {
	Done() <-chan struct{}
	Shutdown()
	Features(ctx context.Context) (*electrum.ServerFeatures, error)
	GetTransaction(ctx context.Context, txid string) (*electrum.GetTransactionResult, error)
	BlockHeader(ctx context.Context, height uint32) (string, error)
	BlockHeaders(ctx context.Context, startHeight, count uint32) (*electrum.GetBlockHeadersResult, error)
}

// electrumNetworkManager provides a thread-safe way to use and reset the
// mutable electrumNetworkClient field. Could replace this with a getter/setter
// on electrumWallet, but that gets a little messy with defers capturing a
// potentially old/replaced electrumNetworkClient.
type electrumNetworkManager struct {
	sync.RWMutex
	electrumNetworkClient
}

func (enm *electrumNetworkManager) reset(cl electrumNetworkClient) {
	enm.Lock()
	defer enm.Unlock()
	enm.electrumNetworkClient = cl
}

func (enm *electrumNetworkManager) Done() <-chan struct{} {
	enm.RLock()
	defer enm.RUnlock()
	return enm.electrumNetworkClient.Done()
}

func (enm *electrumNetworkManager) Shutdown() {
	enm.RLock()
	defer enm.RUnlock()
	enm.electrumNetworkClient.Shutdown()
}

func (enm *electrumNetworkManager) GetTransaction(ctx context.Context, txid string) (*electrum.GetTransactionResult, error) {
	enm.RLock()
	cl := enm.electrumNetworkClient
	enm.RUnlock()
	return cl.GetTransaction(ctx, txid)
}

func (enm *electrumNetworkManager) BlockHeader(ctx context.Context, height uint32) (string, error) {
	enm.RLock()
	cl := enm.electrumNetworkClient
	enm.RUnlock()
	return cl.BlockHeader(ctx, height)
}

func (enm *electrumNetworkManager) BlockHeaders(ctx context.Context, startHeight, count uint32) (*electrum.GetBlockHeadersResult, error) {
	enm.RLock()
	cl := enm.electrumNetworkClient
	enm.RUnlock()
	return cl.BlockHeaders(ctx, startHeight, count)
}

type electrumWallet struct {
	log           dex.Logger
	chainParams   *chaincfg.Params
	decodeAddr    dexbtc.AddressDecoder
	stringAddr    dexbtc.AddressStringer
	deserializeTx func([]byte) (*wire.MsgTx, error)
	serializeTx   func(*wire.MsgTx) ([]byte, error)
	wallet        electrumWalletClient
	chain         *electrumNetworkManager
	segwit        bool

	lockedOutpointsMtx sync.RWMutex
	lockedOutpoints    map[outPoint]struct{}

	pwMtx    sync.RWMutex
	pw       string
	unlocked bool
}

type electrumWalletConfig struct {
	params         *chaincfg.Params
	log            dex.Logger
	addrDecoder    dexbtc.AddressDecoder
	addrStringer   dexbtc.AddressStringer
	txDeserializer func([]byte) (*wire.MsgTx, error)
	txSerializer   func(*wire.MsgTx) ([]byte, error)
	segwit         bool // indicates if segwit addresses are expected from requests
}

func newElectrumWallet(ew electrumWalletClient, cfg *electrumWalletConfig) *electrumWallet {
	addrDecoder := cfg.addrDecoder
	if addrDecoder == nil {
		addrDecoder = btcutil.DecodeAddress
	}

	addrStringer := cfg.addrStringer
	if addrStringer == nil {
		addrStringer = func(addr btcutil.Address, _ *chaincfg.Params) (string, error) {
			return addr.String(), nil
		}
	}

	txDeserializer := cfg.txDeserializer
	if txDeserializer == nil {
		txDeserializer = msgTxFromBytes
	}
	txSerializer := cfg.txSerializer
	if txSerializer == nil {
		txSerializer = serializeMsgTx
	}

	return &electrumWallet{
		log:           cfg.log,
		chainParams:   cfg.params,
		decodeAddr:    addrDecoder,
		stringAddr:    addrStringer,
		deserializeTx: txDeserializer,
		serializeTx:   txSerializer,
		wallet:        ew,
		segwit:        cfg.segwit,
		// chain is constructed after wallet connects to a server
		lockedOutpoints: make(map[outPoint]struct{}),
	}
}

// BEGIN unimplemented asset.Wallet methods

func (ew *electrumWallet) RawRequest(string, []json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("not available") // and not used
}

// END unimplemented methods

// Prefer the SSL port if set, but allow TCP if that's all it has.
func bestAddr(host string, gsr *electrum.GetServersResult) (string, *tls.Config) {
	if gsr.SSL != 0 {
		rootCAs, _ := x509.SystemCertPool()
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
			RootCAs:            rootCAs,
			// MinVersion:         tls.VersionTLS12,
			ServerName: host,
		}
		port := strconv.FormatUint(uint64(gsr.SSL), 10)
		return net.JoinHostPort(host, port), tlsConfig
	} else if gsr.TCP != 0 {
		port := strconv.FormatUint(uint64(gsr.TCP), 10)
		return net.JoinHostPort(host, port), nil
	}
	return "", nil
}

// Look up the port of the active server via getservers and return a "host:port"
// formatted address. A non-nil tls.Config is returned if an SSL port. A empty
// host input will pick a random SSL host.
func (ew *electrumWallet) connInfo(host string) (addr string, tlsConfig *tls.Config, err error) {
	servers, err := ew.wallet.GetServers()
	if err != nil {
		return "", nil, err
	}
	var wsrv *electrum.GetServersResult
	if host == "" { // pick a random SSL host
		var sslServers []*electrum.GetServersResult
		for _, srv := range servers {
			if srv.SSL != 0 {
				sslServers = append(sslServers, srv)
			}
		}
		// TODO: allow non-tcp onion hosts
		if len(sslServers) == 0 {
			return "", nil, errors.New("no SSL servers")
		}
		wsrv = sslServers[rand.Intn(len(sslServers))]
	} else {
		for _, srv := range servers {
			if srv.Host == host {
				wsrv = srv
				break
			}
		}
		if wsrv == nil {
			return "", nil, fmt.Errorf("Electrum wallet server %q not found in getservers result", host)
		}
	}
	addr, tlsConfig = bestAddr(host, wsrv)
	if addr == "" {
		return "", nil, fmt.Errorf("no suitable address for host %v", host)
	}
	return addr, tlsConfig, nil
}

func (ew *electrumWallet) connect(ctx context.Context, wg *sync.WaitGroup) error {
	// Helper to get a host:port string and connection options for a host name.
	connInfo := func(host string) (addr string, srvOpts *electrum.ConnectOpts, err error) {
		addr, tlsConfig, err := ew.connInfo(host)
		if err != nil {
			return "", nil, fmt.Errorf("no suitable address for host %q: %w", host, err)
		}
		srvOpts = &electrum.ConnectOpts{
			// TorProxy: TODO
			TLSConfig:   tlsConfig, // may be nil if not ssl host
			DebugLogger: ew.log.Debugf,
		}
		return addr, srvOpts, nil
	}

	info, err := ew.wallet.GetInfo() // also initial connectivity test with the external wallet
	if err != nil {
		return err
	}
	if !info.Connected || info.Server == "" {
		return errors.New("Electrum wallet has no server connections")
	}

	// Determine if segwit expectation is met. Request and decode an address,
	// then compare with the segwit config field.
	addr, err := ew.wallet.GetUnusedAddress()
	if err != nil {
		return err
	}
	address, err := ew.decodeAddr(addr, ew.chainParams)
	if err != nil {
		return err
	}
	_, segwit := address.(interface {
		WitnessVersion() byte
	})
	if segwit != ew.segwit {
		return fmt.Errorf("segwit expectation not met: wanted segwit = %v (old wallet seed?)", ew.segwit)
	}

	addr, srvOpts, err := connInfo(info.Server)
	if err != nil {
		return fmt.Errorf("no suitable address for host %v: %w", info.Server, err)
	}
	chain, err := electrum.ConnectServer(ctx, addr, srvOpts)
	if err != nil {
		return err // maybe just try a different one if it doesn't allow multiple conns
	}
	ew.log.Infof("Now connected to electrum server %v.", addr)
	ew.chain = &electrumNetworkManager{electrumNetworkClient: chain}

	// Start a goroutine to keep the chain client alive and on the same
	// ElectrumX server as the external Electrum wallet if possible.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer ew.chain.Shutdown()
		lastWalletServer := info.Server

		failing := make(map[string]int)
		const maxFails = 8

		ticker := time.NewTicker(6 * time.Second) // to keep wallet and chain client on same server
		defer ticker.Stop()

		for {
			var walletCheck bool
			select {
			case <-ew.chain.Done():
				ew.log.Warnf("Electrum server connection lost. Reconnecting in 5 seconds...")
				select {
				case <-time.After(5 * time.Second):
				case <-ctx.Done():
					return
				}

			case <-ticker.C: // just checking with wallet for changes
				walletCheck = true

			case <-ctx.Done():
				return
			}

			info, err := ew.wallet.GetInfo()
			if err != nil {
				ew.log.Errorf("Electrum wallet getinfo failed: %v", err)
				continue
			}
			if walletCheck { // just checking if wallet's server changed
				if lastWalletServer == info.Server {
					continue // no change
				}
				delete(failing, info.Server) // clean slate now that wallet has just gotten on it
				ew.log.Infof("Electrum wallet changed server to %v", info.Server)
			}
			lastWalletServer = info.Server

			tryAddr := info.Server
			if fails := failing[tryAddr]; fails > maxFails {
				ew.log.Warnf("Server %q has failed to connect %d times. Trying a random one...", tryAddr, fails)
				tryAddr = "" // try a random one instead
			}

			addr, srvOpts, err := connInfo(tryAddr)
			if err != nil {
				failing[tryAddr]++
				ew.log.Errorf("No suitable address for host %q: %v", tryAddr, err)
				continue
			}

			if walletCheck {
				ew.chain.Shutdown()
			}
			ew.log.Infof("Connecting to new server %v...", addr)
			chain, err := electrum.ConnectServer(ctx, addr, srvOpts)
			if err != nil {
				ew.log.Errorf("Failed to connect to %v: %v", addr, err)
				failing[tryAddr]++
				continue
			}
			ew.log.Infof("Chain service now connected to electrum server %v", addr)
			ew.chain.reset(chain)

			if ctx.Err() != nil { // in case shutdown while waiting on ConnectServer
				return
			}
		}
	}()

	return err
}

func (ew *electrumWallet) estimateSmartFee(confTarget int64, _ *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	satPerKB, err := ew.wallet.FeeRate(confTarget)
	if err != nil {
		return nil, err
	}
	feeRate := float64(satPerKB) / 1e8 // BTC/KvB
	return &btcjson.EstimateSmartFeeResult{
		FeeRate: &feeRate,
	}, nil
}

func (ew *electrumWallet) sendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	b, err := ew.serializeTx(tx)
	if err != nil {
		return nil, err
	}
	txid, err := ew.wallet.Broadcast(b)
	if err != nil {
		return nil, err
	}
	hash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, err // well that sucks, it's already sent
	}
	return hash, nil
}

func (ew *electrumWallet) outputIsSpent(txid string, vout uint32, pkScript []byte) (bool, error) {
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScript, ew.chainParams)
	if err != nil {
		return false, fmt.Errorf("failed to decode pkScript: %w", err)
	}
	if len(addrs) != 1 {
		return false, fmt.Errorf("pkScript encodes %d addresses, not 1", len(addrs))
	}
	addr, err := ew.stringAddr(addrs[0], ew.chainParams)
	if err != nil {
		return false, fmt.Errorf("invalid address encoding: %w", err)
	}
	// Now see the unspent outputs for this address include this outpoint.
	addrUnspents, err := ew.wallet.GetAddressUnspent(addr)
	if err != nil {
		return false, fmt.Errorf("getaddressunspent: %w", err)
	}
	for _, utxo := range addrUnspents {
		if utxo.TxHash == txid && uint32(utxo.TxPos) == vout {
			return false, nil // still unspent
		}
	}
	return true, nil // not in the unspent list
}

func (ew *electrumWallet) getTxOut(txHash *chainhash.Hash, vout uint32, _ []byte, _ time.Time) (*wire.TxOut, uint32, error) {
	return ew.getTxOutput(txHash, vout)
}

func (ew *electrumWallet) getTxOutput(txHash *chainhash.Hash, vout uint32) (*wire.TxOut, uint32, error) {
	// In case this is a wallet transaction, try the wallet DB methods first,
	// then fall back to the more expensive server request.
	txid := txHash.String()
	txRaw, confs, err := ew.checkWalletTx(txid)
	if err != nil {
		txRes, err := ew.chain.GetTransaction(context.Background(), txid)
		if err != nil {
			return nil, 0, err
		}
		confs = uint32(txRes.Confirmations)
		txRaw, err = hex.DecodeString(txRes.Hex)
		if err != nil {
			return nil, 0, err
		}
	}

	msgTx, err := ew.deserializeTx(txRaw)
	if err != nil {
		return nil, 0, err
	}
	if vout >= uint32(len(msgTx.TxOut)) {
		return nil, 0, fmt.Errorf("output %d of tx %v does not exists", vout, txid)
	}
	pkScript := msgTx.TxOut[vout].PkScript
	amt := msgTx.TxOut[vout].Value

	// Given the pkScript, we can query for unspent outputs to see if this one
	// is unspent.
	spent, err := ew.outputIsSpent(txid, vout, pkScript)
	if err != nil {
		return nil, 0, err
	}
	if spent {
		return nil, 0, nil
	}

	return wire.NewTxOut(amt, pkScript), confs, nil
}

func (ew *electrumWallet) getBlockHeaderByHeight(height int64) (*wire.BlockHeader, error) {
	hdrStr, err := ew.chain.BlockHeader(context.Background(), uint32(height))
	if err != nil {
		return nil, err
	}
	hdr := &wire.BlockHeader{}
	err = hdr.Deserialize(hex.NewDecoder(strings.NewReader(hdrStr)))
	if err != nil {
		return nil, err
	}
	return hdr, nil
}

func (ew *electrumWallet) medianTime() (time.Time, error) {
	chainHeight, err := ew.getBestBlockHeight()
	if err != nil {
		return time.Time{}, err
	}
	return ew.calcMedianTime(int64(chainHeight))
}

func (ew *electrumWallet) calcMedianTime(height int64) (time.Time, error) {
	startHeight := height - medianTimeBlocks + 1
	if startHeight < 0 {
		startHeight = 0
	}

	// TODO: check a block hash => median time cache

	hdrsRes, err := ew.chain.BlockHeaders(context.Background(), uint32(startHeight),
		uint32(height-startHeight+1))
	if err != nil {
		return time.Time{}, err
	}

	if hdrsRes.Count != medianTimeBlocks {
		ew.log.Warnf("Failed to retrieve headers for %d blocks since block %v, got %d",
			medianTimeBlocks, height, hdrsRes.Count)
	}
	if hdrsRes.Count == 0 {
		return time.Time{}, errors.New("no headers retrieved")
	}

	hdrReader := hex.NewDecoder(strings.NewReader(hdrsRes.HexConcat))

	timestamps := make([]int64, 0, hdrsRes.Count)
	for i := int64(0); i < int64(hdrsRes.Count); i++ {
		hdr := &wire.BlockHeader{}
		err = hdr.Deserialize(hdrReader)
		if err != nil {
			if i > 0 {
				ew.log.Errorf("Failed to deserialize header for block %d: %v",
					startHeight+i, err)
				break // we have at least one time stamp, work with it
			}
			return time.Time{}, err
		}
		timestamps = append(timestamps, hdr.Timestamp.Unix())
	}
	// Naive way fetching each header separately, if we needed to use
	// btc.calcMedianTime as a chainStamper:
	// for i := height; i > height-medianTimeBlocks && i > 0; i-- {
	// 	hdr, err := ew.getBlockHeaderByHeight(height)
	// 	if err != nil {
	// 		return time.Time{}, err
	// 	}
	// 	timestamps = append(timestamps, hdr.Timestamp.Unix())
	// }

	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})

	medianTimestamp := timestamps[len(timestamps)/2]
	return time.Unix(medianTimestamp, 0), nil
}

func (ew *electrumWallet) getBlockHash(height int64) (*chainhash.Hash, error) {
	hdr, err := ew.getBlockHeaderByHeight(height)
	if err != nil {
		return nil, err
	}
	hash := hdr.BlockHash()
	return &hash, nil
}

func (ew *electrumWallet) getBestBlockHash() (*chainhash.Hash, error) {
	inf, err := ew.wallet.GetInfo()
	if err != nil {
		return nil, err
	}
	return ew.getBlockHash(inf.SyncHeight)
}

func (ew *electrumWallet) getBestBlockHeight() (int32, error) {
	inf, err := ew.wallet.GetInfo()
	if err != nil {
		return 0, err
	}
	return int32(inf.SyncHeight), nil
}

func (ew *electrumWallet) getBestBlockHeader() (*blockHeader, error) {
	inf, err := ew.wallet.GetInfo()
	if err != nil {
		return nil, err
	}

	hdr, err := ew.getBlockHeaderByHeight(inf.SyncHeight)
	if err != nil {
		return nil, err
	}

	header := &blockHeader{
		Hash:              hdr.BlockHash().String(),
		Height:            inf.SyncHeight,
		Confirmations:     1, // it's the head
		Time:              hdr.Timestamp.Unix(),
		PreviousBlockHash: hdr.PrevBlock.String(),
	}
	return header, nil
}

func (ew *electrumWallet) balances() (*GetBalancesResult, error) {
	eBal, err := ew.wallet.GetBalance()
	if err != nil {
		return nil, err
	}
	// NOTE: Nothing from the Electrum wallet's response indicates trusted vs.
	// untrusted. To allow unconfirmed coins to be spent, we treat both
	// confirmed and unconfirmed as trusted. This is like dogecoind's handling
	// of balance. TODO: listunspent -> checkWalletTx(txid) -> for each
	// input, checkWalletTx(prevout) and ismine(addr)
	return &GetBalancesResult{
		Mine: Balances{
			Trusted:  eBal.Confirmed + eBal.Unconfirmed,
			Immature: eBal.Immature,
		},
	}, nil
}

func (ew *electrumWallet) listUnspent() ([]*ListUnspentResult, error) {
	eUnspent, err := ew.wallet.ListUnspent()
	if err != nil {
		return nil, err
	}
	chainHeight, err := ew.getBestBlockHeight()
	if err != nil {
		return nil, err
	}

	// Filter out locked outpoints since listUnspent includes them.
	lockedOPs := ew.listLockedOutpoints()
	lockedOPMap := make(map[RPCOutpoint]bool, len(lockedOPs))
	for _, pt := range lockedOPs {
		lockedOPMap[*pt] = true
	}

	unspents := make([]*ListUnspentResult, 0, len(eUnspent))
	for _, utxo := range eUnspent {
		if lockedOPMap[RPCOutpoint{utxo.PrevOutHash, utxo.PrevOutIdx}] {
			continue
		}
		addr, err := ew.decodeAddr(utxo.Address, ew.chainParams)
		if err != nil {
			ew.log.Warnf("Output (%v:%d) with bad address %v found: %v",
				utxo.PrevOutHash, utxo.PrevOutIdx, utxo.Address, err)
			continue
		}
		pkScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			ew.log.Warnf("Output (%v:%d) with bad address %v found: %v",
				utxo.PrevOutHash, utxo.PrevOutIdx, utxo.Address, err)
			continue
		}
		val, err := strconv.ParseFloat(utxo.Value, 64)
		if err != nil {
			ew.log.Warnf("Output (%v:%d) with bad value %v found: %v",
				utxo.PrevOutHash, utxo.PrevOutIdx, val, err)
			continue
		}
		var confs uint32
		if height := int32(utxo.Height); height > 0 {
			// height is non-zero, so confirmed, but if the RPCs are
			// inconsistent with respect to height, avoid an underflow or
			// appearing unconfirmed.
			if height > chainHeight {
				confs = 1
			} else {
				confs = uint32(chainHeight - height + 1)
			}
		}
		redeemScript, err := hex.DecodeString(utxo.RedeemScript)
		if err != nil {
			ew.log.Warnf("Output (%v:%d) with bad redeemscript %v found: %v",
				utxo.PrevOutHash, utxo.PrevOutIdx, utxo.RedeemScript, err)
			continue
		}

		unspents = append(unspents, &ListUnspentResult{
			TxID:          utxo.PrevOutHash,
			Vout:          utxo.PrevOutIdx,
			Address:       utxo.Address,
			ScriptPubKey:  pkScript,
			Amount:        val,
			Confirmations: confs,
			RedeemScript:  redeemScript,
			Spendable:     true, // can electrum have unspendable?
			Solvable:      true,
			// Safe is unknown, leave ptr nil
		})
	}
	return unspents, nil
}

func (ew *electrumWallet) lockUnspent(unlock bool, ops []*output) error {
	eUnspent, err := ew.wallet.ListUnspent()
	if err != nil {
		return err
	}
	opMap := make(map[outPoint]struct{}, len(ops))
	for _, op := range ops {
		opMap[op.pt] = struct{}{}
	}
unspents:
	for _, utxo := range eUnspent {
		for op := range opMap {
			if op.vout == utxo.PrevOutIdx && op.txHash.String() == utxo.PrevOutHash {
				// FreezeAddress and UnfreezeAddress do not error when called
				// repeatedly for the same address.
				if unlock {
					if err = ew.wallet.UnfreezeUTXO(utxo.PrevOutHash, utxo.PrevOutIdx); err != nil {
						return err
					}
					ew.lockedOutpointsMtx.Lock()
					delete(ew.lockedOutpoints, op)
					ew.lockedOutpointsMtx.Unlock()
					delete(opMap, op)
					continue unspents
				}

				if err = ew.wallet.FreezeUTXO(utxo.PrevOutHash, utxo.PrevOutIdx); err != nil {
					return err
				}
				// listunspent returns locked utxos, so we have to track it.
				ew.lockedOutpointsMtx.Lock()
				ew.lockedOutpoints[op] = struct{}{}
				ew.lockedOutpointsMtx.Unlock()
				delete(opMap, op)
				continue unspents
			}
		}
	}
	if len(opMap) > 0 && !unlock {
		return fmt.Errorf("failed to lock some utxos")
		// unlock the ones we locked?
	} // ok if unlocking spent ones
	return nil
}

func (ew *electrumWallet) listLockedOutpoints() []*RPCOutpoint {
	ew.lockedOutpointsMtx.RLock()
	defer ew.lockedOutpointsMtx.RUnlock()
	locked := make([]*RPCOutpoint, 0, len(ew.lockedOutpoints))
	for op := range ew.lockedOutpoints {
		locked = append(locked, &RPCOutpoint{
			TxID: op.txHash.String(),
			Vout: op.vout,
		})
	}
	return locked
}

// This method signature is required to satisfy the btc.Wallet interface.
func (ew *electrumWallet) listLockUnspent() ([]*RPCOutpoint, error) {
	return ew.listLockedOutpoints(), nil
}

func (ew *electrumWallet) externalAddress() (btcutil.Address, error) {
	addr, err := ew.wallet.CreateNewAddress()
	if err != nil {
		return nil, err
	}
	return ew.decodeAddr(addr, ew.chainParams)
}

func (ew *electrumWallet) changeAddress() (btcutil.Address, error) {
	return ew.externalAddress() // sadly, cannot request internal addresses
}

func (ew *electrumWallet) refundAddress() (btcutil.Address, error) {
	addr, err := ew.wallet.GetUnusedAddress()
	if err != nil {
		return nil, err
	}
	return ew.decodeAddr(addr, ew.chainParams)
}

func (ew *electrumWallet) signTx(inTx *wire.MsgTx) (*wire.MsgTx, error) {
	// If the wallet's signtransaction RPC ever has a problem with the PSBT, we
	// could attempt to sign the transaction ourselves by pulling the inputs'
	// private keys and using txscript manually, but this can vary greatly
	// between assets.

	packet, err := psbt.NewFromUnsignedTx(inTx)
	if err != nil {
		return nil, err
	}
	psbtB64, err := packet.B64Encode()
	if err != nil {
		return nil, err
	}

	signedB, err := ew.wallet.SignTx(ew.walletPass(), psbtB64)
	if err != nil {
		return nil, err
	}
	return ew.deserializeTx(signedB)
}

type hash160er interface {
	Hash160() *[20]byte
}

type pubKeyer interface {
	PubKey() *btcec.PublicKey
}

func (ew *electrumWallet) privKeyForAddress(addr string) (*btcec.PrivateKey, error) {
	addrDec, err := ew.decodeAddr(addr, ew.chainParams)
	if err != nil {
		return nil, err
	}
	wifStr, err := ew.wallet.GetPrivateKeys(ew.walletPass(), addr)
	if err != nil {
		return nil, err
	}
	wif, err := btcutil.DecodeWIF(wifStr)
	if err != nil {
		return nil, err
	} // wif.PrivKey is the result

	// Sanity check that PrivKey corresponds to the pubkey(hash).
	var pkh []byte
	switch addrT := addrDec.(type) { // maybe switch on interfaces like Hash160() and PubKey() instead of concrete types
	case pubKeyer: // *btcutil.AddressPubKey:
		// Get same format as wif.SerializePubKey()
		var pk []byte
		if wif.CompressPubKey {
			pk = addrT.PubKey().SerializeCompressed()
		} else {
			pk = addrT.PubKey().SerializeUncompressed()
		}
		pkh = btcutil.Hash160(pk) // addrT.ScriptAddress() would require SetFormat(compress/uncompress)
	case *btcutil.AddressScriptHash, *btcutil.AddressWitnessScriptHash:
		return wif.PrivKey, nil // assume unknown redeem script references this pubkey
	case hash160er: // p2pkh and p2wpkh
		pkh = addrT.Hash160()[:]
	}
	wifPKH := btcutil.Hash160(wif.SerializePubKey())
	if !bytes.Equal(pkh, wifPKH) {
		return nil, errors.New("pubkey mismatch")
	}
	return wif.PrivKey, nil
}

func (ew *electrumWallet) pass() (pw string, unlocked bool) {
	ew.pwMtx.RLock()
	defer ew.pwMtx.RUnlock()
	return ew.pw, ew.unlocked
}

// walletLock locks the wallet. Part of the btc.Wallet interface.
func (ew *electrumWallet) walletLock() error {
	ew.pwMtx.Lock()
	defer ew.pwMtx.Unlock()
	ew.pw, ew.unlocked = "", false
	return nil
}

// locked indicates if the wallet has been unlocked. Part of the btc.Wallet
// interface.
func (ew *electrumWallet) locked() bool {
	ew.pwMtx.RLock()
	defer ew.pwMtx.RUnlock()
	return !ew.unlocked
}

// walletPass returns the wallet passphrase. Since an empty password is valid,
// use pass or locked to determine if locked. This is for convenience.
func (ew *electrumWallet) walletPass() string {
	pw, _ := ew.pass()
	return pw
}

// walletUnlock attempts to unlock the wallet with the provided password. On
// success, the password is stored and may be accessed via pass or walletPass.
// Part of the btc.Wallet interface.
func (ew *electrumWallet) walletUnlock(pw []byte) error {
	addr, err := ew.wallet.GetUnusedAddress()
	if err != nil {
		return err
	}
	pass := string(pw)
	wifStr, err := ew.wallet.GetPrivateKeys(pass, addr)
	if err != nil {
		return err
	} // that should be enough, but validate the returned keys in case they are empty or something
	if _, err = btcutil.DecodeWIF(wifStr); err != nil {
		return err
	}
	ew.pwMtx.Lock()
	ew.pw, ew.unlocked = pass, true
	ew.pwMtx.Unlock()
	return nil
}

func (ew *electrumWallet) peerCount() (uint32, error) {
	if ew.chain == nil {
		return 0, nil
	}

	info, err := ew.wallet.GetInfo()
	if err != nil {
		return 0, err
	}
	select {
	case <-ew.chain.Done():
		return 0, errors.New("electrumx server connection down")
	default:
	}

	return uint32(info.Connections), nil
}

func (ew *electrumWallet) ownsAddress(addr btcutil.Address) (bool, error) {
	addrStr, err := ew.stringAddr(addr, ew.chainParams)
	if err != nil {
		return false, err
	}
	valid, mine, err := ew.wallet.CheckAddress(addrStr)
	if err != nil {
		return false, err
	}
	if !valid { // maybe electrum doesn't know all encodings that btcutil does
		return false, nil // an error here may prevent reconfiguring a misconfigured wallet
	}
	return mine, nil
}

func (ew *electrumWallet) syncStatus() (*syncStatus, error) {
	info, err := ew.wallet.GetInfo()
	if err != nil {
		return nil, err
	}
	return &syncStatus{
		Target:  int32(info.ServerHeight),
		Height:  int32(info.SyncHeight),
		Syncing: !info.Connected || info.SyncHeight < info.ServerHeight,
	}, nil
}

func (ew *electrumWallet) checkWalletTx(txid string) ([]byte, uint32, error) {
	// GetWalletTxConfs only works for wallet transactions, while
	// wallet.GetRawTransaction will try the wallet DB first, but fall back to
	// querying a server, so do GetWalletTxConfs first to prevent that.
	confs, err := ew.wallet.GetWalletTxConfs(txid)
	if err != nil {
		return nil, 0, err
	}
	txRaw, err := ew.wallet.GetRawTransaction(txid)
	if err != nil {
		return nil, 0, err
	}
	if confs < 0 {
		confs = 0
	}
	return txRaw, uint32(confs), nil
}

func (ew *electrumWallet) getWalletTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	// Try the wallet first. If it is not a wallet transaction or if it is
	// confirmed, fall back to the chain method to get the block info and time
	// fields.
	txid := txHash.String()
	txRaw, confs, err := ew.checkWalletTx(txid)
	if err == nil && confs == 0 {
		return &GetTransactionResult{
			TxID: txid,
			Hex:  txRaw,
			// Time/TimeReceived? now? needed?
		}, nil
	} // else we have to ask a server for the verbose response with block info

	txInfo, err := ew.chain.GetTransaction(context.Background(), txid)
	if err != nil {
		return nil, err
	}
	txRaw, err = hex.DecodeString(txInfo.Hex)
	if err != nil {
		return nil, err
	}
	return &GetTransactionResult{
		Confirmations: uint64(txInfo.Confirmations),
		BlockHash:     txInfo.BlockHash,
		// BlockIndex unknown
		BlockTime:    uint64(txInfo.BlockTime),
		TxID:         txInfo.TxID, // txHash.String()
		Time:         uint64(txInfo.Time),
		TimeReceived: uint64(txInfo.Time),
		Hex:          txRaw,
	}, nil
}

func (ew *electrumWallet) swapConfirmations(txHash *chainhash.Hash, vout uint32, contract []byte, startTime time.Time) (confs uint32, spent bool, err error) {
	// To determine if it is spent, we need the address of the output.
	var pkScript []byte
	txid := txHash.String()
	// Try the wallet first in case this is a wallet transaction (own swap).
	txRaw, confs, err := ew.checkWalletTx(txid)
	if err == nil {
		msgTx, err := ew.deserializeTx(txRaw)
		if err != nil {
			return 0, false, err
		}
		if vout >= uint32(len(msgTx.TxOut)) {
			return 0, false, fmt.Errorf("output %d of tx %v does not exists", vout, txid)
		}
		pkScript = msgTx.TxOut[vout].PkScript
	} else {
		// Fall back to the more expensive server request.
		txInfo, err := ew.chain.GetTransaction(context.Background(), txid)
		if err != nil {
			return 0, false, err
		}
		confs = uint32(txInfo.Confirmations)
		if txInfo.Confirmations < 1 {
			confs = 0
		}
		if vout >= uint32(len(txInfo.Vout)) {
			return 0, false, fmt.Errorf("output %d of tx %v does not exists", vout, txid)
		}
		txOut := &txInfo.Vout[vout]
		pkScript, err = hex.DecodeString(txOut.PkScript.Hex)
		if err != nil {
			return 0, false, fmt.Errorf("invalid pkScript: %w", err)
		}
	}

	// Decode the pkScript and extract the address.
	scriptClass, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScript, ew.chainParams)
	if err != nil {
		return 0, false, fmt.Errorf("invalid pkScript: %v", err)
	}
	switch scriptClass { // require p2sh or p2wsh... necessary?
	case txscript.ScriptHashTy, txscript.WitnessV0ScriptHashTy:
	default:
		return 0, false, fmt.Errorf("pkScript not a script hash: %v", scriptClass)
	}
	if len(addrs) != 1 {
		return 0, false, fmt.Errorf("invalid pkScript: %d addresses", len(addrs))
	}
	swapAddr, err := ew.stringAddr(addrs[0], ew.chainParams)
	if len(addrs) != 1 {
		return 0, false, fmt.Errorf("invalid swap address encoding: %w", err)
	}

	// Now see if the unspent outputs for this address include this outpoint.
	addrUnspents, err := ew.wallet.GetAddressUnspent(swapAddr)
	if err != nil {
		return 0, false, fmt.Errorf("getaddressunspent: %v", err)
	}
	spent = true
	for _, utxo := range addrUnspents {
		if utxo.TxHash == txid && uint32(utxo.TxPos) == vout {
			spent = false
			break
		}
	}
	return confs, spent, nil
}

func (ew *electrumWallet) sendWithSubtract(address string, value, feeRate uint64) (*chainhash.Hash, error) {
	pw, unlocked := ew.pass() // check first to spare some RPCs if locked
	if !unlocked {
		return nil, errors.New("wallet locked")
	}
	addr, err := ew.decodeAddr(address, ew.chainParams)
	if err != nil {
		return nil, err
	}
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, err
	}

	unfundedTxSize := dexbtc.MinimumTxOverhead + dexbtc.P2WPKHOutputSize /* change */ +
		dexbtc.TxOutOverhead + uint64(len(pkScript)) // send-to address

	unspents, err := ew.listUnspent()
	if err != nil {
		return nil, fmt.Errorf("error listing unspent outputs: %w", err)
	}
	utxos, _, _, err := convertUnspent(0, unspents, ew.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error converting unspent outputs: %w", err)
	}

	// With sendWithSubtract, fees are subtracted from the sent amount, so we
	// target an input sum, not an output value. Makes the math easy.
	enough := func(_, inputsVal uint64) bool {
		return inputsVal >= value
	}
	sum, inputsSize, _, fundingCoins, _, _, err := fund(utxos, enough)
	if err != nil {
		return nil, fmt.Errorf("error funding sendWithSubtract value of %s: %w", amount(value), err)
	}

	fees := (unfundedTxSize + uint64(inputsSize)) * feeRate
	send := value - fees
	// extra := sum - send

	switch {
	case fees > sum:
		return nil, fmt.Errorf("fees > sum")
	case fees > value:
		return nil, fmt.Errorf("fees > value")
	case send > sum:
		return nil, fmt.Errorf("send > sum")
	}

	//tx := wire.NewMsgTx(wire.TxVersion)
	fromCoins := make([]string, 0, len(fundingCoins))
	for op := range fundingCoins {
		// wireOP := wire.NewOutPoint(&op.txHash, op.vout)
		// txIn := wire.NewTxIn(wireOP, []byte{}, nil)
		// tx.AddTxIn(txIn)
		fromCoins = append(fromCoins, op.String())
	}

	// To get Electrum to pick a change address, we use payTo with the
	// from_coins option and an absolute fee.
	txRaw, err := ew.wallet.PayToFromCoinsAbsFee(pw, fromCoins, address, toBTC(send), toBTC(fees))
	if err != nil {
		return nil, err
	}
	// Do some sanity checks on the generated txn: (a) only spend chosen funding
	// coins, (b) must pay to specified address in desired amount.
	msgTx, err := ew.deserializeTx(txRaw)
	if err != nil {
		return nil, err
	}
	for _, txIn := range msgTx.TxIn {
		op := outPoint{txIn.PreviousOutPoint.Hash, txIn.PreviousOutPoint.Index}
		if _, found := fundingCoins[op]; !found {
			return nil, fmt.Errorf("prevout %v was not specified but was spent", op)
		}
	}
	var foundOut bool
	for i, txOut := range msgTx.TxOut {
		if bytes.Equal(txOut.PkScript, pkScript) {
			if txOut.Value != int64(send) {
				return nil, fmt.Errorf("output %v paid %v not %v", i, txOut.Value, send)
			}
			foundOut = true
			break
		}
	}
	if !foundOut {
		return nil, fmt.Errorf("no output paying to %v was found", address)
	}

	txid, err := ew.wallet.Broadcast(txRaw)
	if err != nil {
		ew.wallet.RemoveLocalTx(msgTx.TxHash().String())
		return nil, err
	}
	return chainhash.NewHashFromStr(txid) // hope this doesn't error because it's already sent

	/* Cannot request change addresses from Electrum!

	change := extra - fees
	changeAddr, err := ew.changeAddress()
	if err != nil {
		return nil, fmt.Errorf("error retrieving change address: %w", err)
	}

	changeScript, err := txscript.PayToAddrScript(changeAddr)
	if err != nil {
		return nil, fmt.Errorf("error generating pubkey script: %w", err)
	}

	changeOut := wire.NewTxOut(int64(change), changeScript)

	// One last check for dust.
	if dexbtc.IsDust(changeOut, feeRate) { // TODO: use a customizable isDust function e.g. (*baseWallet).IsDust
		// Re-calculate fees and change
		fees = (unfundedTxSize - dexbtc.P2WPKHOutputSize + uint64(inputsSize)) * feeRate
		send = sum - fees
	} else {
		tx.AddTxOut(changeOut)
	}

	wireOP := wire.NewTxOut(int64(send), pkScript)
	tx.AddTxOut(wireOP)

	tx, err = ew.signTx(tx)
	if err != nil {
		return nil, fmt.Errorf("signing error: %w", err)
	}

	return ew.sendRawTransaction(tx)
	*/
}

func (ew *electrumWallet) sendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error) {
	if subtract {
		return ew.sendWithSubtract(address, value, feeRate)
	}

	txRaw, err := ew.wallet.PayTo(ew.walletPass(), address, toBTC(value), float64(feeRate))
	if err != nil {
		return nil, err
	}
	msgTx, err := ew.deserializeTx(txRaw)
	if err != nil {
		return nil, err
	}
	txid, err := ew.wallet.Broadcast(txRaw)
	if err != nil {
		ew.wallet.RemoveLocalTx(msgTx.TxHash().String())
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

func (ew *electrumWallet) sweep(address string, feeRate uint64) ([]byte, error) {
	txRaw, err := ew.wallet.Sweep(ew.walletPass(), address, float64(feeRate))
	if err != nil {
		return nil, err
	}
	msgTx, err := ew.deserializeTx(txRaw)
	if err != nil {
		return nil, err
	}
	_, err = ew.wallet.Broadcast(txRaw)
	if err != nil {
		ew.wallet.RemoveLocalTx(msgTx.TxHash().String())
		return nil, err
	}

	return txRaw, nil
}

func (ew *electrumWallet) outPointAddress(txHash *chainhash.Hash, vout uint32) (string, error) {
	txid := txHash.String()
	txRaw, err := ew.wallet.GetRawTransaction(txid)
	if err != nil {
		return "", err
	}
	msgTx, err := ew.deserializeTx(txRaw)
	if err != nil {
		return "", err
	}
	if vout >= uint32(len(msgTx.TxOut)) {
		return "", fmt.Errorf("output %d of tx %v does not exists", vout, txid)
	}
	pkScript := msgTx.TxOut[vout].PkScript
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScript, ew.chainParams)
	if err != nil {
		return "", fmt.Errorf("invalid pkScript: %v", err)
	}
	if len(addrs) != 1 {
		return "", fmt.Errorf("invalid pkScript: %d addresses", len(addrs))
	}
	addrStr, err := ew.stringAddr(addrs[0], ew.chainParams)
	if err != nil {
		return "", err
	}
	return addrStr, nil
}

func (ew *electrumWallet) findOutputSpender(txHash *chainhash.Hash, vout uint32) (*wire.MsgTx, uint32, error) {
	addr, err := ew.outPointAddress(txHash, vout)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid outpoint address: %w", err)
	}
	// NOTE: we could start with GetAddressUnspent to detect unspent before
	// requesting the entire address history.
	hist, err := ew.wallet.GetAddressHistory(addr)
	if err != nil {
		return nil, 0, fmt.Errorf("unable to get address history: %w", err)
	}
	txid := txHash.String()

	sort.Slice(hist, func(i, j int) bool {
		return hist[i].Height > hist[j].Height // descending
	})

	var outHeight int64
	for _, io := range hist {
		if io.TxHash == txid {
			outHeight = io.Height
			continue // same txn
		}
		if io.Height < outHeight {
			break // spender not before the output's txn
		}
		txRaw, err := ew.wallet.GetRawTransaction(io.TxHash)
		if err != nil {
			ew.log.Warnf("Unable to retrieve transaction %v for address %v: %v",
				io.TxHash, addr, err)
			continue
		}
		msgTx, err := ew.deserializeTx(txRaw)
		if err != nil {
			ew.log.Warnf("Unable to decode transaction %v for address %v: %v",
				io.TxHash, addr, err)
			continue
		}
		for vin, txIn := range msgTx.TxIn {
			prevOut := &txIn.PreviousOutPoint
			if vout == prevOut.Index && prevOut.Hash.IsEqual(txHash) {
				return msgTx, uint32(vin), nil
			}
		}
	}

	return nil, 0, nil // caller should check msgTx (internal method)
}
