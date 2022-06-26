// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc/electrum"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
)

// ExchangeWalletElectrum is the asset.Wallet for an external Electrum wallet.
type ExchangeWalletElectrum struct {
	*baseWallet
	ew *electrumWallet
}

var _ asset.Wallet = (*ExchangeWalletElectrum)(nil)
var _ asset.FeeRater = (*ExchangeWalletElectrum)(nil)
var _ asset.Sweeper = (*ExchangeWalletElectrum)(nil)

// ElectrumWallet creates a new ExchangeWalletElectrum for the provided
// configuration, which must contain the necessary details for accessing the
// Electrum wallet's RPC server in the WalletCFG.Settings map.
func ElectrumWallet(cfg *BTCCloneCFG) (*ExchangeWalletElectrum, error) {
	clientCfg, err := readRPCWalletConfig(cfg.WalletCFG.Settings, cfg.Symbol, cfg.Network, cfg.Ports)
	if err != nil {
		return nil, err
	}

	btc, err := newUnconnectedWallet(cfg, &clientCfg.WalletConfig)
	if err != nil {
		return nil, err
	}

	rpcCfg := &clientCfg.RPCConfig
	ewc := electrum.NewWalletClient(rpcCfg.RPCUser, rpcCfg.RPCPass, "http://"+rpcCfg.RPCBind)
	ew := newElectrumWallet(ewc, &electrumWalletConfig{
		params:         cfg.ChainParams,
		log:            cfg.Logger.SubLogger("ELECTRUM"),
		addrDecoder:    cfg.AddressDecoder,
		addrStringer:   cfg.AddressStringer,
		txDeserializer: cfg.TxDeserializer,
		txSerializer:   cfg.TxSerializer,
		segwit:         cfg.Segwit,
	})
	btc.node = ew

	eew := &ExchangeWalletElectrum{
		baseWallet: btc,
		ew:         ew,
	}
	btc.estimateFee = eew.feeRate // use ExchangeWalletElectrum override, not baseWallet's

	return eew, nil
}

// DepositAddress returns an address for depositing funds into the exchange
// wallet. The address will be unused but not necessarily new. Use NewAddress to
// request a new address, but it should be used immediately.
func (btc *ExchangeWalletElectrum) DepositAddress() (string, error) {
	return btc.ew.wallet.GetUnusedAddress()
}

// RedemptionAddress gets an address for use in redeeming the counterparty's
// swap. This would be included in their swap initialization. The address will
// be unused but not necessarily new because these addresses often go unused.
func (btc *ExchangeWalletElectrum) RedemptionAddress() (string, error) {
	return btc.ew.wallet.GetUnusedAddress()
}

// Connect connects to the Electrum wallet's RPC server and an electrum server
// directly. Goroutines are started to monitor for new blocks and server
// connection changes. Satisfies the dex.Connector interface.
func (btc *ExchangeWalletElectrum) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	wg, err := btc.connect(ctx) // prepares btc.ew.chainV via btc.node.connect()
	if err != nil {
		return nil, err
	}

	commands, err := btc.ew.wallet.Commands()
	if err != nil {
		return nil, err
	}
	var hasFreezeUTXO bool
	for i := range commands {
		if commands[i] == "freeze_utxo" {
			hasFreezeUTXO = true
			break
		}
	}
	if !hasFreezeUTXO {
		return nil, errors.New("wallet does not support the freeze_utxo command")
	}

	serverFeats, err := btc.ew.chain().Features(ctx)
	if err != nil {
		return nil, err
	}
	// TODO: for chainforks with the same genesis hash (BTC -> BCH), compare a
	// block hash at some post-fork height.
	if btc.chainParams.GenesisHash.String() != serverFeats.Genesis {
		return nil, fmt.Errorf("wanted genesis hash %v, got %v (wrong network)",
			btc.chainParams.GenesisHash.String(), serverFeats.Genesis)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.watchBlocks(ctx) // ExchangeWalletElectrum override
		btc.cancelRedemptionSearches()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.monitorPeers(ctx)
	}()

	return wg, nil
}

// Sweep sends all the funds in the wallet to an address.
func (btc *ExchangeWalletElectrum) Sweep(address string, feeSuggestion uint64) (asset.Coin, error) {
	addr, err := btc.decodeAddr(address, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("address decode error: %w", err)
	}
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("PayToAddrScript error: %w", err)
	}

	txRaw, err := btc.ew.sweep(address, feeSuggestion)
	if err != nil {
		return nil, err
	}

	msgTx, err := btc.deserializeTx(txRaw)
	if err != nil {
		return nil, err
	}
	txHash := msgTx.TxHash()
	for vout, txOut := range msgTx.TxOut {
		if bytes.Equal(txOut.PkScript, pkScript) {
			return newOutput(&txHash, uint32(vout), uint64(txOut.Value)), nil
		}
	}

	// Well, the txn is sent, so let's at least direct the user to the txid even
	// though we failed to find the output with the expected pkScript. Perhaps
	// the Electrum wallet generated a slightly different pkScript for the
	// provided address.
	btc.log.Warnf("Generated tx does not seem to contain an output to %v!", address)
	return newOutput(&txHash, 0, 0 /* ! */), nil
}

// override feeRate to avoid unnecessary conversions and btcjson types.
func (btc *ExchangeWalletElectrum) feeRate(_ RawRequester, confTarget uint64) (uint64, error) {
	satPerKB, err := btc.ew.wallet.FeeRate(int64(confTarget))
	if err != nil {
		return 0, err
	}
	return uint64(dex.IntDivUp(satPerKB, 1000)), nil
}

// FeeRate gets a fee rate estimate. Satisfies asset.FeeRater.
func (btc *ExchangeWalletElectrum) FeeRate() uint64 {
	feeRate, err := btc.feeRate(nil, 1)
	if err != nil {
		btc.log.Errorf("Failed to retrieve fee rate: %v", err)
		return 0
	}
	return feeRate
}

// findRedemption will search for the spending transaction of specified
// outpoint. If found, the secret key will be extracted from the input scripts.
// If not found, but otherwise without an error, a nil Hash will be returned
// along with a nil error. Thus, both the error and the Hash should be checked.
// This convention is only used since this is not part of the public API.
func (btc *ExchangeWalletElectrum) findRedemption(op outPoint, contractHash []byte) (*chainhash.Hash, uint32, []byte, error) {
	msgTx, vin, err := btc.ew.findOutputSpender(&op.txHash, op.vout)
	if err != nil {
		return nil, 0, nil, err
	}
	if msgTx == nil {
		return nil, 0, nil, nil
	}
	txHash := msgTx.TxHash()
	txIn := msgTx.TxIn[vin]
	secret, err := dexbtc.FindKeyPush(txIn.Witness, txIn.SignatureScript,
		contractHash, btc.segwit, btc.chainParams)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to extract secret key from tx %v input %d: %w",
			txHash, vin, err) // name the located tx in the error since we found it
	}
	return &txHash, vin, secret, nil
}

func (btc *ExchangeWalletElectrum) tryRedemptionRequests() {
	btc.findRedemptionMtx.RLock()
	reqs := make([]*findRedemptionReq, 0, len(btc.findRedemptionQueue))
	for _, req := range btc.findRedemptionQueue {
		reqs = append(reqs, req)
	}
	btc.findRedemptionMtx.RUnlock()

	for _, req := range reqs {
		txHash, vin, secret, err := btc.findRedemption(req.outPt, req.contractHash)
		if err != nil {
			req.fail("findRedemption: %w", err)
			continue
		}
		if txHash == nil {
			continue // maybe next time
		}
		req.success(&findRedemptionResult{
			redemptionCoinID: toCoinID(txHash, vin),
			secret:           secret,
		})
	}
}

// FindRedemption locates a swap contract output's redemption transaction input
// and the secret key used to spend the output.
func (btc *ExchangeWalletElectrum) FindRedemption(ctx context.Context, coinID, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, nil, err
	}
	contractHash := btc.hashContract(contract)
	// We can verify the contract hash via:
	// txRes, _ := btc.ewc.getWalletTransaction(txHash)
	// msgTx, _ := msgTxFromBytes(txRes.Hex)
	// contractHash := dexbtc.ExtractScriptHash(msgTx.TxOut[vout].PkScript)
	// OR
	// txOut, _, _ := btc.ew.getTxOutput(txHash, vout)
	// contractHash := dexbtc.ExtractScriptHash(txOut.PkScript)

	// Check once before putting this in the queue.
	outPt := newOutPoint(txHash, vout)
	spendTxID, vin, secret, err := btc.findRedemption(outPt, contractHash)
	if err != nil {
		return nil, nil, err
	}
	if spendTxID != nil {
		return toCoinID(spendTxID, vin), secret, nil
	}

	req := &findRedemptionReq{
		outPt:        outPt,
		resultChan:   make(chan *findRedemptionResult, 1),
		contractHash: contractHash,
		// blockHash, blockHeight, and pkScript not used by this impl.
		blockHash: &chainhash.Hash{},
	}
	if err := btc.queueFindRedemptionRequest(req); err != nil {
		return nil, nil, err
	}

	var result *findRedemptionResult
	select {
	case result = <-req.resultChan:
		if result == nil {
			err = fmt.Errorf("unexpected nil result for redemption search for %s", outPt)
		}
	case <-ctx.Done():
		err = fmt.Errorf("context cancelled during search for redemption for %s", outPt)
	}

	// If this contract is still in the findRedemptionQueue, remove from the
	// queue to prevent further redemption search attempts for this contract.
	btc.findRedemptionMtx.Lock()
	delete(btc.findRedemptionQueue, outPt)
	btc.findRedemptionMtx.Unlock()

	// result would be nil if ctx is canceled or the result channel is closed
	// without data, which would happen if the redemption search is aborted when
	// this ExchangeWallet is shut down.
	if result != nil {
		return result.redemptionCoinID, result.secret, result.err
	}
	return nil, nil, err
}

// watchBlocks pings for new blocks and runs the tipChange callback function
// when the block changes.
func (btc *ExchangeWalletElectrum) watchBlocks(ctx context.Context) {
	const electrumBlockTick = 5 * time.Second
	ticker := time.NewTicker(electrumBlockTick)
	defer ticker.Stop()

	// TODO: Why is currentTip a field, esp. one guarded by a mutex? Its use can
	// be limited to the watchBlocks loop. Work around it being set in connect?

	for {
		select {
		case <-ticker.C:
			// Don't make server requests on every tick. Wallet has a headers
			// subscription, so we can just ask wallet the height. That means
			// only comparing heights instead of hashes, which means we might
			// not notice a reorg to a block at the same height, which is
			// unimportant because of how electrum searches for transactions.
			stat, err := btc.node.syncStatus()
			if err != nil {
				go btc.tipChange(fmt.Errorf("failed to get sync status"))
				continue
			}

			btc.tipMtx.RLock()
			sameTip := btc.currentTip.height == int64(stat.Height)
			btc.tipMtx.RUnlock()
			if sameTip {
				// Could have actually been a reorg to different block at same
				// height. We'll report a new tip block on the next block.
				continue
			}

			// btc.node.getBlockHash(int64(stat.Height))
			newTipHdr, err := btc.node.getBestBlockHeader()
			if err != nil {
				// NOTE: often says "height X out of range", then succeeds on next tick
				if !strings.Contains(err.Error(), "out of range") {
					go btc.tipChange(fmt.Errorf("failed to get best block header from %s electrum server: %w", btc.symbol, err))
				}
				continue
			}
			newTipHash, err := chainhash.NewHashFromStr(newTipHdr.Hash)
			if err != nil {
				go btc.tipChange(fmt.Errorf("invalid hash %v: %w", newTipHdr.Hash, err))
				continue
			}

			newTip := &block{newTipHdr.Height, *newTipHash}
			btc.reportNewTip(newTip)

		case <-ctx.Done():
			return
		}
	}
}

// reportNewTip sets the currentTip. The tipChange callback function is invoked
// and a goroutine is started to check if any contracts in the
// findRedemptionQueue are redeemed in the new blocks.
func (btc *ExchangeWalletElectrum) reportNewTip(newTip *block) {
	btc.tipMtx.Lock()
	prevTip := btc.currentTip
	btc.currentTip = newTip
	btc.tipMtx.Unlock()

	btc.log.Debugf("tip change: %d (%s) => %d (%s)", prevTip.height, prevTip.hash, newTip.height, newTip.hash)
	go btc.tipChange(nil)
	go btc.tryRedemptionRequests()
}
