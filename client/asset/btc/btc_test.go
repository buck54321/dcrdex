//go:build !spvlive
// +build !spvlive

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

var (
	tLogger   dex.Logger
	tCtx      context.Context
	tLotSize  uint64 = 1e6 // 0.01 BTC
	tRateStep uint64 = 10
	tBTC             = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		Version:      version,
		SwapSize:     dexbtc.InitTxSize,
		SwapSizeBase: dexbtc.InitTxSizeBase,
		MaxFeeRate:   34,
		SwapConf:     1,
	}
	optimalFeeRate uint64 = 24
	tErr                  = fmt.Errorf("test error")
	tTxID                 = "308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad9"
	tTxHash        *chainhash.Hash
	tP2PKHAddr     = "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi"
	tP2PKH         []byte
	tP2WPKH        []byte
	tP2WPKHAddr           = "bc1qq49ypf420s0kh52l9pk7ha8n8nhsugdpculjas"
	feeSuggestion  uint64 = 10
)

func btcAddr(segwit bool) btcutil.Address {
	var addr btcutil.Address
	if segwit {
		addr, _ = btcutil.DecodeAddress(tP2WPKHAddr, &chaincfg.MainNetParams)
	} else {
		addr, _ = btcutil.DecodeAddress(tP2PKHAddr, &chaincfg.MainNetParams)
	}
	return addr
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func signFuncRaw(t *testing.T, params []json.RawMessage, sizeTweak int, sigComplete, segwit bool) (json.RawMessage, error) {
	signTxRes := SignTxResult{
		Complete: sigComplete,
	}
	var msgHex string
	err := json.Unmarshal(params[0], &msgHex)
	if err != nil {
		t.Fatalf("error unmarshaling transaction hex: %v", err)
	}
	msgBytes, _ := hex.DecodeString(msgHex)
	txReader := bytes.NewReader(msgBytes)
	msgTx := wire.NewMsgTx(wire.TxVersion)
	err = msgTx.Deserialize(txReader)
	if err != nil {
		t.Fatalf("error deserializing contract: %v", err)
	}

	signFunc(msgTx, sizeTweak, segwit)

	buf := new(bytes.Buffer)
	err = msgTx.Serialize(buf)
	if err != nil {
		t.Fatalf("error serializing contract: %v", err)
	}
	signTxRes.Hex = buf.Bytes()
	return mustMarshal(signTxRes), nil
}

func signFunc(tx *wire.MsgTx, sizeTweak int, segwit bool) {
	// Set the sigScripts to random bytes of the correct length for spending a
	// p2pkh output.
	if segwit {
		sigSize := 73 + sizeTweak
		for i := range tx.TxIn {
			tx.TxIn[i].Witness = wire.TxWitness{
				randBytes(sigSize),
				randBytes(33),
			}
		}
	} else {
		scriptSize := dexbtc.RedeemP2PKHSigScriptSize + sizeTweak
		for i := range tx.TxIn {
			tx.TxIn[i].SignatureScript = randBytes(scriptSize)
		}
	}
}

type msgBlockWithHeight struct {
	msgBlock *wire.MsgBlock
	height   int64
}

type testData struct {
	badSendHash   *chainhash.Hash
	sendErr       error
	sentRawTx     *wire.MsgTx
	txOutRes      *btcjson.GetTxOutResult
	txOutErr      error
	sigIncomplete bool
	signFunc      func(*wire.MsgTx)
	signMsgFunc   func([]json.RawMessage) (json.RawMessage, error)

	blockchainMtx sync.RWMutex
	verboseBlocks map[string]*msgBlockWithHeight
	dbBlockForTx  map[chainhash.Hash]*hashEntry
	mainchain     map[int64]*chainhash.Hash

	getBestBlockHashErr error
	mempoolTxs          map[chainhash.Hash]*wire.MsgTx
	rawVerboseErr       error
	lockedCoins         []*RPCOutpoint
	estFeeErr           error
	listLockUnspent     []*RPCOutpoint
	getBalances         *GetBalancesResult
	getBalancesErr      error
	lockUnspentErr      error
	changeAddr          string
	changeAddrErr       error
	newAddress          string
	newAddressErr       error
	privKeyForAddr      *btcutil.WIF
	privKeyForAddrErr   error

	getTransaction    *GetTransactionResult
	getTransactionErr error

	getBlockchainInfo    *getBlockchainInfoResult
	getBlockchainInfoErr error
	unlockErr            error
	lockErr              error
	sendToAddress        string
	sendToAddressErr     error
	setTxFee             bool
	signTxErr            error
	listUnspent          []*ListUnspentResult
	listUnspentErr       error

	// spv
	fetchInputInfoTx  *wire.MsgTx
	getCFilterScripts map[chainhash.Hash][][]byte
	checkpoints       map[outPoint]*scanCheckpoint
	confs             uint32
	confsSpent        bool
	confsErr          error
	walletTxSpent     bool
}

func newTestData() *testData {
	// setup genesis block, required by bestblock polling goroutine
	genesisHash := chaincfg.MainNetParams.GenesisHash
	return &testData{
		txOutRes: newTxOutResult([]byte{}, 1, 0),
		verboseBlocks: map[string]*msgBlockWithHeight{
			genesisHash.String(): {msgBlock: &wire.MsgBlock{}},
		},
		dbBlockForTx: make(map[chainhash.Hash]*hashEntry),
		mainchain: map[int64]*chainhash.Hash{
			0: genesisHash,
		},
		mempoolTxs:        make(map[chainhash.Hash]*wire.MsgTx),
		fetchInputInfoTx:  dummyTx(),
		getCFilterScripts: make(map[chainhash.Hash][][]byte),
		confsErr:          WalletTransactionNotFound,
		checkpoints:       make(map[outPoint]*scanCheckpoint),
	}
}

func (c *testData) getBlock(blockHash string) *msgBlockWithHeight {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	return c.verboseBlocks[blockHash]
}

func (c *testData) GetBestBlockHeight() int64 {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	var bestBlkHeight int64
	for height := range c.mainchain {
		if height >= bestBlkHeight {
			bestBlkHeight = height
		}
	}
	return bestBlkHeight
}

func (c *testData) bestBlock() (*chainhash.Hash, int64) {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	var bestHash *chainhash.Hash
	var bestBlkHeight int64
	for height, hash := range c.mainchain {
		if height >= bestBlkHeight {
			bestBlkHeight = height
			bestHash = hash
		}
	}
	return bestHash, bestBlkHeight
}

func encodeOrError(thing interface{}, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(thing)
}

type tRawRequester struct {
	*testData
}

func (c *tRawRequester) RawRequest(_ context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	switch method {
	// TODO: handle methodGetBlockHash and add actual tests to cover it.
	case methodEstimateSmartFee:
		if c.testData.estFeeErr != nil {
			return nil, c.testData.estFeeErr
		}
		optimalRate := float64(optimalFeeRate) * 1e-5 // ~0.00024
		return json.Marshal(&btcjson.EstimateSmartFeeResult{
			Blocks:  2,
			FeeRate: &optimalRate,
		})
	case methodSendRawTransaction:
		var txHex string
		err := json.Unmarshal(params[0], &txHex)
		if err != nil {
			return nil, err
		}
		tx, err := msgTxFromHex(txHex)
		if err != nil {
			return nil, err
		}
		c.sentRawTx = tx
		if c.sendErr == nil && c.badSendHash == nil {
			h := tx.TxHash().String()
			return json.Marshal(&h)
		}
		if c.sendErr != nil {
			return nil, c.sendErr
		}
		return json.Marshal(c.badSendHash.String())
	case methodGetTxOut:
		return encodeOrError(c.txOutRes, c.txOutErr)
	case methodGetBestBlockHash:
		if c.getBestBlockHashErr != nil {
			return nil, c.getBestBlockHashErr
		}
		bestHash, _ := c.bestBlock()
		return json.Marshal(bestHash.String())
	case methodGetBlockHash:
		var blockHeight int64
		if err := json.Unmarshal(params[0], &blockHeight); err != nil {
			return nil, err
		}
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		for height, blockHash := range c.mainchain {
			if height == blockHeight {
				return json.Marshal(blockHash.String())
			}
		}
		return nil, fmt.Errorf("block not found")

	case methodGetRawMempool:
		hashes := make([]string, 0, len(c.mempoolTxs))
		for txHash := range c.mempoolTxs {
			hashes = append(hashes, txHash.String())
		}
		return json.Marshal(hashes)
	case methodGetRawTransaction:
		if c.testData.rawVerboseErr != nil {
			return nil, c.testData.rawVerboseErr
		}
		var hashStr string
		err := json.Unmarshal(params[0], &hashStr)
		if err != nil {
			return nil, err
		}
		txHash, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			return nil, err
		}
		msgTx := c.mempoolTxs[*txHash]
		if msgTx == nil {
			return nil, fmt.Errorf("transaction not found")
		}
		txB, _ := serializeMsgTx(msgTx)
		return json.Marshal(hex.EncodeToString(txB))
	case methodSignTx:
		if c.signTxErr != nil {
			return nil, c.signTxErr
		}
		signTxRes := SignTxResult{
			Complete: !c.sigIncomplete,
		}
		var msgHex string
		err := json.Unmarshal(params[0], &msgHex)
		if err != nil {
			return nil, fmt.Errorf("json.Unmarshal error for tRawRequester -> RawRequest -> methodSignTx: %v", err)
		}
		msgBytes, _ := hex.DecodeString(msgHex)
		txReader := bytes.NewReader(msgBytes)
		msgTx := wire.NewMsgTx(wire.TxVersion)
		err = msgTx.Deserialize(txReader)
		if err != nil {
			return nil, fmt.Errorf("MsgTx.Deserialize error for tRawRequester -> RawRequest -> methodSignTx: %v", err)
		}

		c.signFunc(msgTx)

		buf := new(bytes.Buffer)
		err = msgTx.Serialize(buf)
		if err != nil {
			return nil, fmt.Errorf("MsgTx.Serialize error for tRawRequester -> RawRequest -> methodSignTx: %v", err)
		}
		signTxRes.Hex = buf.Bytes()
		return mustMarshal(signTxRes), nil
	case methodGetBlock:
		c.blockchainMtx.Lock()
		defer c.blockchainMtx.Unlock()
		var blockHashStr string
		err := json.Unmarshal(params[0], &blockHashStr)
		if err != nil {
			return nil, err
		}

		blk, found := c.verboseBlocks[blockHashStr]
		if !found {
			return nil, fmt.Errorf("block not found")
		}
		var buf bytes.Buffer
		err = blk.msgBlock.Serialize(&buf)
		if err != nil {
			return nil, err
		}
		return json.Marshal(hex.EncodeToString(buf.Bytes()))

	case methodGetBlockHeader:
		var blkHash string
		_ = json.Unmarshal(params[0], &blkHash)
		block := c.getBlock(blkHash)
		if block == nil {
			return nil, fmt.Errorf("no block verbose found")
		}
		// block may get modified concurrently, lock mtx before reading fields.
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		return json.Marshal(&blockHeader{
			Hash:   block.msgBlock.BlockHash().String(),
			Height: block.height,
			// Confirmations: block.Confirmations,
			// Time:          block.Time,
		})
	case methodLockUnspent:
		if c.lockUnspentErr != nil {
			return json.Marshal(false)
		}
		coins := make([]*RPCOutpoint, 0)
		_ = json.Unmarshal(params[1], &coins)
		if string(params[0]) == "false" {
			c.lockedCoins = coins
		}
		return json.Marshal(true)
	case methodListLockUnspent:
		return mustMarshal(c.listLockUnspent), nil
	case methodGetBalances:
		return encodeOrError(c.getBalances, c.getBalancesErr)
	case methodChangeAddress:
		return encodeOrError(c.changeAddr, c.changeAddrErr)
	case methodNewAddress:
		return encodeOrError(c.newAddress, c.newAddressErr)
	case methodPrivKeyForAddress:
		if c.privKeyForAddrErr != nil {
			return nil, c.privKeyForAddrErr
		}
		return json.Marshal(c.privKeyForAddr.String())
	case methodGetTransaction:
		return encodeOrError(c.getTransaction, c.getTransactionErr)
	case methodGetBlockchainInfo:
		return encodeOrError(c.getBlockchainInfo, c.getBlockchainInfoErr)
	case methodLock:
		return nil, c.lockErr
	case methodUnlock:
		return nil, c.unlockErr
	case methodSendToAddress:
		return encodeOrError(c.sendToAddress, c.sendToAddressErr)
	case methodSetTxFee:
		return json.Marshal(c.setTxFee)
	case methodListUnspent:
		return encodeOrError(c.listUnspent, c.listUnspentErr)
	}
	panic("method not registered: " + method)
}

const testBlocksPerBlockTimeOffset = 4

func generateTestBlockTime(blockHeight int64) time.Time {
	return time.Unix(1e6, 0).Add(time.Duration(blockHeight) * maxFutureBlockTime / testBlocksPerBlockTimeOffset)
}

func (c *testData) addRawTx(blockHeight int64, tx *wire.MsgTx) (*chainhash.Hash, *wire.MsgBlock) {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	blockHash, found := c.mainchain[blockHeight]
	if !found {
		var newHash chainhash.Hash
		copy(newHash[:], randBytes(32))
		blockHash = &newHash
		prevBlock := &chainhash.Hash{}
		if blockHeight > 0 {
			var exists bool
			prevBlock, exists = c.mainchain[blockHeight-1]
			if !exists {
				prevBlock = &chainhash.Hash{}
			}
		}
		header := wire.NewBlockHeader(0, prevBlock, &chainhash.Hash{}, 1, 2)
		header.Timestamp = generateTestBlockTime(blockHeight)
		msgBlock := wire.NewMsgBlock(header)
		c.verboseBlocks[blockHash.String()] = &msgBlockWithHeight{
			msgBlock: msgBlock,
			height:   blockHeight,
		}
		c.mainchain[blockHeight] = blockHash
	}
	block := c.verboseBlocks[blockHash.String()]
	block.msgBlock.AddTransaction(tx)
	return blockHash, block.msgBlock
}

func (c *testData) addDBBlockForTx(txHash, blockHash *chainhash.Hash) {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	c.dbBlockForTx[*txHash] = &hashEntry{hash: *blockHash}
}

func (c *testData) getBlockAtHeight(blockHeight int64) (*chainhash.Hash, *msgBlockWithHeight) {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	blockHash, found := c.mainchain[blockHeight]
	if !found {
		return nil, nil
	}
	blk := c.verboseBlocks[blockHash.String()]
	return blockHash, blk
}

func (c *testData) truncateChains() {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	c.mainchain = make(map[int64]*chainhash.Hash)
	c.verboseBlocks = make(map[string]*msgBlockWithHeight)
	c.mempoolTxs = make(map[chainhash.Hash]*wire.MsgTx)
}

func makeRawTx(pkScripts []dex.Bytes, inputs []*wire.TxIn) *wire.MsgTx {
	tx := &wire.MsgTx{
		TxIn: inputs,
	}
	for _, pkScript := range pkScripts {
		tx.TxOut = append(tx.TxOut, wire.NewTxOut(1, pkScript))
	}
	return tx
}

func makeTxHex(pkScripts []dex.Bytes, inputs []*wire.TxIn) ([]byte, error) {
	msgTx := wire.NewMsgTx(wire.TxVersion)
	for _, txIn := range inputs {
		msgTx.AddTxIn(txIn)
	}
	for _, pkScript := range pkScripts {
		txOut := wire.NewTxOut(100000000, pkScript)
		msgTx.AddTxOut(txOut)
	}
	txBuf := bytes.NewBuffer(make([]byte, 0, dexbtc.MsgTxVBytes(msgTx)))
	err := msgTx.Serialize(txBuf)
	if err != nil {
		return nil, err
	}
	return txBuf.Bytes(), nil
}

func makeRPCVin(txHash *chainhash.Hash, vout uint32, sigScript []byte, witness [][]byte) *wire.TxIn {
	var rpcWitness []string
	for _, b := range witness {
		rpcWitness = append(rpcWitness, hex.EncodeToString(b))
	}

	return wire.NewTxIn(wire.NewOutPoint(txHash, vout), sigScript, witness)
}

func dummyInput() *wire.TxIn {
	return wire.NewTxIn(wire.NewOutPoint(&chainhash.Hash{0x01}, 0), nil, nil)
}

func dummyTx() *wire.MsgTx {
	return makeRawTx([]dex.Bytes{randBytes(32)}, []*wire.TxIn{dummyInput()})
}

func newTxOutResult(script []byte, value uint64, confs int64) *btcjson.GetTxOutResult {
	return &btcjson.GetTxOutResult{
		Confirmations: confs,
		Value:         float64(value) / 1e8,
		ScriptPubKey: btcjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(script),
		},
	}
}

func makeSwapContract(segwit bool, lockTimeOffset time.Duration) (secret []byte, secretHash [32]byte, pkScript, contract []byte, addr, contractAddr btcutil.Address, lockTime time.Time) {
	secret = randBytes(32)
	secretHash = sha256.Sum256(secret)

	addr = btcAddr(segwit)

	lockTime = time.Now().Add(lockTimeOffset)
	contract, err := dexbtc.MakeContract(addr, addr, secretHash[:], lockTime.Unix(), segwit, &chaincfg.MainNetParams)
	if err != nil {
		panic("error making swap contract:" + err.Error())
	}
	contractAddr, _ = scriptHashAddress(segwit, contract, &chaincfg.MainNetParams)
	pkScript, _ = txscript.PayToAddrScript(contractAddr)
	return
}

func tNewWallet(segwit bool, walletType string) (*ExchangeWallet, *testData, func(), error) {
	if segwit {
		tBTC.SwapSize = dexbtc.InitTxSizeSegwit
		tBTC.SwapSizeBase = dexbtc.InitTxSizeBaseSegwit
	} else {
		tBTC.SwapSize = dexbtc.InitTxSize
		tBTC.SwapSizeBase = dexbtc.InitTxSizeBase
	}

	data := newTestData()
	walletCfg := &asset.WalletConfig{
		TipChange: func(error) {},
	}
	walletCtx, shutdown := context.WithCancel(tCtx)
	cfg := &BTCCloneCFG{
		WalletCFG:           walletCfg,
		Symbol:              "btc",
		Logger:              tLogger,
		ChainParams:         &chaincfg.MainNetParams,
		WalletInfo:          WalletInfo,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		Segwit:              segwit,
	}

	// rpcClient := newRPCClient(requester, segwit, nil, false, minNetworkVersion, dex.StdOutLogger("RPCTEST", dex.LevelTrace), &chaincfg.MainNetParams)

	var wallet *ExchangeWallet
	var err error
	switch walletType {
	case walletTypeRPC:
		wallet, err = newRPCWallet(&tRawRequester{data}, cfg, &WalletConfig{})
	case walletTypeSPV:
		wallet, err = newUnconnectedWallet(cfg, &WalletConfig{})
		if err == nil {
			neutrinoClient := &tNeutrinoClient{data}
			wallet.node = &spvWallet{
				chainParams: &chaincfg.MainNetParams,
				wallet:      &tBtcWallet{data},
				cl:          neutrinoClient,
				chainClient: nil,
				acctNum:     0,
				txBlocks:    data.dbBlockForTx,
				checkpoints: data.checkpoints,
				log:         cfg.Logger.SubLogger("SPV"),
				loader:      nil,
			}
		}
	}

	if err != nil {
		shutdown()
		return nil, nil, nil, err
	}
	// Initialize the best block.
	bestHash, err := wallet.node.getBestBlockHash()
	if err != nil {
		shutdown()
		return nil, nil, nil, err
	}
	wallet.tipMtx.Lock()
	wallet.currentTip = &block{
		height: data.GetBestBlockHeight(),
		hash:   *bestHash,
	}
	wallet.tipMtx.Unlock()
	go wallet.run(walletCtx)

	return wallet, data, shutdown, nil
}

func mustMarshal(thing interface{}) []byte {
	b, err := json.Marshal(thing)
	if err != nil {
		panic("mustMarshal error: " + err.Error())
	}
	return b
}

func TestMain(m *testing.M) {
	tLogger = dex.StdOutLogger("TEST", dex.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	tTxHash, _ = chainhash.NewHashFromStr(tTxID)
	tP2PKH, _ = hex.DecodeString("76a9148fc02268f208a61767504fe0b48d228641ba81e388ac")
	tP2WPKH, _ = hex.DecodeString("0014148fc02268f208a61767504fe0b48d228641ba81")
	// tP2SH, _ = hex.DecodeString("76a91412a9abf5c32392f38bd8a1f57d81b1aeecc5699588ac")
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

type testFunc func(t *testing.T, segwit bool, walletType string)

func runRubric(t *testing.T, f testFunc) {
	t.Run("rpc|segwit", func(t *testing.T) {
		f(t, true, walletTypeRPC)
	})
	t.Run("rpc|non-segwit", func(t *testing.T) {
		f(t, false, walletTypeRPC)
	})
	t.Run("spv|segwit", func(t *testing.T) {
		f(t, true, walletTypeSPV)
	})
}

func TestAvailableFund(t *testing.T) {
	runRubric(t, testAvailableFund)
}

func testAvailableFund(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	// With an empty list returned, there should be no error, but the value zero
	// should be returned.
	unspents := make([]*ListUnspentResult, 0)
	node.listUnspent = unspents // only needed for Fund, not Balance
	node.listLockUnspent = []*RPCOutpoint{}
	var bals GetBalancesResult
	node.getBalances = &bals
	bal, err := wallet.Balance()
	if err != nil {
		t.Fatalf("error for zero utxos: %v", err)
	}
	if bal.Available != 0 {
		t.Fatalf("expected available = 0, got %d", bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected unconf = 0, got %d", bal.Immature)
	}

	node.getBalancesErr = tErr
	_, err = wallet.Balance()
	if err == nil {
		t.Fatalf("no wallet error for rpc error")
	}
	node.getBalancesErr = nil
	var littleLots uint64 = 12
	littleOrder := tLotSize * littleLots
	littleFunds := calc.RequiredOrderFunds(littleOrder, dexbtc.RedeemP2PKHInputSize, littleLots, tBTC)
	littleUTXO := &ListUnspentResult{
		TxID:          tTxID,
		Address:       "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi",
		Amount:        float64(littleFunds) / 1e8,
		Confirmations: 0,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents = append(unspents, littleUTXO)
	node.listUnspent = unspents
	bals.Mine.Trusted = float64(littleFunds) / 1e8
	node.getBalances = &bals
	lockedVal := uint64(1e6)
	node.listLockUnspent = []*RPCOutpoint{
		{
			TxID: tTxID,
			Vout: 1,
		},
	}

	msgTx := makeRawTx([]dex.Bytes{{0x01}, {0x02}}, []*wire.TxIn{dummyInput()})
	msgTx.TxOut[1].Value = int64(lockedVal)
	txBuf := bytes.NewBuffer(make([]byte, 0, dexbtc.MsgTxVBytes(msgTx)))
	msgTx.Serialize(txBuf)
	const blockHeight = 5
	blockHash, _ := node.addRawTx(blockHeight, msgTx)

	node.getTransaction = &GetTransactionResult{
		BlockHash:  blockHash.String(),
		BlockIndex: blockHeight,
		Details: []*WalletTxDetails{
			{
				Amount: float64(lockedVal) / 1e8,
				Vout:   1,
			},
		},
		Hex: txBuf.Bytes(),
	}

	bal, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 1 utxo: %v", err)
	}
	if bal.Available != littleFunds-lockedVal {
		t.Fatalf("expected available = %d for confirmed utxos, got %d", littleOrder-lockedVal, bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected immature = 0, got %d", bal.Immature)
	}
	if bal.Locked != lockedVal {
		t.Fatalf("expected locked = %d, got %d", lockedVal, bal.Locked)
	}

	var lottaLots uint64 = 100
	lottaOrder := tLotSize * lottaLots
	// Add funding for an extra input to accommodate the later combined tests.
	lottaFunds := calc.RequiredOrderFunds(lottaOrder, 2*dexbtc.RedeemP2PKHInputSize, lottaLots, tBTC)
	lottaUTXO := &ListUnspentResult{
		TxID:          tTxID,
		Address:       "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi",
		Amount:        float64(lottaFunds) / 1e8,
		Confirmations: 1,
		Vout:          1,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents = append(unspents, lottaUTXO)
	littleUTXO.Confirmations = 1
	node.listUnspent = unspents
	bals.Mine.Trusted += float64(lottaFunds) / 1e8
	node.getBalances = &bals
	bal, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 2 utxos: %v", err)
	}
	if bal.Available != littleFunds+lottaFunds-lockedVal {
		t.Fatalf("expected available = %d for 2 outputs, got %d", littleFunds+lottaFunds-lockedVal, bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected immature = 0 for 2 outputs, got %d", bal.Immature)
	}

	ord := &asset.Order{
		Value:         0,
		MaxSwapCount:  1,
		DEXConfig:     tBTC,
		FeeSuggestion: feeSuggestion,
	}

	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / tLotSize
	}

	// Zero value
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no funding error for zero value")
	}

	// Nothing to spend
	node.listUnspent = []*ListUnspentResult{}
	setOrderValue(littleOrder)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for zero utxos")
	}
	node.listUnspent = unspents

	// RPC error
	node.listUnspentErr = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no funding error for rpc error")
	}
	node.listUnspentErr = nil

	// Negative response when locking outputs.
	// There is no way to error locking outpoints in spv
	if walletType != walletTypeSPV {
		node.lockUnspentErr = tErr
		_, _, err = wallet.FundOrder(ord)
		if err == nil {
			t.Fatalf("no error for lockunspent result = false: %v", err)
		}
		node.lockUnspentErr = nil
	}

	// Fund a little bit, with unsafe littleUTXO.
	littleUTXO.Safe = false
	littleUTXO.Confirmations = 0
	node.listUnspent = unspents
	spendables, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding small amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v := spendables[0].Value()
	if v != lottaFunds { // has to pick the larger output
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}

	// Now with safe confirmed littleUTXO.
	littleUTXO.Safe = true
	littleUTXO.Confirmations = 2
	node.listUnspent = unspents
	spendables, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding small amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != littleFunds {
		t.Fatalf("expected spendable of value %d, got %d", littleFunds, v)
	}

	// Return/unlock the reserved coins to avoid warning in subsequent tests
	// about fundingCoins map containing the coins already. i.e.
	// "Known order-funding coin %v returned by listunspent"
	_ = wallet.ReturnCoins(spendables)

	// Make lottaOrder unconfirmed like littleOrder, favoring little now.
	lottaUTXO.Confirmations = 0
	node.listUnspent = unspents
	spendables, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding small amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != littleFunds { // now picks the smaller output
		t.Fatalf("expected spendable of value %d, got %d", littleFunds, v)
	}
	_ = wallet.ReturnCoins(spendables)

	// Fund a lotta bit, covered by just the lottaBit UTXO.
	setOrderValue(lottaOrder)
	spendables, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding large amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != lottaFunds {
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}
	_ = wallet.ReturnCoins(spendables)

	// require both spendables
	extraLottaOrder := littleOrder + lottaOrder
	extraLottaLots := littleLots + lottaLots
	setOrderValue(extraLottaOrder)
	spendables, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding large amount: %v", err)
	}
	if len(spendables) != 2 {
		t.Fatalf("expected 2 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != lottaFunds {
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}
	_ = wallet.ReturnCoins(spendables)

	// Not enough to cover transaction fees.
	tweak := float64(littleFunds+lottaFunds-calc.RequiredOrderFunds(extraLottaOrder, 2*dexbtc.RedeemP2PKHInputSize, extraLottaLots, tBTC)+1) / 1e8
	lottaUTXO.Amount -= tweak
	node.listUnspent = unspents
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough to cover tx fees")
	}
	lottaUTXO.Amount += tweak
	node.listUnspent = unspents

	// Prepare for a split transaction.
	baggageFees := tBTC.MaxFeeRate * splitTxBaggage
	node.changeAddr = tP2WPKHAddr
	wallet.useSplitTx = true
	// No error when no split performed cuz math.
	coins, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for no-split split: %v", err)
	}
	// Should be both coins.
	if len(coins) != 2 {
		t.Fatalf("no-split split didn't return both coins")
	}
	_ = wallet.ReturnCoins(coins)

	// No split because not standing order.
	ord.Immediate = true
	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for no-split split: %v", err)
	}
	ord.Immediate = false
	if len(coins) != 2 {
		t.Fatalf("no-split split didn't return both coins")
	}
	_ = wallet.ReturnCoins(coins)

	// With a little more locked, the split should be performed.
	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}
	lottaUTXO.Amount += float64(baggageFees) / 1e8
	node.listUnspent = unspents
	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for split tx: %v", err)
	}
	// Should be just one coin.
	if len(coins) != 1 {
		t.Fatalf("split failed - coin count != 1")
	}
	if node.sentRawTx == nil {
		t.Fatalf("split failed - no tx sent")
	}
	_ = wallet.ReturnCoins(coins)

	// // Hit some error paths.

	// Split transaction requires valid fee suggestion.
	// TODO:
	// 1.0: Error when no suggestion.
	// ord.FeeSuggestion = 0
	// _, _, err = wallet.FundOrder(ord)
	// if err == nil {
	// 	t.Fatalf("no error for no fee suggestions on split tx")
	// }
	ord.FeeSuggestion = tBTC.MaxFeeRate + 1
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for no fee suggestions on split tx")
	}
	// Check success again.
	ord.FeeSuggestion = tBTC.MaxFeeRate
	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error fixing split tx: %v", err)
	}
	_ = wallet.ReturnCoins(coins)

	// GetRawChangeAddress error
	node.changeAddrErr = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for split tx change addr error")
	}
	node.changeAddrErr = nil

	// SendRawTx error
	node.sendErr = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for split tx send error")
	}
	node.sendErr = nil

	// Success again.
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for split tx recovery run")
	}
}

// Since ReturnCoins takes the asset.Coin interface, make sure any interface
// is acceptable.
type tCoin struct{ id []byte }

func (c *tCoin) ID() dex.Bytes {
	if len(c.id) > 0 {
		return c.id
	}
	return make([]byte, 36)
}
func (c *tCoin) String() string { return hex.EncodeToString(c.id) }
func (c *tCoin) Value() uint64  { return 100 }

func TestReturnCoins(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet(true, walletTypeRPC)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	// Test it with the local output type.
	coins := asset.Coins{
		newOutput(tTxHash, 0, 1),
	}
	err = wallet.ReturnCoins(coins)
	if err != nil {
		t.Fatalf("error with output type coins: %v", err)
	}

	// Should error for no coins.
	err = wallet.ReturnCoins(asset.Coins{})
	if err == nil {
		t.Fatalf("no error for zero coins")
	}

	// Have the RPC return negative response.
	node.lockUnspentErr = tErr
	err = wallet.ReturnCoins(coins)
	if err == nil {
		t.Fatalf("no error for RPC failure")
	}
	node.lockUnspentErr = nil

	// ReturnCoins should accept any type that implements asset.Coin.
	err = wallet.ReturnCoins(asset.Coins{&tCoin{}, &tCoin{}})
	if err != nil {
		t.Fatalf("error with custom coin type: %v", err)
	}
}

func TestFundingCoins(t *testing.T) {
	// runRubric(t, testFundingCoins)
	testFundingCoins(t, false, walletTypeRPC)
}

func testFundingCoins(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	const vout = 1
	const txBlockHeight = 3
	tx := makeRawTx([]dex.Bytes{{0x01}, tP2PKH}, []*wire.TxIn{dummyInput()})
	txHash := tx.TxHash()
	_, _ = node.addRawTx(txBlockHeight, tx)
	coinID := toCoinID(&txHash, vout)
	// Make spendable (confs > 0)
	node.addRawTx(txBlockHeight+1, dummyTx())

	p2pkhUnspent := &ListUnspentResult{
		TxID:         txHash.String(),
		Vout:         vout,
		ScriptPubKey: tP2PKH,
		Spendable:    true,
		Solvable:     true,
		Safe:         true,
		Amount:       1,
	}
	unspents := []*ListUnspentResult{p2pkhUnspent}
	node.listLockUnspent = []*RPCOutpoint{}
	node.listUnspent = unspents
	coinIDs := []dex.Bytes{coinID}

	ensureGood := func() {
		t.Helper()
		coins, err := wallet.FundingCoins(coinIDs)
		if err != nil {
			t.Fatalf("FundingCoins error: %v", err)
		}
		if len(coins) != 1 {
			t.Fatalf("expected 1 coin, got %d", len(coins))
		}
	}
	ensureGood()

	ensureErr := func(tag string) {
		t.Helper()
		// Clear the cache.
		wallet.fundingCoins = make(map[outPoint]*utxo)
		_, err := wallet.FundingCoins(coinIDs)
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	// No coins
	node.listUnspent = []*ListUnspentResult{}
	ensureErr("no coins")
	node.listUnspent = unspents

	// RPC error
	node.listUnspentErr = tErr
	ensureErr("rpc coins")
	node.listUnspentErr = nil

	// Bad coin ID.
	ogIDs := coinIDs
	coinIDs = []dex.Bytes{randBytes(35)}
	ensureErr("bad coin ID")
	coinIDs = ogIDs

	// Coins locked but not in wallet.fundingCoins.
	node.listLockUnspent = []*RPCOutpoint{
		{TxID: p2pkhUnspent.TxID, Vout: p2pkhUnspent.Vout},
	}
	node.listUnspent = []*ListUnspentResult{}
	getTxRes := &GetTransactionResult{
		Details: []*WalletTxDetails{
			{
				Vout:   p2pkhUnspent.Vout,
				Amount: p2pkhUnspent.Amount,
			},
		},
	}
	node.getTransaction = getTxRes

	ensureGood()
}

func checkMaxOrder(t *testing.T, wallet *ExchangeWallet, lots, swapVal, maxFees, estWorstCase, estBestCase, locked uint64) {
	t.Helper()
	maxOrder, err := wallet.MaxOrder(tLotSize, feeSuggestion, tBTC)
	if err != nil {
		t.Fatalf("MaxOrder error: %v", err)
	}
	checkSwapEstimate(t, maxOrder, lots, swapVal, maxFees, estWorstCase, estBestCase, locked)
}

func checkSwapEstimate(t *testing.T, est *asset.SwapEstimate, lots, swapVal, maxFees, estWorstCase, estBestCase, locked uint64) {
	t.Helper()
	if est.Lots != lots {
		t.Fatalf("Estimate has wrong Lots. wanted %d, got %d", lots, est.Lots)
	}
	if est.Value != swapVal {
		t.Fatalf("Estimate has wrong Value. wanted %d, got %d", swapVal, est.Value)
	}
	if est.MaxFees != maxFees {
		t.Fatalf("Estimate has wrong MaxFees. wanted %d, got %d", maxFees, est.MaxFees)
	}
	if est.RealisticWorstCase != estWorstCase {
		t.Fatalf("Estimate has wrong RealisticWorstCase. wanted %d, got %d", estWorstCase, est.RealisticWorstCase)
	}
	if est.RealisticBestCase != estBestCase {
		t.Fatalf("Estimate has wrong RealisticBestCase. wanted %d, got %d", estBestCase, est.RealisticBestCase)
	}
	if est.Locked != locked {
		t.Fatalf("Estimate has wrong Locked. wanted %d, got %d", locked, est.Locked)
	}
}

func TestFundEdges(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet(false, walletTypeRPC)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	swapVal := uint64(1e7)
	lots := swapVal / tLotSize

	checkMax := func(lots, swapVal, maxFees, estWorstCase, estBestCase, locked uint64) {
		t.Helper()
		checkMaxOrder(t, wallet, lots, swapVal, maxFees, estWorstCase, estBestCase, locked)
	}

	// Base Fees
	// fee_rate: 34 satoshi / vbyte (MaxFeeRate)
	// swap_size: 225 bytes (InitTxSize)
	// p2pkh input: 149 bytes (RedeemP2PKHInputSize)

	// NOTE: Shouldn't swap_size_base be 73 bytes?

	// swap_size_base: 76 bytes (225 - 149 p2pkh input) (InitTxSizeBase)
	// lot_size: 1e6
	// swap_value: 1e7
	// lots = swap_value / lot_size = 10
	//   total_bytes = first_swap_size + chained_swap_sizes
	//   chained_swap_sizes = (lots - 1) * swap_size
	//   first_swap_size = swap_size_base + backing_bytes
	//   total_bytes = swap_size_base + backing_bytes + (lots - 1) * swap_size
	//   base_tx_bytes = total_bytes - backing_bytes
	// base_tx_bytes = (lots - 1) * swap_size + swap_size_base = 9 * 225 + 76 = 2101
	// base_fees = base_tx_bytes * fee_rate = 2101 * 34 = 71434
	// backing_bytes: 1x P2PKH inputs = dexbtc.P2PKHInputSize = 149 bytes
	// backing_fees: 149 * fee_rate(34 atoms/byte) = 5066 atoms
	// total_bytes  = base_tx_bytes + backing_bytes = 2101 + 149 = 2250
	// total_fees: base_fees + backing_fees = 71434 + 5066 = 76500 atoms
	//          OR total_bytes * fee_rate = 2250 * 34 = 76500
	// base_best_case_bytes = swap_size_base + (lots - 1) * swap_output_size (P2SHOutputSize) + backing_bytes
	//                      = 76 + 9*32 + 149 = 513
	const swapSize = 225
	const totalBytes = 2250
	const bestCaseBytes = 513
	const swapOutputSize = 32                           // (P2SHOutputSize)
	backingFees := uint64(totalBytes) * tBTC.MaxFeeRate // total_bytes * fee_rate
	p2pkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2PKHAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents := []*ListUnspentResult{p2pkhUnspent}
	node.listUnspent = unspents
	ord := &asset.Order{
		Value:         swapVal,
		MaxSwapCount:  lots,
		DEXConfig:     tBTC,
		FeeSuggestion: feeSuggestion,
	}

	var feeReduction uint64 = swapSize * tBTC.MaxFeeRate
	estFeeReduction := swapSize * feeSuggestion
	checkMax(lots-1, swapVal-tLotSize, backingFees-feeReduction, totalBytes*feeSuggestion-estFeeReduction,
		(bestCaseBytes-swapOutputSize)*feeSuggestion, swapVal+backingFees-1)

	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2pkh utxo")
	}
	// Now add the needed satoshi and try again.
	p2pkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.listUnspent = unspents

	checkMax(lots, swapVal, backingFees, totalBytes*feeSuggestion, bestCaseBytes*feeSuggestion, swapVal+backingFees)

	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2pkh utxo: %v", err)
	}

	// For a split transaction, we would need to cover the splitTxBaggage as
	// well.
	wallet.useSplitTx = true
	node.changeAddr = tP2WPKHAddr
	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}
	backingFees = uint64(totalBytes+splitTxBaggage) * tBTC.MaxFeeRate
	// 1 too few atoms
	v := swapVal + backingFees - 1
	p2pkhUnspent.Amount = float64(v) / 1e8
	node.listUnspent = unspents

	coins, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when skipping split tx due to baggage: %v", err)
	}
	if coins[0].Value() != v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	// Just enough.
	v = swapVal + backingFees
	p2pkhUnspent.Amount = float64(v) / 1e8
	node.listUnspent = unspents

	checkMax(lots, swapVal, backingFees, (totalBytes+splitTxBaggage)*feeSuggestion, (bestCaseBytes+splitTxBaggage)*feeSuggestion, v)

	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding split tx: %v", err)
	}
	if coins[0].Value() == v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	wallet.useSplitTx = false

	// P2SH(P2PKH) p2sh pkScript = 23 bytes, p2pkh pkScript (redeemscript) = 25 bytes
	// sigScript = signature(1 + 73) + pubkey(1 + 33) + redeemscript(1 + 25) = 134
	// P2SH input size = overhead(40) + sigScriptData(1 + 134) = 40 + 135 = 175 bytes
	// backing fees: 175 bytes * fee_rate(34) = 5950 satoshi
	// Use 1 P2SH AND 1 P2PKH from the previous test.
	// total: 71434 + 5950 + 5066 = 82450 satoshi
	p2shRedeem, _ := hex.DecodeString("76a914db1755408acd315baa75c18ebbe0e8eaddf64a9788ac") // 25, p2pkh redeem script
	scriptAddr := "37XDx4CwPVEg5mC3awSPGCKA5Fe5FdsAS2"
	p2shScriptPubKey, _ := hex.DecodeString("a9143ff6a24a50135f69be9ffed744443da08408fc1a87") // 23, p2sh pkScript
	backingFees = 82450
	halfSwap := swapVal / 2
	p2shUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       scriptAddr,
		Amount:        float64(halfSwap) / 1e8,
		Confirmations: 10,
		ScriptPubKey:  p2shScriptPubKey,
		RedeemScript:  p2shRedeem,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	p2pkhUnspent.Amount = float64(halfSwap+backingFees-1) / 1e8
	unspents = []*ListUnspentResult{p2pkhUnspent, p2shUnspent}
	node.listUnspent = unspents
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in two utxos")
	}
	p2pkhUnspent.Amount = float64(halfSwap+backingFees) / 1e8
	node.listUnspent = unspents
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in two utxos: %v", err)
	}

	// P2WPKH witness: RedeemP2WPKHInputWitnessWeight = 109
	// P2WPKH input size = overhead(40) + no sigScript(1+0) + witness(ceil(109/4)) = 69 vbytes
	// backing fees: 69 * fee_rate(34) = 2346 satoshi
	// total: base_fees(71434) + 2346 = 73780 satoshi
	backingFees = 73780
	p2wpkhAddr := tP2WPKHAddr
	p2wpkhPkScript, _ := hex.DecodeString("0014054a40a6aa7c1f6bd15f286debf4f33cef0e21a1")
	p2wpkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       p2wpkhAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 3,
		ScriptPubKey:  p2wpkhPkScript,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents = []*ListUnspentResult{p2wpkhUnspent}
	node.listUnspent = unspents
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wpkh utxo")
	}
	p2wpkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.listUnspent = unspents
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wpkh utxo: %v", err)
	}

	// P2WSH(P2WPKH)
	//  p2wpkh redeem script length, btc.P2WPKHPkScriptSize: 22
	// witness: version(1) + signature(1 + 73) + pubkey(1 + 33) + redeemscript(1 + 22) = 132
	// input size: overhead(40) + no sigScript(1+0) + witness(132)/4 = 74 vbyte
	// backing fees: 74 * 34 = 2516 satoshi
	// total: base_fees(71434) + 2516 = 73950 satoshi
	backingFees = 73950
	p2wpkhRedeemScript, _ := hex.DecodeString("0014b71554f9a66ef4fa4dbeddb9fa491f5a1d938ebc") //22
	p2wshAddr := "bc1q9heng7q275grmy483cueqrr00dvyxpd8w6kes3nzptm7087d6lvsvffpqf"
	p2wshPkScript, _ := hex.DecodeString("00202df334780af5103d92a78e39900c6f7b584305a776ad9846620af7e79fcdd7d9") //34
	p2wpshUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       p2wshAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 7,
		ScriptPubKey:  p2wshPkScript,
		RedeemScript:  p2wpkhRedeemScript,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents = []*ListUnspentResult{p2wpshUnspent}
	node.listUnspent = unspents
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wsh utxo")
	}
	p2wpshUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.listUnspent = unspents
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wsh utxo: %v", err)
	}
}

func TestFundEdgesSegwit(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet(true, walletTypeRPC)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	swapVal := uint64(1e7)
	lots := swapVal / tLotSize

	checkMax := func(lots, swapVal, maxFees, estWorstCase, estBestCase, locked uint64) {
		t.Helper()
		checkMaxOrder(t, wallet, lots, swapVal, maxFees, estWorstCase, estBestCase, locked)
	}

	// Base Fees
	// fee_rate: 34 satoshi / vbyte (MaxFeeRate)

	// swap_size: 153 bytes (InitTxSizeSegwit)
	// p2wpkh input, incl. marker and flag: 69 bytes (RedeemP2WPKHInputSize + ((RedeemP2WPKHInputWitnessWeight + 2 + 3) / 4))
	// swap_size_base: 84 bytes (153 - 69 p2pkh input) (InitTxSizeBaseSegwit)

	// lot_size: 1e6
	// swap_value: 1e7
	// lots = swap_value / lot_size = 10
	//   total_bytes = first_swap_size + chained_swap_sizes
	//   chained_swap_sizes = (lots - 1) * swap_size
	//   first_swap_size = swap_size_base + backing_bytes
	//   total_bytes  = swap_size_base + backing_bytes + (lots - 1) * swap_size
	//   base_tx_bytes = total_bytes - backing_bytes
	// base_tx_bytes = (lots - 1) * swap_size + swap_size_base = 9 * 153 + 84 = 1461
	// base_fees = base_tx_bytes * fee_rate = 1461 * 34 = 49674
	// backing_bytes: 1x P2WPKH-spending input = p2wpkh input = 69 bytes
	// backing_fees: 69 * fee_rate(34 atoms/byte) = 2346 atoms
	// total_bytes  = base_tx_bytes + backing_bytes = 1461 + 69 = 1530
	// total_fees: base_fees + backing_fees = 49674 + 2346 = 52020 atoms
	//          OR total_bytes * fee_rate = 1530 * 34 = 52020
	// base_best_case_bytes = swap_size_base + (lots - 1) * swap_output_size (P2SHOutputSize) + backing_bytes
	//                      = 84 + 9*43 + 69 = 540
	const swapSize = 153
	const totalBytes = 1530
	const bestCaseBytes = 540
	const swapOutputSize = 43                           // (P2WSHOutputSize)
	backingFees := uint64(totalBytes) * tBTC.MaxFeeRate // total_bytes * fee_rate
	p2wpkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2WPKHAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  tP2WPKH,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents := []*ListUnspentResult{p2wpkhUnspent}
	node.listUnspent = unspents
	ord := &asset.Order{
		Value:         swapVal,
		MaxSwapCount:  lots,
		DEXConfig:     tBTC,
		FeeSuggestion: feeSuggestion,
	}

	var feeReduction uint64 = swapSize * tBTC.MaxFeeRate
	estFeeReduction := swapSize * feeSuggestion
	checkMax(lots-1, swapVal-tLotSize, backingFees-feeReduction, totalBytes*feeSuggestion-estFeeReduction,
		(bestCaseBytes-swapOutputSize)*feeSuggestion, swapVal+backingFees-1)

	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wpkh utxo")
	}
	// Now add the needed satoshi and try again.
	p2wpkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.listUnspent = unspents

	checkMax(lots, swapVal, backingFees, totalBytes*feeSuggestion, bestCaseBytes*feeSuggestion, swapVal+backingFees)

	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wpkh utxo: %v", err)
	}

	// For a split transaction, we would need to cover the splitTxBaggage as
	// well.
	wallet.useSplitTx = true
	node.changeAddr = tP2WPKHAddr
	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}
	backingFees = uint64(totalBytes+splitTxBaggageSegwit) * tBTC.MaxFeeRate
	v := swapVal + backingFees - 1
	p2wpkhUnspent.Amount = float64(v) / 1e8
	node.listUnspent = unspents
	coins, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when skipping split tx because not enough to cover baggage: %v", err)
	}
	if coins[0].Value() != v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	// Now get the split.
	v = swapVal + backingFees
	p2wpkhUnspent.Amount = float64(v) / 1e8
	node.listUnspent = unspents

	checkMax(lots, swapVal, backingFees, (totalBytes+splitTxBaggageSegwit)*feeSuggestion, (bestCaseBytes+splitTxBaggageSegwit)*feeSuggestion, v)

	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding split tx: %v", err)
	}
	if coins[0].Value() == v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	wallet.useSplitTx = false
}

func TestSwap(t *testing.T) {
	runRubric(t, testSwap)
}

func testSwap(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	swapVal := toSatoshi(5)
	coins := asset.Coins{
		newOutput(tTxHash, 0, toSatoshi(3)),
		newOutput(tTxHash, 0, toSatoshi(3)),
	}
	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	node.newAddress = addrStr
	node.changeAddr = addrStr

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}
	node.privKeyForAddr = wif

	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	contract := &asset.Contract{
		Address:    addrStr,
		Value:      swapVal,
		SecretHash: secretHash,
		LockTime:   uint64(time.Now().Unix()),
	}

	swaps := &asset.Swaps{
		Inputs:     coins,
		Contracts:  []*asset.Contract{contract},
		LockChange: true,
		FeeRate:    tBTC.MaxFeeRate,
	}

	// Aim for 3 signature cycles.
	sigSizer := 0
	node.signFunc = func(tx *wire.MsgTx) {
		var sizeTweak int
		if sigSizer%2 == 0 {
			sizeTweak = -2
		}
		sigSizer++
		signFunc(tx, sizeTweak, wallet.segwit)
	}

	// This time should succeed.
	_, changeCoin, feesPaid, err := wallet.Swap(swaps)
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}

	// Make sure the change coin is locked.
	if len(node.lockedCoins) != 1 {
		t.Fatalf("did not lock change coin")
	}
	txHash, _, _ := decodeCoinID(changeCoin.ID())
	if node.lockedCoins[0].TxID != txHash.String() {
		t.Fatalf("wrong coin locked during swap")
	}

	// Fees should be returned.
	minFees := tBTC.MaxFeeRate * dexbtc.MsgTxVBytes(node.sentRawTx)
	if feesPaid < minFees {
		t.Fatalf("sent fees, %d, less than required fees, %d", feesPaid, minFees)
	}

	// Not enough funds
	swaps.Inputs = coins[:1]
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for listunspent not enough funds")
	}
	swaps.Inputs = coins

	// AddressPKH error
	node.newAddressErr = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getnewaddress rpc error")
	}
	node.newAddressErr = nil

	// ChangeAddress error
	node.changeAddrErr = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.changeAddrErr = nil

	// SignTx error
	node.signTxErr = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for signrawtransactionwithwallet rpc error")
	}
	node.signTxErr = nil

	// incomplete signatures
	node.sigIncomplete = true
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for incomplete signature rpc error")
	}
	node.sigIncomplete = false

	// Make sure we can succeed again.
	_, _, _, err = wallet.Swap(swaps)
	if err != nil {
		t.Fatalf("re-swap error: %v", err)
	}
}

type TAuditInfo struct{}

func (ai *TAuditInfo) Recipient() string     { return tP2PKHAddr }
func (ai *TAuditInfo) Expiration() time.Time { return time.Time{} }
func (ai *TAuditInfo) Coin() asset.Coin      { return &tCoin{} }
func (ai *TAuditInfo) Contract() dex.Bytes   { return nil }
func (ai *TAuditInfo) SecretHash() dex.Bytes { return nil }

func TestRedeem(t *testing.T) {
	runRubric(t, testRedeem)
}

func testRedeem(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	swapVal := toSatoshi(5)

	secret, _, _, contract, addr, _, lockTime := makeSwapContract(segwit, time.Hour*12)

	coin := newOutput(tTxHash, 0, swapVal)
	ci := &asset.AuditInfo{
		Coin:       coin,
		Contract:   contract,
		Recipient:  addr.String(),
		Expiration: lockTime,
	}

	redemption := &asset.Redemption{
		Spends: ci,
		Secret: secret,
	}

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}

	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	node.changeAddr = addrStr
	node.privKeyForAddr = wif

	redemptions := &asset.RedeemForm{
		Redemptions: []*asset.Redemption{redemption},
	}

	_, _, feesPaid, err := wallet.Redeem(redemptions)
	if err != nil {
		t.Fatalf("redeem error: %v", err)
	}

	// Check that fees are returned.
	minFees := optimalFeeRate * dexbtc.MsgTxVBytes(node.sentRawTx)
	if feesPaid < minFees {
		t.Fatalf("sent fees, %d, less than expected minimum fees, %d", feesPaid, minFees)
	}

	// No audit info
	redemption.Spends = nil
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for nil AuditInfo")
	}
	redemption.Spends = ci

	// Spoofing AuditInfo is not allowed.
	redemption.Spends = &asset.AuditInfo{}
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for spoofed AuditInfo")
	}
	redemption.Spends = ci

	// Wrong secret hash
	redemption.Secret = randBytes(32)
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for wrong secret")
	}
	redemption.Secret = secret

	// too low of value
	coin.value = 200
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for redemption not worth the fees")
	}
	coin.value = swapVal

	// Change address error
	node.changeAddrErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for change address error")
	}
	node.changeAddrErr = nil

	// Missing priv key error
	node.privKeyForAddrErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for missing private key")
	}
	node.privKeyForAddrErr = nil

	// Send error
	node.sendErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for send error")
	}
	node.sendErr = nil

	// Wrong hash
	var h chainhash.Hash
	h[0] = 0x01
	node.badSendHash = &h
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for wrong return hash")
	}
	node.badSendHash = nil
}

func TestSignMessage(t *testing.T) {
	runRubric(t, testSignMessage)
}

func testSignMessage(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	vout := uint32(5)
	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, pubKey := btcec.PrivKeyFromBytes(btcec.S256(), privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}

	msg := randBytes(36)
	pk := pubKey.SerializeCompressed()
	signature, err := privKey.Sign(msg)
	if err != nil {
		t.Fatalf("signature error: %v", err)
	}
	sig := signature.Serialize()

	pt := newOutPoint(tTxHash, vout)
	utxo := &utxo{address: tP2PKHAddr}
	wallet.fundingCoins[pt] = utxo
	node.privKeyForAddr = wif
	node.signMsgFunc = func(params []json.RawMessage) (json.RawMessage, error) {
		if len(params) != 2 {
			t.Fatalf("expected 2 params, found %d", len(params))
		}
		var sentKey string
		var sentMsg dex.Bytes
		err := json.Unmarshal(params[0], &sentKey)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		_ = sentMsg.UnmarshalJSON(params[1])
		if sentKey != wif.String() {
			t.Fatalf("received wrong key. expected '%s', got '%s'", wif.String(), sentKey)
		}
		var checkMsg dex.Bytes = msg
		if sentMsg.String() != checkMsg.String() {
			t.Fatalf("received wrong message. expected '%s', got '%s'", checkMsg.String(), sentMsg.String())
		}
		sig, _ := wif.PrivKey.Sign(sentMsg)
		r, _ := json.Marshal(base64.StdEncoding.EncodeToString(sig.Serialize()))
		return r, nil
	}

	var coin asset.Coin = newOutput(tTxHash, vout, 5e7)
	pubkeys, sigs, err := wallet.SignMessage(coin, msg)
	if err != nil {
		t.Fatalf("SignMessage error: %v", err)
	}
	if len(pubkeys) != 1 {
		t.Fatalf("expected 1 pubkey, received %d", len(pubkeys))
	}
	if len(sigs) != 1 {
		t.Fatalf("expected 1 sig, received %d", len(sigs))
	}
	if !bytes.Equal(pk, pubkeys[0]) {
		t.Fatalf("wrong pubkey. expected %x, got %x", pubkeys[0], pk)
	}
	if !bytes.Equal(sig, sigs[0]) {
		t.Fatalf("wrong signature. exptected %x, got %x", sigs[0], sig)
	}

	// Unknown UTXO
	delete(wallet.fundingCoins, pt)
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for unknown utxo")
	}
	wallet.fundingCoins[pt] = utxo

	// dumpprivkey error
	node.privKeyForAddrErr = tErr
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.privKeyForAddrErr = nil

	// bad coin
	badCoin := &tCoin{id: make([]byte, 15)}
	_, _, err = wallet.SignMessage(badCoin, msg)
	if err == nil {
		t.Fatalf("no error for bad coin")
	}
}

func TestAuditContract(t *testing.T) {
	runRubric(t, testAuditContract)
}

func testAuditContract(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	swapVal := toSatoshi(5)
	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	lockTime := time.Now().Add(time.Hour * 12)
	now := time.Now()
	addr, _ := btcutil.DecodeAddress(tP2PKHAddr, &chaincfg.MainNetParams)
	if segwit {
		addr, _ = btcutil.DecodeAddress(tP2WPKHAddr, &chaincfg.MainNetParams)
	}

	contract, err := dexbtc.MakeContract(addr, addr, secretHash, lockTime.Unix(), segwit, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	var contractAddr btcutil.Address
	if segwit {
		h := sha256.Sum256(contract)
		contractAddr, _ = btcutil.NewAddressWitnessScriptHash(h[:], &chaincfg.MainNetParams)
	} else {
		contractAddr, _ = btcutil.NewAddressScriptHash(contract, &chaincfg.MainNetParams)
	}
	pkScript, _ := txscript.PayToAddrScript(contractAddr)

	// Prime a blockchain
	const tipHeight = 10
	const txBlockHeight = 9
	for i := int64(1); i < tipHeight; i++ {
		node.addRawTx(i, dummyTx())
	}

	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	blockHash, _ := node.addRawTx(txBlockHeight, tx)
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript} //spv
	node.txOutRes = &btcjson.GetTxOutResult{                // rpc
		Confirmations: 2,
		Value:         float64(swapVal) / 1e8,
		ScriptPubKey: btcjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}
	node.getTransactionErr = WalletTransactionNotFound

	txHash := tx.TxHash()
	const vout = 0
	outPt := newOutPoint(&txHash, vout)

	audit, err := wallet.AuditContract(toCoinID(&txHash, vout), contract, nil, now)
	if err != nil {
		t.Fatalf("audit error: %v", err)
	}
	if audit.Recipient != addr.String() {
		t.Fatalf("wrong recipient. wanted '%s', got '%s'", addr, audit.Recipient)
	}
	if !bytes.Equal(audit.Contract, contract) {
		t.Fatalf("contract not set to coin redeem script")
	}
	if audit.Expiration.Equal(lockTime) {
		t.Fatalf("wrong lock time. wanted %d, got %d", lockTime.Unix(), audit.Expiration.Unix())
	}

	// Invalid txid
	_, err = wallet.AuditContract(make([]byte, 15), contract, nil, now)
	if err == nil {
		t.Fatalf("no error for bad txid")
	}

	// GetTxOut error
	node.txOutErr = tErr
	delete(node.getCFilterScripts, *blockHash)
	delete(node.checkpoints, outPt)
	_, err = wallet.AuditContract(toCoinID(&txHash, vout), contract, nil, now)
	if err == nil {
		t.Fatalf("no error for unknown txout")
	}
	node.txOutErr = nil
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}

	// Wrong contract
	pkh, _ := hex.DecodeString("c6a704f11af6cbee8738ff19fc28cdc70aba0b82")
	wrongAddr, _ := btcutil.NewAddressPubKeyHash(pkh, &chaincfg.MainNetParams)
	badContract, _ := txscript.PayToAddrScript(wrongAddr)
	_, err = wallet.AuditContract(toCoinID(&txHash, vout), badContract, nil, now)
	if err == nil {
		t.Fatalf("no error for wrong contract")
	}
}

type tReceipt struct {
	coin       *tCoin
	contract   []byte
	expiration uint64
}

func (r *tReceipt) Expiration() time.Time { return time.Unix(int64(r.expiration), 0).UTC() }
func (r *tReceipt) Coin() asset.Coin      { return r.coin }
func (r *tReceipt) Contract() dex.Bytes   { return r.contract }

func TestFindRedemption(t *testing.T) {
	runRubric(t, testFindRedemption)
}

func testFindRedemption(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	contractHeight := node.GetBestBlockHeight() + 1
	otherTxid := "7a7b3b5c3638516bc8e7f19b4a3dec00f052a599fed5036c2b89829de2367bb6"
	otherTxHash, _ := chainhash.NewHashFromStr(otherTxid)
	contractVout := uint32(1)

	secret, _, pkScript, contract, addr, contractAddr, _ := makeSwapContract(segwit, time.Hour*12)
	otherScript, _ := txscript.PayToAddrScript(addr)

	var redemptionWitness, otherWitness [][]byte
	var redemptionSigScript, otherSigScript []byte
	if segwit {
		redemptionWitness = dexbtc.RedeemP2WSHContract(contract, randBytes(73), randBytes(33), secret)
		otherWitness = [][]byte{randBytes(73), randBytes(33)}
	} else {
		redemptionSigScript, _ = dexbtc.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
		otherSigScript, _ = txscript.NewScriptBuilder().
			AddData(randBytes(73)).
			AddData(randBytes(33)).
			Script()
	}

	// Prepare the "blockchain"
	inputs := []*wire.TxIn{makeRPCVin(otherTxHash, 0, otherSigScript, otherWitness)}
	// Add the contract transaction. Put the pay-to-contract script at index 1.
	contractTx := makeRawTx([]dex.Bytes{otherScript, pkScript}, inputs)
	contractTxHash := contractTx.TxHash()
	coinID := toCoinID(&contractTxHash, contractVout)
	blockHash, _ := node.addRawTx(contractHeight, contractTx)
	txHex, err := makeTxHex([]dex.Bytes{otherScript, pkScript}, inputs)
	if err != nil {
		t.Fatalf("error generating hex for contract tx: %v", err)
	}
	getTxRes := &GetTransactionResult{
		BlockHash:  blockHash.String(),
		BlockIndex: contractHeight,
		Details: []*WalletTxDetails{
			{
				Address:  contractAddr.String(),
				Category: TxCatSend,
				Vout:     contractVout,
			},
		},
		Hex: txHex,
	}
	node.getTransaction = getTxRes

	// Add an intermediate block for good measure.
	node.addRawTx(contractHeight+1, makeRawTx([]dex.Bytes{otherScript}, inputs))

	// Now add the redemption.
	redeemVin := makeRPCVin(&contractTxHash, contractVout, redemptionSigScript, redemptionWitness)
	inputs = append(inputs, redeemVin)
	redeemBlockHash, _ := node.addRawTx(contractHeight+2, makeRawTx([]dex.Bytes{otherScript}, inputs))
	node.getCFilterScripts[*redeemBlockHash] = [][]byte{pkScript}

	// Update currentTip from "RPC". Normally run() would do this.
	wallet.reportNewTip(tCtx, &block{
		hash:   *redeemBlockHash,
		height: contractHeight + 2,
	})

	// Check find redemption result.
	_, checkSecret, err := wallet.FindRedemption(tCtx, coinID)
	if err != nil {
		t.Fatalf("error finding redemption: %v", err)
	}
	if !bytes.Equal(checkSecret, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, checkSecret)
	}

	// gettransaction error
	node.getTransactionErr = tErr
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for gettransaction rpc error")
	}
	node.getTransactionErr = nil

	// timeout finding missing redemption
	redeemVin.PreviousOutPoint.Hash = *otherTxHash
	delete(node.getCFilterScripts, *redeemBlockHash)
	timedCtx, cancel := context.WithTimeout(tCtx, 500*time.Millisecond) // 0.5 seconds is long enough
	defer cancel()
	_, k, err := wallet.FindRedemption(timedCtx, coinID)
	if timedCtx.Err() == nil || k != nil {
		// Expected ctx to cancel after timeout and no secret should be found.
		t.Fatalf("unexpected result for missing redemption: secret: %v, err: %v", k, err)
	}

	node.blockchainMtx.Lock()
	redeemVin.PreviousOutPoint.Hash = contractTxHash
	node.getCFilterScripts[*redeemBlockHash] = [][]byte{pkScript}
	node.blockchainMtx.Unlock()

	// Canceled context
	deadCtx, cancelCtx := context.WithCancel(tCtx)
	cancelCtx()
	_, _, err = wallet.FindRedemption(deadCtx, coinID)
	if err == nil {
		t.Fatalf("no error for canceled context")
	}

	// Expect FindRedemption to error because of bad input sig.
	node.blockchainMtx.Lock()
	redeemVin.Witness = [][]byte{randBytes(100)}
	redeemVin.SignatureScript = randBytes(100)

	node.blockchainMtx.Unlock()
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for wrong redemption")
	}
	node.blockchainMtx.Lock()
	redeemVin.Witness = redemptionWitness
	redeemVin.SignatureScript = redemptionSigScript
	node.blockchainMtx.Unlock()

	// Sanity check to make sure it passes again.
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err != nil {
		t.Fatalf("error after clearing errors: %v", err)
	}
}

func TestRefund(t *testing.T) {
	runRubric(t, testRefund)
}

func testRefund(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	_, _, pkScript, contract, addr, _, _ := makeSwapContract(segwit, time.Hour*12)

	bigTxOut := newTxOutResult(nil, 1e8, 2)
	node.txOutRes = bigTxOut // rpc
	node.changeAddr = addr.String()
	const feeSuggestion = 100

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}
	node.privKeyForAddr = wif

	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	const vout = 0
	tx.TxOut[vout].Value = 1e8
	txHash := tx.TxHash()
	outPt := newOutPoint(&txHash, vout)
	blockHash, _ := node.addRawTx(1, tx)
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	node.getTransactionErr = WalletTransactionNotFound

	contractOutput := newOutput(&txHash, 0, 1e8)
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Invalid coin
	badReceipt := &tReceipt{
		coin: &tCoin{id: make([]byte, 15)},
	}
	_, err = wallet.Refund(badReceipt.coin.id, badReceipt.Contract(), feeSuggestion)
	if err == nil {
		t.Fatalf("no error for bad receipt")
	}

	ensureErr := func(tag string) {
		delete(node.checkpoints, outPt)
		_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
		if err == nil {
			t.Fatalf("no error for %q", tag)
		}
	}

	// gettxout error
	node.txOutErr = tErr
	node.getCFilterScripts[*blockHash] = nil
	ensureErr("no utxo")
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	node.txOutErr = nil

	// bad contract
	badContractOutput := newOutput(tTxHash, 0, 1e8)
	badContract := randBytes(50)
	_, err = wallet.Refund(badContractOutput.ID(), badContract, feeSuggestion)
	if err == nil {
		t.Fatalf("no error for bad contract")
	}

	// Too small.
	node.txOutRes = newTxOutResult(nil, 100, 2)
	tx.TxOut[0].Value = 2
	ensureErr("value < fees")
	node.txOutRes = bigTxOut
	tx.TxOut[0].Value = 1e8

	// getrawchangeaddress error
	node.changeAddrErr = tErr
	ensureErr("getchangeaddress error")
	node.changeAddrErr = nil

	// signature error
	node.privKeyForAddrErr = tErr
	ensureErr("dumpprivkey error")
	node.privKeyForAddrErr = nil

	// send error
	node.sendErr = tErr
	ensureErr("send error")
	node.sendErr = nil

	// bad checkhash
	var badHash chainhash.Hash
	badHash[0] = 0x05
	node.badSendHash = &badHash
	ensureErr("checkhash error")
	node.badSendHash = nil

	// Sanity check that we can succeed again.
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err != nil {
		t.Fatalf("re-refund error: %v", err)
	}

	// TODO test spv spent
}

func TestLockUnlock(t *testing.T) {
	runRubric(t, testLockUnlock)
}

func testLockUnlock(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	pw := []byte("pass")

	// just checking that the errors come through.
	err = wallet.Unlock(pw)
	if err != nil {
		t.Fatalf("unlock error: %v", err)
	}
	node.unlockErr = tErr
	err = wallet.Unlock(pw)
	if err == nil {
		t.Fatalf("no error for walletpassphrase error")
	}

	// Locking can't error on SPV.
	if walletType == walletTypeRPC {
		err = wallet.Lock()
		if err != nil {
			t.Fatalf("lock error: %v", err)
		}
		node.lockErr = tErr
		err = wallet.Lock()
		if err == nil {
			t.Fatalf("no error for walletlock rpc error")
		}
	}

}

type tSenderType byte

const (
	tPayFeeSender tSenderType = iota
	tWithdrawSender
)

func testSender(t *testing.T, senderType tSenderType, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	const feeSuggestion = 100
	sender := func(addr string, val uint64) (asset.Coin, error) {
		return wallet.PayFee(addr, val, defaultFee)
	}
	if senderType == tWithdrawSender {
		sender = func(addr string, val uint64) (asset.Coin, error) {
			return wallet.Withdraw(addr, val, feeSuggestion)
		}
	}
	addr := btcAddr(segwit)
	fee := float64(1) // BTC
	node.setTxFee = true
	node.changeAddr = btcAddr(segwit).String()

	pkScript, _ := txscript.PayToAddrScript(addr)
	tx := makeRawTx([]dex.Bytes{randBytes(5), pkScript}, []*wire.TxIn{dummyInput()})
	txHash := tx.TxHash()
	const vout = 1
	const blockHeight = 2
	blockHash, _ := node.addRawTx(blockHeight, tx)

	txB, _ := serializeMsgTx(tx)

	node.sendToAddress = txHash.String()
	node.getTransaction = &GetTransactionResult{
		BlockHash:  blockHash.String(),
		BlockIndex: blockHeight,
		Hex:        txB,
	}

	unspents := []*ListUnspentResult{{
		TxID:          txHash.String(),
		Address:       addr.String(),
		Amount:        100,
		Confirmations: 1,
		Vout:          vout,
		ScriptPubKey:  pkScript,
		Safe:          true,
		Spendable:     true,
	}}
	node.listUnspent = unspents

	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}

	_, err = sender(addr.String(), toSatoshi(fee))
	if err != nil {
		t.Fatalf("send error: %v", err)
	}

	// SendToAddress error
	node.sendToAddressErr = tErr
	_, err = sender(addr.String(), 1e8)
	if err == nil {
		t.Fatalf("no error for SendToAddress error: %v", err)
	}
	node.sendToAddressErr = nil

	// GetTransaction error
	node.getTransactionErr = tErr
	_, err = sender(addr.String(), 1e8)
	if err == nil {
		t.Fatalf("no error for gettransaction error: %v", err)
	}
	node.getTransactionErr = nil

	// good again
	_, err = sender(addr.String(), toSatoshi(fee))
	if err != nil {
		t.Fatalf("PayFee error afterwards: %v", err)
	}
}

func TestPayFee(t *testing.T) {
	runRubric(t, func(t *testing.T, segwit bool, walletType string) {
		testSender(t, tPayFeeSender, segwit, walletType)
	})
}

func TestWithdraw(t *testing.T) {
	runRubric(t, func(t *testing.T, segwit bool, walletType string) {
		testSender(t, tWithdrawSender, segwit, walletType)
	})
}

func TestConfirmations(t *testing.T) {
	runRubric(t, testConfirmations)
}

func testConfirmations(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	// coinID := make([]byte, 36)
	// copy(coinID[:32], tTxHash[:])

	_, _, pkScript, contract, _, _, _ := makeSwapContract(segwit, time.Hour*12)
	const tipHeight = 10
	const swapHeight = 2
	const spendHeight = 4
	const expConfs = tipHeight - swapHeight + 1

	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	blockHash, swapBlock := node.addRawTx(swapHeight, tx)
	txHash := tx.TxHash()
	coinID := toCoinID(&txHash, 0)
	// Simulate a spending transaction, and advance the tip so that the swap
	// has two confirmations.
	spendingTx := dummyTx()
	spendingTx.TxIn[0].PreviousOutPoint.Hash = txHash
	spendingBlockHash, _ := node.addRawTx(spendHeight, spendingTx)

	// Prime the blockchain
	for i := int64(1); i <= tipHeight; i++ {
		node.addRawTx(i, dummyTx())
	}

	matchTime := swapBlock.Header.Timestamp

	// Bad coin id
	_, _, err = wallet.SwapConfirmations(context.Background(), randBytes(35), contract, matchTime)
	if err == nil {
		t.Fatalf("no error for bad coin ID")
	}

	// Short path.
	txOutRes := &btcjson.GetTxOutResult{
		Confirmations: expConfs,
		BestBlock:     blockHash.String(),
	}
	node.txOutRes = txOutRes
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	confs, _, err := wallet.SwapConfirmations(context.Background(), coinID, contract, matchTime)
	if err != nil {
		t.Fatalf("error for gettransaction path: %v", err)
	}
	if confs != expConfs {
		t.Fatalf("confs not retrieved from gettxout path. expected %d, got %d", expConfs, confs)
	}

	// no tx output found
	node.txOutRes = nil
	node.getCFilterScripts[*blockHash] = nil
	node.getTransactionErr = tErr
	_, _, err = wallet.SwapConfirmations(context.Background(), coinID, contract, matchTime)
	if err == nil {
		t.Fatalf("no error for gettransaction error")
	}
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	node.getTransactionErr = nil
	txB, _ := serializeMsgTx(tx)
	node.getTransaction = &GetTransactionResult{
		BlockHash: blockHash.String(),
		Hex:       txB,
	}

	node.getCFilterScripts[*spendingBlockHash] = [][]byte{pkScript}
	node.walletTxSpent = true
	_, spent, err := wallet.SwapConfirmations(context.Background(), coinID, contract, matchTime)
	if err != nil {
		t.Fatalf("error for spent swap: %v", err)
	}
	if !spent {
		t.Fatalf("swap not spent")
	}
}

func TestSendEdges(t *testing.T) {
	runRubric(t, testSendEdges)
}

func testSendEdges(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	const feeRate uint64 = 3

	const swapVal = 2e8 // leaving untyped. NewTxOut wants int64

	var addr, contractAddr btcutil.Address
	var dexReqFees, dustCoverage uint64

	addr = btcAddr(segwit)

	if segwit {
		contractAddr, _ = btcutil.NewAddressWitnessScriptHash(randBytes(32), &chaincfg.MainNetParams)
		// See dexbtc.IsDust for the source of this dustCoverage voodoo.
		dustCoverage = (dexbtc.P2WPKHOutputSize + 41 + (107 / 4)) * feeRate * 3
		dexReqFees = dexbtc.InitTxSizeSegwit * feeRate
	} else {
		contractAddr, _ = btcutil.NewAddressScriptHash(randBytes(20), &chaincfg.MainNetParams)
		dustCoverage = (dexbtc.P2PKHOutputSize + 41 + 107) * feeRate * 3
		dexReqFees = dexbtc.InitTxSize * feeRate
	}

	pkScript, _ := txscript.PayToAddrScript(contractAddr)

	newBaseTx := func() *wire.MsgTx {
		baseTx := wire.NewMsgTx(wire.TxVersion)
		baseTx.AddTxIn(wire.NewTxIn(new(wire.OutPoint), nil, nil))
		baseTx.AddTxOut(wire.NewTxOut(swapVal, pkScript))
		return baseTx
	}

	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}

	tests := []struct {
		name      string
		funding   uint64
		expChange bool
	}{
		{
			name:    "not enough for change output",
			funding: swapVal + dexReqFees - 1,
		},
		{
			// Still dust here, but a different path.
			name:    "exactly enough for change output",
			funding: swapVal + dexReqFees,
		},
		{
			name:    "more than enough for change output but still dust",
			funding: swapVal + dexReqFees + 1,
		},
		{
			name:    "1 atom short to not be dust",
			funding: swapVal + dexReqFees + dustCoverage - 1,
		},
		{
			name:      "exactly enough to not be dust",
			funding:   swapVal + dexReqFees + dustCoverage,
			expChange: true,
		},
	}

	for _, tt := range tests {
		tx, err := wallet.sendWithReturn(newBaseTx(), addr, tt.funding, swapVal, feeRate)
		if err != nil {
			t.Fatalf("sendWithReturn error: %v", err)
		}

		if len(tx.TxOut) == 1 && tt.expChange {
			t.Fatalf("%s: no change added", tt.name)
		} else if len(tx.TxOut) == 2 && !tt.expChange {
			t.Fatalf("%s: change output added for dust. Output value = %d", tt.name, tx.TxOut[1].Value)
		}
	}
}

func TestSyncStatus(t *testing.T) {
	runRubric(t, testSyncStatus)
}

func testSyncStatus(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	node.getBlockchainInfo = &getBlockchainInfoResult{
		Headers: 100,
		Blocks:  99,
	}

	synced, progress, err := wallet.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (synced expected): %v", err)
	}
	if !synced {
		t.Fatalf("synced = false for 1 block to go")
	}
	if progress < 1 {
		t.Fatalf("progress not complete when loading last block")
	}

	node.getBlockchainInfoErr = tErr // rpc
	node.getBestBlockHashErr = tErr  // spv
	_, _, err = wallet.SyncStatus()
	if err == nil {
		t.Fatalf("SyncStatus error not propagated")
	}
	node.getBlockchainInfoErr = nil
	node.getBestBlockHashErr = nil

	wallet.tipAtConnect = 100
	node.getBlockchainInfo = &getBlockchainInfoResult{
		Headers: 200,
		Blocks:  150,
	}
	node.addRawTx(150, makeRawTx([]dex.Bytes{randBytes(1)}, []*wire.TxIn{dummyInput()})) // spv needs this for BestBlock
	synced, progress, err = wallet.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (half-synced): %v", err)
	}
	if synced {
		t.Fatalf("synced = true for 50 blocks to go")
	}
	if progress > 0.500001 || progress < 0.4999999 {
		t.Fatalf("progress out of range. Expected 0.5, got %.2f", progress)
	}
}

func TestPreSwap(t *testing.T) {
	runRubric(t, testPreSwap)
}

func testPreSwap(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, err := tNewWallet(segwit, walletType)
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	// See math from TestFundEdges. 10 lots with max fee rate of 34 sats/vbyte.

	swapVal := uint64(1e7)
	lots := swapVal / tLotSize // 10 lots

	// var swapSize = 225
	var totalBytes uint64 = 2250
	var bestCaseBytes uint64 = 513
	pkScript := tP2PKH
	if segwit {
		// swapSize = 153
		totalBytes = 1530
		bestCaseBytes = 540
		pkScript = tP2WPKH
	}

	backingFees := totalBytes * tBTC.MaxFeeRate // total_bytes * fee_rate

	minReq := swapVal + backingFees

	unspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2PKHAddr,
		Confirmations: 5,
		ScriptPubKey:  pkScript,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents := []*ListUnspentResult{unspent}

	setFunds := func(v uint64) {
		unspent.Amount = float64(v) / 1e8
		node.listUnspent = unspents
	}

	form := &asset.PreSwapForm{
		LotSize:       tLotSize,
		Lots:          lots,
		AssetConfig:   tBTC,
		Immediate:     false,
		FeeSuggestion: feeSuggestion,
	}

	setFunds(minReq)

	// Initial success.
	preSwap, err := wallet.PreSwap(form)
	if err != nil {
		t.Fatalf("PreSwap error: %v", err)
	}

	maxFees := totalBytes * tBTC.MaxFeeRate
	estHighFees := totalBytes * feeSuggestion
	estLowFees := bestCaseBytes * feeSuggestion
	checkSwapEstimate(t, preSwap.Estimate, lots, swapVal, maxFees, estHighFees, estLowFees, minReq)

	// Too little funding is an error.
	setFunds(minReq - 1)
	_, err = wallet.PreSwap(form)
	if err == nil {
		t.Fatalf("no PreSwap error for not enough funds")
	}
	setFunds(minReq)

	// Success again.
	_, err = wallet.PreSwap(form)
	if err != nil {
		t.Fatalf("PreSwap error: %v", err)
	}
}

func TestPreRedeem(t *testing.T) {
	runRubric(t, testPreRedeem)
}

func testPreRedeem(t *testing.T, segwit bool, walletType string) {
	wallet, _, shutdown, _ := tNewWallet(segwit, walletType)
	defer shutdown()

	preRedeem, err := wallet.PreRedeem(&asset.PreRedeemForm{
		LotSize: 123456, // Doesn't actually matter
		Lots:    5,
	})
	// Shouldn't actually be any path to error.
	if err != nil {
		t.Fatalf("PreRedeem non-segwit error: %v", err)
	}

	// Just a couple of sanity checks.
	if preRedeem.Estimate.RealisticBestCase >= preRedeem.Estimate.RealisticWorstCase {
		t.Fatalf("best case > worst case")
	}
}

func TestTryRedemptionRequests(t *testing.T) {
	// runRubric(t, testTryRedemptionRequests)
	testTryRedemptionRequests(t, true, walletTypeSPV)
}

func testTryRedemptionRequests(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown, _ := tNewWallet(segwit, walletType)
	defer shutdown()

	const swapVout = 1

	randHash := func() *chainhash.Hash {
		var h chainhash.Hash
		copy(h[:], randBytes(32))
		return &h
	}

	otherScript, _ := txscript.PayToAddrScript(btcAddr(segwit))
	otherInput := []*wire.TxIn{makeRPCVin(randHash(), 0, randBytes(5), nil)}
	otherTx := func() *wire.MsgTx {
		return makeRawTx([]dex.Bytes{otherScript}, otherInput)
	}

	addBlocks := func(n int) {
		var h int64 = 0
		// Make dummy transactions.
		for i := 0; i < n; i++ {
			node.addRawTx(h, otherTx())
			h++
		}
	}

	getTx := func(blockHeight int64, txIdx int) (*wire.MsgTx, *chainhash.Hash) {
		if blockHeight == -1 {
			// mempool
			txHash := randHash()
			tx := otherTx()
			node.mempoolTxs[*txHash] = tx
			return tx, nil
		}
		blockHash, blk := node.getBlockAtHeight(blockHeight)
		for len(blk.msgBlock.Transactions) <= txIdx {
			blk.msgBlock.Transactions = append(blk.msgBlock.Transactions, otherTx())
		}
		return blk.msgBlock.Transactions[txIdx], blockHash
	}

	type tRedeem struct {
		redeemTxIdx, redeemVin        int
		swapHeight, redeemBlockHeight int64
		notRedeemed                   bool
	}

	redeemReq := func(r *tRedeem) *findRedemptionReq {
		var swapBlockHash *chainhash.Hash
		var swapHeight int64
		if r.swapHeight >= 0 {
			swapHeight = r.swapHeight
			swapBlockHash, _ = node.getBlockAtHeight(swapHeight)
		}

		swapTxHash := randHash()
		secret, _, pkScript, contract, _, _, _ := makeSwapContract(segwit, time.Hour*12)

		if !r.notRedeemed {
			redeemTx, redeemBlockHash := getTx(r.redeemBlockHeight, r.redeemTxIdx)

			// redemptionSigScript, _ := dexbtc.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
			for len(redeemTx.TxIn) < r.redeemVin {
				redeemTx.TxIn = append(redeemTx.TxIn, makeRPCVin(randHash(), 0, nil, nil))
			}

			var redemptionSigScript []byte
			var redemptionWitness [][]byte
			if segwit {
				redemptionWitness = dexbtc.RedeemP2WSHContract(contract, randBytes(73), randBytes(33), secret)
			} else {
				redemptionSigScript, _ = dexbtc.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
			}

			redeemTx.TxIn = append(redeemTx.TxIn, makeRPCVin(swapTxHash, swapVout, redemptionSigScript, redemptionWitness))
			if redeemBlockHash != nil {
				node.getCFilterScripts[*redeemBlockHash] = [][]byte{pkScript}
			}
		}

		req := &findRedemptionReq{
			outPt:        newOutPoint(swapTxHash, swapVout),
			blockHash:    swapBlockHash,
			blockHeight:  int32(swapHeight),
			resultChan:   make(chan *findRedemptionResult, 1),
			pkScript:     pkScript,
			contractHash: hashContract(segwit, contract),
		}
		wallet.findRedemptionQueue[req.outPt] = req
		return req
	}

	type test struct {
		numBlocks        int
		startBlockHeight int64
		redeems          []*tRedeem
		forcedErr        bool
		canceledCtx      bool
	}

	isMempoolTest := func(tt *test) bool {
		for _, r := range tt.redeems {
			if r.redeemBlockHeight == -1 {
				return true
			}
		}
		return false
	}

	tests := []*test{
		{ // Normal redemption
			numBlocks: 2,
			redeems: []*tRedeem{{
				redeemBlockHeight: 1,
				redeemTxIdx:       1,
				redeemVin:         1,
			}},
		},
		{ // Mempool redemption
			numBlocks: 2,
			redeems: []*tRedeem{{
				redeemBlockHeight: -1,
				redeemTxIdx:       2,
				redeemVin:         2,
			}},
		},
		{ // A couple of redemptions, both in tip.
			numBlocks:        3,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				redeemBlockHeight: 2,
			}, {
				redeemBlockHeight: 2,
			}},
		},
		{ // A couple of redemptions, spread apart.
			numBlocks: 6,
			redeems: []*tRedeem{{
				redeemBlockHeight: 2,
			}, {
				redeemBlockHeight: 4,
			}},
		},
		{ // nil start block
			numBlocks:        5,
			startBlockHeight: -1,
			redeems: []*tRedeem{{
				swapHeight:        4,
				redeemBlockHeight: 4,
			}},
		},
		{ // A mix of mined and mempool redeems
			numBlocks:        3,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				redeemBlockHeight: -1,
			}, {
				redeemBlockHeight: 2,
			}},
		},
		{ // One found, one not found.
			numBlocks:        4,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				redeemBlockHeight: 2,
			}, {
				notRedeemed: true,
			}},
		},
		{ // One found in mempool, one not found.
			numBlocks:        4,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				redeemBlockHeight: -1,
			}, {
				notRedeemed: true,
			}},
		},
		{ // Swap not mined.
			numBlocks:        3,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				swapHeight:        -1,
				redeemBlockHeight: -1,
			}},
		},
		{ // Fatal error
			numBlocks: 2,
			forcedErr: true,
			redeems: []*tRedeem{{
				redeemBlockHeight: 1,
			}},
		},
		{ // Canceled context.
			numBlocks:   2,
			canceledCtx: true,
			redeems: []*tRedeem{{
				redeemBlockHeight: 1,
			}},
		},
	}

	for _, tt := range tests {
		// Skip tests where we're expected to see mempool in SPV.
		if walletType == walletTypeSPV && isMempoolTest(tt) {
			continue
		}

		node.truncateChains()
		wallet.findRedemptionQueue = make(map[outPoint]*findRedemptionReq)
		node.getBestBlockHashErr = nil
		if tt.forcedErr {
			node.getBestBlockHashErr = tErr
		}
		addBlocks(tt.numBlocks)
		var startBlock *chainhash.Hash
		if tt.startBlockHeight >= 0 {
			startBlock, _ = node.getBlockAtHeight(tt.startBlockHeight)
		}

		ctx := tCtx
		if tt.canceledCtx {
			timedCtx, cancel := context.WithTimeout(tCtx, time.Second)
			ctx = timedCtx
			cancel()
		}

		reqs := make([]*findRedemptionReq, 0, len(tt.redeems))
		for _, redeem := range tt.redeems {
			reqs = append(reqs, redeemReq(redeem))
		}

		wallet.tryRedemptionRequests(ctx, startBlock, reqs)

		for i, req := range reqs {
			select {
			case res := <-req.resultChan:
				if res.err != nil {
					if !tt.forcedErr {
						t.Fatalf("result error: %v", res.err)
					}
				} else if tt.canceledCtx {
					t.Fatalf("got success with canceled context")
				}
			default:
				redeem := tt.redeems[i]
				if !redeem.notRedeemed && !tt.canceledCtx {
					t.Fatalf("redemption not found")
				}
			}
		}
	}

}
