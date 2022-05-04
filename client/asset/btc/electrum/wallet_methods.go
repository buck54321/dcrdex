// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package electrum

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	methodCommands          = "commands" // list of supported methods
	methodGetInfo           = "getinfo"
	methodGetServers        = "getservers"
	methodGetFeeRate        = "getfeerate"
	methodCreateNewAddress  = "createnewaddress" // beyond gap limit
	methodGetUnusedAddress  = "getunusedaddress"
	methodGetAddressHistory = "getaddresshistory"
	methodGetAddressUnspent = "getaddressunspent"
	methodGetTransaction    = "gettransaction"
	methodListUnspent       = "listunspent"
	methodGetPrivateKeys    = "getprivatekeys"
	methodPayTo             = "payto"
	methodBroadcast         = "broadcast"
	methodGetTxStatus       = "get_tx_status" // only wallet txns
	methodGetBalance        = "getbalance"
	methodIsMine            = "ismine"
	methodValidateAddress   = "validateaddress"
	methodSignTransaction   = "signtransaction"
	methodFreeze            = "freeze" // freezes all utxos for an address, freeze_utxo not avail in 4.0.9
	methodUnfreeze          = "unfreeze"
	methodFreezeUTXO        = "freeze_utxo"
	methodUnfreezeUTXO      = "unfreeze_utxo"
)

// Commands gets a list of the supported wallet RPCs.
func (wc *WalletClient) Commands() ([]string, error) {
	var res string
	err := wc.Call(methodCommands, nil, &res)
	if err != nil {
		return nil, err
	}
	return strings.Split(res, " "), nil
}

// GetInfo gets basic Electrum wallet info.
func (wc *WalletClient) GetInfo() (*GetInfoResult, error) {
	var res GetInfoResult
	err := wc.Call(methodGetInfo, nil, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// GetServers gets the electrum servers known to the wallet. These are the
// possible servers to which Electrum may connect. This includes the currently
// connected server named in the GetInfo result.
func (wc *WalletClient) GetServers() ([]*GetServersResult, error) {
	type getServersResult struct {
		Pruning string `json:"pruning"` // oldest block or "-" for no pruning
		SSL     string `json:"s"`       // port, as a string for some reason
		TCP     string `json:"t"`
		Version string `json:"version"` // e.g. "1.4.2"
	}
	var res map[string]*getServersResult
	err := wc.Call(methodGetServers, nil, &res)
	if err != nil {
		return nil, err
	}

	servers := make([]*GetServersResult, 0, len(res))
	for host, info := range res {
		var ssl, tcp uint16
		if info.SSL != "" {
			sslP, err := strconv.ParseUint(info.SSL, 10, 16)
			if err == nil {
				ssl = uint16(sslP)
			} else {
				fmt.Println(err)
			}
		}
		if info.TCP != "" {
			tcpP, err := strconv.ParseUint(info.TCP, 10, 16)
			if err == nil {
				tcp = uint16(tcpP)
			} else {
				fmt.Println(err)
			}
		}
		servers = append(servers, &GetServersResult{
			Host:    host,
			Pruning: info.Pruning,
			SSL:     ssl,
			TCP:     tcp,
			Version: info.Version,
		})
	}

	return servers, nil
}

type feeRateReq struct {
	Method string  `json:"fee_method"`
	Level  float64 `json:"fee_level"`
}

// FeeRate gets a fee rate estimate for a block confirmation target, where 1
// indicates the next block.
func (wc *WalletClient) FeeRate(confTarget int64) (int64, error) {
	if confTarget > 10 {
		confTarget = 10
	} else if confTarget < 1 {
		confTarget = 1
	}

	// Based on the Electrum wallet UI:
	// "mempool": 1.0 corresponds to 0.1 MB from tip, 0.833 to 0.2 MB, 0.667 to 0.5 MB, 0.5 to 1.0 MB, 0.333 to 2 MB
	// "eta": 1.0 corresponds to "next block", 0.75 to "within 2 blocks", 0.5 to 5 blks, 0.25 to 10 blks (non-linear)
	target := map[int64]float64{1: 1.0, 2: 0.75, 3: 0.66, 4: 0.56, 5: 0.5,
		6: 0.445, 7: 0.39, 8: 0.333, 9: 0.278, 10: 0.25}[confTarget] // "eta", roughly interpolated

	var satPerKB int64
	err := wc.Call(methodGetFeeRate, feeRateReq{"eta", target}, &satPerKB) // or anylist{"mempool", target}
	if err != nil {
		return 0, err
	}
	return satPerKB, nil
}

// CreateNewAddress generates a new address, ignoring the gap limit. NOTE: There
// is no method to retrieve a change address.
func (wc *WalletClient) CreateNewAddress() (string, error) {
	var res string
	err := wc.Call(methodCreateNewAddress, nil, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}

// GetUnusedAddress gets the next unused address from the wallet. It may have
// already been requested.
func (wc *WalletClient) GetUnusedAddress() (string, error) {
	var res string
	err := wc.Call(methodGetUnusedAddress, nil, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}

// CheckAddress validates the address and reports if it belongs to the wallet.
func (wc *WalletClient) CheckAddress(addr string) (valid, mine bool, err error) {
	err = wc.Call(methodIsMine, positional{addr}, &mine)
	if err != nil {
		return
	}
	err = wc.Call(methodValidateAddress, positional{addr}, &valid)
	if err != nil {
		return
	}
	return
}

// GetAddressHistory returns the history an address. Confirmed transactions will
// have a nil Fee field, while unconfirmed transactions will have a Fee and a
// value of zero for Height.
func (wc *WalletClient) GetAddressHistory(addr string) ([]*GetAddressHistoryResult, error) {
	var res []*GetAddressHistoryResult
	err := wc.Call(methodGetAddressHistory, positional{addr}, &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// GetAddressUnspent returns the unspent outputs for an address. Unconfirmed
// outputs will have a value of zero for Height.
func (wc *WalletClient) GetAddressUnspent(addr string) ([]*GetAddressUnspentResult, error) {
	var res []*GetAddressUnspentResult
	err := wc.Call(methodGetAddressUnspent, positional{addr}, &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// FreezeAddress freezes/locks all UTXOs paying to the address.
func (wc *WalletClient) FreezeAddress(addr string) error {
	var res bool
	err := wc.Call(methodFreeze, positional{addr}, &res)
	if err != nil {
		return err
	}
	if !res {
		return fmt.Errorf("address not owned: %v", addr)
	}
	return nil
}

// UnfreezeAddress unfreezes/unlocks all UTXOs paying to the address.
func (wc *WalletClient) UnfreezeAddress(addr string) error {
	var res bool
	err := wc.Call(methodUnfreeze, positional{addr}, &res)
	if err != nil {
		return err
	}
	if !res {
		return fmt.Errorf("address not owned: %v", addr)
	}
	return nil
}

// FreezeUTXO freezes/locks a single UTXO. It will still be reported by
// listunspent while locked.
func (wc *WalletClient) FreezeUTXO(txid string, out uint32) error {
	utxo := txid + ":" + strconv.FormatUint(uint64(out), 10)
	var res bool
	err := wc.Call(methodFreezeUTXO, positional{utxo}, &res)
	if err != nil {
		return err
	}
	if !res { // always returns true in all forks I've checked
		return fmt.Errorf("wallet could not freeze utxo %v", utxo)
	}
	return nil
}

// UnfreezeUTXO unfreezes/unlocks a single UTXO.
func (wc *WalletClient) UnfreezeUTXO(txid string, out uint32) error {
	utxo := txid + ":" + strconv.FormatUint(uint64(out), 10)
	var res bool
	err := wc.Call(methodUnfreezeUTXO, positional{utxo}, &res)
	if err != nil {
		return err
	}
	if !res { // always returns true in all forks I've checked
		return fmt.Errorf("wallet could not unfreeze utxo %v", utxo)
	}
	return nil
}

// GetRawTransaction retrieves the serialized transaction identified by txid.
func (wc *WalletClient) GetRawTransaction(txid string) ([]byte, error) {
	var res string
	err := wc.Call(methodGetTransaction, positional{txid}, &res)
	if err != nil {
		return nil, err
	}
	tx, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// GetWalletTxConfs will get the confirmations on the wallet-related
// transaction. This function will error if it is either not a wallet
// transaction or not known to the wallet.
func (wc *WalletClient) GetWalletTxConfs(txid string) (int, error) {
	var res struct {
		Confs int `json:"confirmations"`
	}
	err := wc.Call(methodGetTxStatus, positional{txid}, &res)
	if err != nil {
		return 0, err
	}
	return res.Confs, nil
}

// ListUnspent returns details on all unspent outputs for the wallet. Note that
// the pkScript is not included, and the user would have to retrieve it with
// GetRawTransaction for PrevOutHash if the output is of interest.
func (wc *WalletClient) ListUnspent() ([]*ListUnspentResult, error) {
	var res []*ListUnspentResult
	err := wc.Call(methodListUnspent, nil, &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// GetBalance returns the result of the getbalance wallet RPC.
func (wc *WalletClient) GetBalance() (*Balance, error) {
	var res struct {
		Confirmed   floatString `json:"confirmed"`
		Unconfirmed floatString `json:"unconfirmed"`
		Immature    floatString `json:"unmatured"` // yes, unmatured!
	}
	err := wc.Call(methodGetBalance, nil, &res)
	if err != nil {
		return nil, err
	}
	return &Balance{
		Confirmed:   float64(res.Confirmed),
		Unconfirmed: float64(res.Unconfirmed),
		Immature:    float64(res.Immature),
	}, nil
}

// payto(self, destination, amount, fee=None, feerate=None, from_addr=None, from_coins=None, change_addr=None,
// nocheck=False, unsigned=False, rbf=None, password=None, locktime=None, addtransaction=False, wallet: Abstract_Wallet = None):
type paytoReq struct {
	Addr       string   `json:"destination"`
	Amount     string   `json:"amount"` // BTC, or "!" for max
	Fee        *float64 `json:"fee,omitempty"`
	FeeRate    *float64 `json:"feerate,omitempty"` // sat/vB, gets multiplied by 1000 for extra precision, omit for high prio
	ChangeAddr string   `json:"change_addr,omitempty"`
	// FromAddr omitted
	FromUTXOs      string `json:"from_coins,omitempty"`
	NoCheck        bool   `json:"nocheck"`
	Unsigned       bool   `json:"unsigned"` // unsigned returns a base64 psbt thing
	RBF            bool   `json:"rbf"`      // default to false
	Password       string `json:"password"`
	LockTime       *int64 `json:"locktime,omitempty"`
	AddTransaction bool   `json:"addtransaction"`
}

// PayTo sends the specified amount in BTC (or the conventional unit for the
// assets e.g. LTC) to an address using a certain fee rate. The transaction is
// not broadcasted; the raw bytes of the signed transaction are returned. After
// the caller verifies the transaction, it may be sent with Broadcast.
func (wc *WalletClient) PayTo(walletPass string, addr string, amtBTC float64, feeRate float64) ([]byte, error) {
	if feeRate < 1 {
		return nil, errors.New("fee rate in sat/vB too low")
	}
	amt := strconv.FormatFloat(amtBTC, 'f', 8, 64)
	var res string
	err := wc.Call(methodPayTo, &paytoReq{
		Addr:     addr,
		Amount:   amt,
		FeeRate:  &feeRate,
		Password: walletPass,
		// AddTransaction adds the transaction to Electrum as a "local" txn
		// before broadcasting. If we don't, rapid back-to-back sends can result
		// in a mempool conflict from spending the same prevouts.
		AddTransaction: true,
	}, &res)
	if err != nil {
		return nil, err
	}
	txRaw, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return txRaw, nil
}

// PayToFromAbsFee allows specifying prevouts (in txid:vout format) and an
// absolute fee in BTC instead of a fee rate. This combination allows specifying
// precisely how much will be withdrawn from the wallet (subtracting fees),
// unless the change is dust and omitted. The transaction is not broadcasted;
// the raw bytes of the signed transaction are returned. After the caller
// verifies the transaction, it may be sent with Broadcast.
func (wc *WalletClient) PayToFromCoinsAbsFee(walletPass string, fromCoins []string, addr string, amtBTC float64, absFee float64) ([]byte, error) {
	if absFee > 1 {
		return nil, errors.New("abs fee too high")
	}
	amt := strconv.FormatFloat(amtBTC, 'f', 8, 64)
	var res string
	err := wc.Call(methodPayTo, &paytoReq{
		Addr:           addr,
		Amount:         amt,
		Fee:            &absFee,
		Password:       walletPass,
		FromUTXOs:      strings.Join(fromCoins, ","),
		AddTransaction: true,
	}, &res)
	if err != nil {
		return nil, err
	}
	txRaw, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return txRaw, nil
}

// Sweep sends all available funds to an address with a specified fee rate. No
// change output is created. The transaction is not broadcasted; the raw bytes
// of the signed transaction are returned. After the caller verifies the
// transaction, it may be sent with Broadcast.
func (wc *WalletClient) Sweep(walletPass string, addr string, feeRate float64) ([]byte, error) {
	if feeRate < 1 {
		return nil, errors.New("fee rate in sat/vB too low")
	}
	var res string
	err := wc.Call(methodPayTo, &paytoReq{
		Addr:           addr,
		Amount:         "!", // special "max" indicator, creating no change output
		FeeRate:        &feeRate,
		Password:       walletPass,
		AddTransaction: true,
	}, &res)
	if err != nil {
		return nil, err
	}
	txRaw, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return txRaw, nil
}

type signTransactionArgs struct {
	Tx   string `json:"tx"`
	Pass string `json:"password"`
	// 4.0.9 has privkey in this request, but 4.2 does not since it has a
	// signtransaction_with_privkey request. (this RPC should not use positional
	// arguments)
	// Privkey string `json:"privkey,omitempty"` // sign with wallet if empty
}

// SignTx signs the base-64 encoded PSBT with the wallet's keys, returning the
// signed transaction.
func (wc *WalletClient) SignTx(walletPass string, psbtB64 string) ([]byte, error) {
	var res string
	err := wc.Call(methodSignTransaction, &signTransactionArgs{psbtB64, walletPass}, &res)
	if err != nil {
		return nil, err
	}
	txRaw, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return txRaw, nil
}

// Broadcast submits the transaction to the network.
func (wc *WalletClient) Broadcast(tx []byte) (string, error) {
	txStr := hex.EncodeToString(tx)
	var res string
	err := wc.Call(methodBroadcast, positional{txStr}, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}

// RemoveLocalTx is used to remove a "local" transaction from the Electrum
// wallet DB. This can only be done if the tx was not broadcasted. This is
// required if using a payTo method that added the local transaction but either
// it failed to broadcast or the user no longer wants to send it after
// inspecting the raw transaction.
func (wc *WalletClient) RemoveLocalTx(txid string) error {
	return wc.Call(methodBroadcast, positional{txid}, nil)
}

type getPrivKeyArgs struct {
	Addr string `json:"address"`
	Pass string `json:"password"`
}

// GetPrivateKeys uses the getprivatekeys RPC to retrieve the keys for a given
// address. The returned string is WIF-encoded.
func (wc *WalletClient) GetPrivateKeys(walletPass, addr string) (string, error) {
	var res string
	err := wc.Call(methodGetPrivateKeys, &getPrivKeyArgs{addr, walletPass}, &res)
	if err != nil {
		return "", err
	}
	privSpit := strings.Split(res, ":")
	if len(privSpit) != 2 {
		return "", errors.New("bad key")
	}
	return privSpit[1], nil
}
