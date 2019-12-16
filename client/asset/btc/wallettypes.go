// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import "decred.org/dcrdex/dex"

// ListUnspentResult models a successful response from the listunspent request.
type ListUnspentResult struct {
	TxID          string    `json:"txid"`
	Vout          uint32    `json:"vout"`
	Address       string    `json:"address"`
	Label         string    `json:"label"`
	ScriptPubKey  dex.Bytes `json:"scriptPubKey"`
	Amount        float64   `json:"amount"`
	Confirmations uint32    `json:"confirmations"`
	RedeemScript  dex.Bytes `json:"redeemScript"`
	Spendable     bool      `json:"spendable"`
	Solvable      bool      `json:"solvable"`
	Safe          bool      `json:"safe"`
}

// SignTxResult models the data from the signrawtransaction command.
type SignTxResult struct {
	Hex      dex.Bytes      `json:"hex"`
	Complete bool           `json:"complete"`
	Errors   []*SignTxError `json:"errors"`
}

// SignTxError models the data that contains script verification errors from the
// signrawtransaction request
type SignTxError struct {
	TxID      string    `json:"txid"`
	Vout      uint32    `json:"vout"`
	ScriptSig dex.Bytes `json:"scriptSig"`
	Sequence  uint64    `json:"sequence"`
	Error     string    `json:"error"`
}

// GetTransactionResult models the data from the gettransaction command.
type GetTransactionResult struct {
	Amount         float64            `json:"amount"`
	Fee            float64            `json:"fee"`
	Confirmations  uint64             `json:"confirmations"`
	BlockHash      string             `json:"blockhash"`
	BlockIndex     int64              `json:"blockindex"`
	BlockTime      uint64             `json:"blocktime"`
	TxID           string             `json:"txid"`
	Time           uint64             `json:"time"`
	TimeReceived   uint64             `json:"timereceived"`
	BipReplaceable string             `json:"bip125-replaceable"`
	Hex            dex.Bytes          `json:"dex"`
	Details        []*WalletTxDetails `json:"details"`
}

// WalletTxCategory is the tx output category set in WalletTxDetails.
type WalletTxCategory string

const (
	TxCatSend     WalletTxCategory = "send"
	TxCatReceive  WalletTxCategory = "receive"
	TxCatGenerate WalletTxCategory = "generate"
	TxCatImmature WalletTxCategory = "immature"
	TxCatOrphan   WalletTxCategory = "orphan"
)

// WalletTxDetails models the details data from the gettransaction command.
type WalletTxDetails struct {
	Address   string           `json:"address"`
	Category  WalletTxCategory `json:"category"`
	Amount    float64          `json:"amount"`
	Label     string           `json:"label"`
	Vout      uint32           `json:"vout"`
	Fee       float64          `json:"fee"`
	Abandoned bool             `json:"abandoned"`
}

// RPCOutpoint is used to specify outputs to lock in calls to lockunspent.
type RPCOutpoint struct {
	TxID string `json:"txid"`
	Vout uint32 `json:"vout"`
}
