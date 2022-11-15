module decred.org/dcrdex/dex/testing/loadbot

go 1.18

replace decred.org/dcrdex => ../../../

require (
	decred.org/dcrdex v0.0.0-20220620230547-1283356d184b
	github.com/Shopify/toxiproxy/v2 v2.4.0
)

require (
	decred.org/cspp/v2 v2.0.0 // indirect
	decred.org/dcrwallet/v2 v2.0.8 // indirect
	github.com/AndreasBriese/bbloom v0.0.0-20190825152654-46b345b51c96 // indirect
	github.com/VictoriaMetrics/fastcache v1.10.0 // indirect
	github.com/aead/siphash v1.0.1 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/btcsuite/btcd v0.23.3 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.2.1 // indirect
	github.com/btcsuite/btcd/btcutil v1.1.2 // indirect
	github.com/btcsuite/btcd/btcutil/psbt v1.1.5 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.0.1 // indirect
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f // indirect
	github.com/btcsuite/btcwallet v0.16.1 // indirect
	github.com/btcsuite/btcwallet/wallet/txauthor v1.3.2 // indirect
	github.com/btcsuite/btcwallet/wallet/txrules v1.2.0 // indirect
	github.com/btcsuite/btcwallet/wallet/txsizes v1.2.3 // indirect
	github.com/btcsuite/btcwallet/walletdb v1.4.0 // indirect
	github.com/btcsuite/btcwallet/wtxmgr v1.5.0 // indirect
	github.com/btcsuite/go-socks v0.0.0-20170105172521-4720035b7bfd // indirect
	github.com/btcsuite/golangcrypto v0.0.0-20150304025918-53f62d9b43e8 // indirect
	github.com/btcsuite/websocket v0.0.0-20150119174127-31079b680792 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/companyzero/sntrup4591761 v0.0.0-20200131011700-2b0d299dbd22 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dchest/blake2b v1.0.0 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/dcrlabs/neutrino-bch v0.0.0-20221031001408-f296bfa9bd1c // indirect
	github.com/dcrlabs/neutrino-ltc v0.0.0-20221031001456-55ef06cefead // indirect
	github.com/deckarep/golang-set v1.8.0 // indirect
	github.com/decred/base58 v1.0.4 // indirect
	github.com/decred/dcrd/addrmgr/v2 v2.0.0 // indirect
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0 // indirect
	github.com/decred/dcrd/blockchain/standalone/v2 v2.1.0 // indirect
	github.com/decred/dcrd/blockchain/v4 v4.0.2 // indirect
	github.com/decred/dcrd/certgen v1.1.1 // indirect
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3 // indirect
	github.com/decred/dcrd/chaincfg/v3 v3.1.1 // indirect
	github.com/decred/dcrd/connmgr/v3 v3.1.0 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.0 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.1 // indirect
	github.com/decred/dcrd/database/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrec v1.0.0 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.1.0 // indirect
	github.com/decred/dcrd/dcrjson/v4 v4.0.0 // indirect
	github.com/decred/dcrd/dcrutil/v4 v4.0.0 // indirect
	github.com/decred/dcrd/gcs/v3 v3.0.0 // indirect
	github.com/decred/dcrd/hdkeychain/v3 v3.1.0 // indirect
	github.com/decred/dcrd/lru v1.1.1 // indirect
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0 // indirect
	github.com/decred/dcrd/rpcclient/v7 v7.0.0 // indirect
	github.com/decred/dcrd/txscript/v4 v4.0.0 // indirect
	github.com/decred/dcrd/wire v1.5.0 // indirect
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/decred/slog v1.2.0 // indirect
	github.com/dgraph-io/badger v1.6.2 // indirect
	github.com/dgraph-io/ristretto v0.0.4-0.20210318174700-74754f61e018 // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/edsrzf/mmap-go v1.1.0 // indirect
	github.com/ethereum/go-ethereum v1.10.24-0.20220902154041-90711efb0ab6 // indirect
	github.com/fjl/memsize v0.0.0-20190710130421-bcb5799ab5e5 // indirect
	github.com/gballet/go-libpcsclite v0.0.0-20191108122812-4678299bea08 // indirect
	github.com/gcash/bchd v0.19.0 // indirect
	github.com/gcash/bchlog v0.0.0-20180913005452-b4f036f92fa6 // indirect
	github.com/gcash/bchutil v0.0.0-20210113190856-6ea28dff4000 // indirect
	github.com/gcash/bchwallet v0.10.0 // indirect
	github.com/gcash/bchwallet/walletdb v0.0.0-20210524114850-4837f9798568 // indirect
	github.com/gcash/neutrino v0.0.0-20210524114821-3b1878290cf9 // indirect
	github.com/go-chi/chi/v5 v5.0.1 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/golang-jwt/jwt/v4 v4.4.3 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/go-cmp v0.5.8 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/hashicorp/go-bexpr v0.1.10 // indirect
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d // indirect
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/holiman/uint256 v1.2.0 // indirect
	github.com/huin/goupnp v1.0.3 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/jrick/bitset v1.0.0 // indirect
	github.com/jrick/logrotate v1.0.0 // indirect
	github.com/jrick/wsrpc/v2 v2.3.4 // indirect
	github.com/kkdai/bstream v1.0.0 // indirect
	github.com/klauspost/cpuid/v2 v2.1.0 // indirect
	github.com/lib/pq v1.10.4 // indirect
	github.com/lightninglabs/gozmq v0.0.0-20191113021534-d20a764486bf // indirect
	github.com/lightninglabs/neutrino v0.14.3-0.20221024182812-792af8548c14 // indirect
	github.com/lightningnetwork/lnd/clock v1.0.1 // indirect
	github.com/lightningnetwork/lnd/queue v1.0.1 // indirect
	github.com/lightningnetwork/lnd/ticker v1.0.0 // indirect
	github.com/lightningnetwork/lnd/tlv v1.0.2 // indirect
	github.com/ltcsuite/lnd/clock v0.0.0-20200822020009-1a001cbb895a // indirect
	github.com/ltcsuite/lnd/queue v1.0.3 // indirect
	github.com/ltcsuite/lnd/ticker v1.0.1 // indirect
	github.com/ltcsuite/ltcd v0.22.1-beta // indirect
	github.com/ltcsuite/ltcd/btcec/v2 v2.1.0 // indirect
	github.com/ltcsuite/ltcd/ltcutil v1.1.0 // indirect
	github.com/ltcsuite/ltcd/ltcutil/psbt v1.1.0-1 // indirect
	github.com/ltcsuite/ltcwallet v0.13.1 // indirect
	github.com/ltcsuite/ltcwallet/wallet/txauthor v1.1.0 // indirect
	github.com/ltcsuite/ltcwallet/wallet/txrules v1.2.0 // indirect
	github.com/ltcsuite/ltcwallet/wallet/txsizes v1.1.0 // indirect
	github.com/ltcsuite/ltcwallet/walletdb v1.3.5 // indirect
	github.com/ltcsuite/ltcwallet/wtxmgr v1.5.0 // indirect
	github.com/ltcsuite/neutrino v0.13.2 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mitchellh/pointerstructure v1.2.0 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/tsdb v0.10.0 // indirect
	github.com/rivo/uniseg v0.3.4 // indirect
	github.com/rjeczalik/notify v0.9.1 // indirect
	github.com/rs/cors v1.8.2 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/shirou/gopsutil v3.21.11+incompatible // indirect
	github.com/status-im/keycard-go v0.1.0 // indirect
	github.com/stretchr/testify v1.8.0 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20220721030215-126854af5e6d // indirect
	github.com/tklauser/go-sysconf v0.3.10 // indirect
	github.com/tklauser/numcpus v0.5.0 // indirect
	github.com/tyler-smith/go-bip39 v1.1.0 // indirect
	github.com/urfave/cli/v2 v2.17.2-0.20221006022127-8f469abc00aa // indirect
	github.com/xrash/smetrics v0.0.0-20201216005158-039620a65673 // indirect
	github.com/yusufpapurcu/wmi v1.2.2 // indirect
	github.com/zquestz/grab v0.0.0-20190224022517-abcee96e61b1 // indirect
	go.etcd.io/bbolt v1.3.7-0.20220130032806-d5db64bdbfde // indirect
	golang.org/x/crypto v0.4.0 // indirect
	golang.org/x/net v0.3.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/sys v0.3.0 // indirect
	golang.org/x/term v0.3.0 // indirect
	golang.org/x/text v0.5.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce // indirect
	lukechampine.com/blake3 v1.1.7 // indirect
)
