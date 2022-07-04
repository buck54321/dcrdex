#!/usr/bin/env bash

# See https://github.com/spesmilo/electrum
# On Debian, may need to sudo apt-get install libsecp256k1-0 gettext

set -ex

SCRIPT_DIR=`pwd`
SYMBOL=btc
ASSET_DIR=~/dextest/electrum/${SYMBOL}
ELECTRUM_DIR=${ASSET_DIR}/client
REPO_DIR=${ELECTRUM_DIR}/electrum-repo
WALLET_DIR=${ELECTRUM_DIR}/wallet
NET_DIR=${WALLET_DIR}/regtest

rm -rf ${NET_DIR}/blockchain_headers ${NET_DIR}/forks ${NET_DIR}/certs ${NET_DIR}/wallets/default_wallet
mkdir -p ${NET_DIR}/regtest
mkdir -p ${NET_DIR}/wallets
mkdir -p ${REPO_DIR}

cd ${REPO_DIR}

if [ ! -d "${REPO_DIR}/.git" ]; then
    git init
    git remote add origin https://github.com/spesmilo/electrum.git
fi

git fetch --depth 1 origin 0fca35fa4068ea7c9da60cd5330b85c2ca1d0398
git reset --hard FETCH_HEAD

if [ ! -d "${ELECTRUM_DIR}/venv" ]; then
    python -m venv ${ELECTRUM_DIR}/venv
fi
source ${ELECTRUM_DIR}/venv/bin/activate
python -m ensurepip --upgrade
pip install .
pip install requests pycryptodomex pyqt5

./contrib/pull_locale # I think we need this?

cp ${SCRIPT_DIR}/electrum_default_wallet ${NET_DIR}/wallets/default_wallet

cat > "${NET_DIR}/config" <<EOF
{
    "auto_connect": false,
    "blockchain_preferred_block": {
        "hash": "0f9188f13cb7b2c71f2a335e3a4fc328bf5beb436012afca590b1a11466e2206",
        "height": 0
    },
    "check_updates": false,
    "config_version": 3,
    "decimal_point": 8,
    "dont_show_testnet_warning": true,
    "gui_last_wallet": "${NET_DIR}/wallets/default_wallet",
    "is_maximized": false,
    "oneserver": false,
    "recently_open": [
        "${NET_DIR}/wallets/default_wallet"
    ],
    "rpchost": "127.0.0.1",
    "rpcpassword": "pass",
    "rpcport": 6789,
    "rpcuser": "user",
    "server": "127.0.0.1:54002:s",
    "show_channels_tab": true
}
EOF

cat > "${ASSET_DIR}/client-config.ini" <<EOF
rpcuser=user
rpcpassword=pass
rpcbind=127.0.0.1:6789
EOF

./run_electrum --regtest --dir ${WALLET_DIR}