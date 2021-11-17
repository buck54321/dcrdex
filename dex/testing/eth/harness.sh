#!/usr/bin/env bash
# tmux script that sets up an eth simnet harness. It sets up four separate nodes.
# alpha and beta nodes are synced in snap mode. They emulate nodes used by the
# dcrdex server. Either has the authority to mine blocks. They start with
# pre-allocated funds. gamma and delta are synced in light mode and emulate
# nodes used by dexc. They are sent some funds after being created. The harness
# waits for all nodes to sync before allowing tmux input.
set -ex

SESSION="eth-harness"

CHAIN_ADDRESS_JSON_FILE_NAME="UTC--2021-01-27T08-20-38.123221057Z--9ebba10a6136607688ca4f27fab70e23938cd027"
CHAIN_ADDRESS="9ebba10a6136607688ca4f27fab70e23938cd027"
CHAIN_ADDRESS_JSON='{"address":"9ebba10a6136607688ca4f27fab70e23938cd027","crypto":{"cipher":"aes-128-ctr","ciphertext":"dcfbe17de6f315c732855111b782496d76b2d703169afddaaa69e1bc9e02ec51","cipherparams":{"iv":"907e5e050649d1c5c0be782ec7db5cf1"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"060f4e16d601069a6bccae0693a15cd72090baf1ab20e408c89883117d4f7c51"},"mac":"b9ca7dad75a04b77dc7751a814c051f32752603334e4bb4046caf927196a5579"},"id":"74805e39-6a2f-46eb-8125-70c41d12c6d9","version":3}'

ALPHA_ADDRESS="18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-28T08-47-02.993754951Z--18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON='{"address":"18d65fb8d60c1199bb1ad381be47aa692b482605","crypto":{"cipher":"aes-128-ctr","ciphertext":"927bc2432492fc4bbe9acfe0042f5cd2cef25aff251ac1fb2f420ee85e3b6ee4","cipherparams":{"iv":"89e7333535aed5284abd52f841d30c95"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"6fe29ea59d166989be533da62d79802a6b0cef26a9766fa363c7a4bb4c263b5f"},"mac":"c7e2b6c4538c373b2c4e0be7b343db618d39cc68fa872909059357ff36743ca0"},"id":"0e2b9cef-d659-4a26-8739-879129ed0b63","version":3}'
ALPHA_NODE_KEY="71d810d39333296b518c846a3e49eca55f998fd7994998bb3e5048567f2f073c"
# ALPHA_ENODE="897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f"
ALPHA_NODE_PORT="30304"

# BETA_ADDRESS="4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06"
BETA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-27T08-20-58.179642501Z--4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06"
BETA_ADDRESS_JSON='{"address":"4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06","crypto":{"cipher":"aes-128-ctr","ciphertext":"c5672bb829df9e209ca8ce18dbdd1fed69c603d639e06ab09127b672a609c121","cipherparams":{"iv":"24460eb2934c8b61cee3ad0aa7b843c0"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"1f85da881994ca7b4a23f0698da70500a4b79f97a4450b83b129ebf3b4c28f50"},"mac":"1ecea707f1bffa1f6f944cb47e83118d8179e8a5005b83c88610b7e8692a1197"},"id":"56633762-6fb1-4cbf-8396-3a2e4661f7d4","version":3}'
BETA_NODE_KEY="0f3f23a0f14202da009bd59a96457098acea901986629e54d5be1eea32fc404a"
BETA_ENODE="b1d3e358ee5c9b268e911f2cab47bc12d0e65c80a6d2b453fece34facc9ac3caed14aa3bc7578166bb08c5bc9719e5a2267ae14e0b42da393f4d86f6d5829061"
BETA_NODE_PORT="30305"

GAMMA_ADDRESS="41293c2032bac60aa747374e966f79f575d42379"
GAMMA_ADDRESS_JSON_FILE_NAME="UTC--2021-03-01T02-12-42.714340074Z--41293c2032bac60aa747374e966f79f575d42379"
GAMMA_ADDRESS_JSON='{"address":"41293c2032bac60aa747374e966f79f575d42379","crypto":{"cipher":"aes-128-ctr","ciphertext":"5191719067513511b07d959de1a86cd37c3f7011dce75f62c791114c3a62b15b","cipherparams":{"iv":"cdfcd9e475f2af7df08a8a36cc0de976"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"5630591da82b8517f1b8f61719fbb552e41f25861cc20bc4671a11a47b427d31"},"mac":"d13259851d78deb70d1273ab151d4a12583b94f5cbdf31d86f02bb549d241d36"},"id":"235ba177-e32c-4d23-8d94-a57bc04b97ca","version":3}'
GAMMA_NODE_KEY="9e102b8ba8cad4c6b9db6c881915d3f1bb206e76113266bf48266de0474844fd"
GAMMA_ENODE="b1c14deee09b9d5549c90b7b30a35c812a56bf6afea5873b05d7a1bcd79c7b0848bcfa982faf80cc9e758a3a0d9b470f0a002840d365050fd5bf45052a6ec313"
GAMMA_NODE_PORT="30306"

DELTA_ADDRESS="d12ab7cf72ccf1f3882ec99ddc53cd415635c3be"
DELTA_ADDRESS_JSON_FILE_NAME="UTC--2021-03-01T02-31-13.365402148Z--d12ab7cf72ccf1f3882ec99ddc53cd415635c3be"
DELTA_ADDRESS_JSON='{"address":"d12ab7cf72ccf1f3882ec99ddc53cd415635c3be","crypto":{"cipher":"aes-128-ctr","ciphertext":"a0e9a3da5d0c88c922b5d7e817693552fe17dfd4c598e2a8b08ee53a706a8ffc","cipherparams":{"iv":"28b0a443403b7a02001f07a35724f6e6"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"f790f584bf396cacc06f28201aa697825011e84f570759d6108e20c5ee4fffce"},"mac":"529318e5eec2474221912d01e5a534a0b1dbfb19499ffaf942be6375611caa83"},"id":"d8670e33-8094-45b7-9386-d936e6bf4c1b","version":3}'
DELTA_NODE_KEY="725394672587b34bbf15580c59e5199c75c2c7e998ba8df3cb38cc4347d46e2b"
DELTA_ENODE="ca414c361d1a38716170923e4900d9dc9203dbaf8fdcaee73e1f861df9fdf20a1453b76fd218c18bc6f3c7e13cbca0b3416af02a53b8e31188faa45aab398d1c"
DELTA_NODE_PORT="30307"

# TESTING_ADDRESS is used by the client's internal node.
TESTING_ADDRESS="b6de8bb5ed28e6be6d671975cad20c03931be981"

ETH_SWAP_V0="608060405234801561001057600080fd5b5061093d806100206000396000f3fe6080604052600436106100705760003560e01c8063a8793f941161004e578063a8793f9414610108578063b31597ad1461011b578063eb84e7f21461013b578063ef74e594146101b557600080fd5b80637249fbb61461007557806376467cbd1461009757806385d5fde1146100cd575b600080fd5b34801561008157600080fd5b50610095610090366004610730565b6101e2565b005b3480156100a357600080fd5b506100b76100b2366004610730565b6102e4565b6040516100c491906107e1565b60405180910390f35b3480156100d957600080fd5b506100fa6100e8366004610730565b60009081526001602052604090205490565b6040519081526020016100c4565b6100956101163660046106bb565b6103bc565b34801561012757600080fd5b50610095610136366004610762565b6104e8565b34801561014757600080fd5b506101a2610156366004610730565b60006020819052908152604090208054600182015460028301546003840154600485015460058601546006909601549495939492936001600160a01b0392831693929091169160ff1687565b6040516100c49796959493929190610846565b3480156101c157600080fd5b506100fa6101d0366004610730565b60016020526000908152604090205481565b3233146101ee57600080fd5b80600160008281526020819052604090206006015460ff166003811115610217576102176108db565b1461022157600080fd5b6000818152602081905260409020600301546001600160a01b0316331461024757600080fd5b6000818152602081905260409020600101544281111561026657600080fd5b60008381526020819052604080822060068101805460ff191660031790556005015490513391908381818185875af1925050503d80600081146102c5576040519150601f19603f3d011682016040523d82523d6000602084013e6102ca565b606091505b50909150506001811515146102de57600080fd5b50505050565b6103216040805160e081018252600080825260208201819052918101829052606081018290526080810182905260a081018290529060c082015290565b60008281526020818152604091829020825160e081018452815481526001820154928101929092526002810154928201929092526003808301546001600160a01b0390811660608401526004840154166080830152600583015460a0830152600683015491929160c084019160ff909116908111156103a2576103a26108db565b60038111156103b3576103b36108db565b90525092915050565b3233146103c857600080fd5b6000805b828110156104d657368484838181106103e7576103e76108f1565b90506080020190506000806000836020013581526020019081526020016000209050600082606001351161041a57600080fd5b813561042557600080fd5b6000600682015460ff166003811115610440576104406108db565b1461044a57600080fd5b438155813560018201556003810180546001600160a01b03191633179055610478606083016040840161068b565b6004820180546001600160a01b0319166001600160a01b039290921691909117905560608201356005820181905560068201805460ff191660011790556104bf9085610892565b9350505080806104ce906108aa565b9150506103cc565b503481146104e357600080fd5b505050565b3233146104f457600080fd5b8082600160008381526020819052604090206006015460ff16600381111561051e5761051e6108db565b1461052857600080fd5b8160028260405160200161053e91815260200190565b60408051601f1981840301815290829052610558916107a6565b602060405180830381855afa158015610575573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906105989190610749565b146105a257600080fd5b60008381526020819052604080822060068101805460ff19166002179055600481015460059091015491516001600160a01b0390911691908381818185875af1925050503d8060008114610612576040519150601f19603f3d011682016040523d82523d6000602084013e610617565b606091505b509091505060018115151461062b57600080fd5b5050506000908152602081815260408083208381556001808201859055600282018590556003820180546001600160a01b03199081169091556004830180549091169055600582018590556006909101805460ff19169055909152902055565b60006020828403121561069d57600080fd5b81356001600160a01b03811681146106b457600080fd5b9392505050565b600080602083850312156106ce57600080fd5b823567ffffffffffffffff808211156106e657600080fd5b818501915085601f8301126106fa57600080fd5b81358181111561070957600080fd5b8660208260071b850101111561071e57600080fd5b60209290920196919550909350505050565b60006020828403121561074257600080fd5b5035919050565b60006020828403121561075b57600080fd5b5051919050565b6000806040838503121561077557600080fd5b50508035926020909101359150565b600481106107a257634e487b7160e01b600052602160045260246000fd5b9052565b6000825160005b818110156107c757602081860181015185830152016107ad565b818111156107d6576000828501525b509190910192915050565b600060e082019050825182526020830151602083015260408301516040830152606083015160018060a01b038082166060850152806080860151166080850152505060a083015160a083015260c083015161083f60c0840182610784565b5092915050565b87815260208101879052604081018690526001600160a01b0385811660608301528416608082015260a0810183905260e0810161088660c0830184610784565b98975050505050505050565b600082198211156108a5576108a56108c5565b500190565b60006000198214156108be576108be6108c5565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052602160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea264697066735822122053f600858cd326533938b4bd6573e0513544061f6274fdc182cbebba78e33a2964736f6c63430008060033"

# PASSWORD is the password used to unlock all accounts/wallets/addresses.
PASSWORD="abc"

export NODES_ROOT=~/dextest/eth
export GENESIS_JSON_FILE_LOCATION="${NODES_ROOT}/genesis.json"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

mkdir -p "${NODES_ROOT}/alpha"
mkdir -p "${NODES_ROOT}/beta"
mkdir -p "${NODES_ROOT}/harness-ctl"

echo "Writing ctl scripts"
################################################################################
# Control Scripts
################################################################################

# Write genesis json. ".*Block" fields represent block height where certain
# protocols take effect. "clique" is our proof of authority scheme. One block
# can be mined per second with a signature belonging to the address in
# "extradata". The addresses in the "alloc" field are allocated "balance".
# Values are in wei. 1*10^18 wei is equal to one eth. Addresses are allocated
# 11,000 eth. The addresses belong to alpha and beta nodes and two others are
# used in tests.
cat > "${NODES_ROOT}/genesis.json" <<EOF
{
  "config": {
    "chainId": 42,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0,
    "clique": {
      "period": 1,
      "epoch": 30000
    }
  },
  "difficulty": "1",
  "gasLimit": "12487783",
  "extradata": "0x00000000000000000000000000000000000000000000000000000000000000009ebba10a6136607688ca4f27fab70e23938cd0270000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
  "alloc": {
    "18d65fb8d60c1199bb1ad381be47aa692b482605": {
        "balance": "26000000000000000000000"
    },
    "4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06": {
        "balance": "11000000000000000000000"
    },
    "dd93b447f7eBCA361805eBe056259853F3912E04": {
        "balance": "11000000000000000000000"
    },
    "8d83B207674bfd53B418a6E47DA148F5bFeCc652": {
        "balance": "11000000000000000000000"
    }
  }
}
EOF

cat > "${NODES_ROOT}/harness-ctl/send.js" <<EOF
function send(from, to, amt) {
  personal.sendTransaction({from:"0x"+from,to:"0x"+to,value:amt,gasPrice:82000000000}, "${PASSWORD}")
  return true;
}
EOF

cat > "${NODES_ROOT}/harness-ctl/deploy.js" <<EOF
function deploy(from, contract) {
  tx = personal.sendTransaction({from:"0x"+from,data:"0x"+contract,gasPrice:82000000000}, "${PASSWORD}")
  return tx;
}
EOF

cat > "${NODES_ROOT}/harness-ctl/contractAddress.js" <<EOF
function contractAddress(tx) {
  addr = eth.getTransactionReceipt(tx).contractAddress
  return addr;
}
EOF

# Add node script.
HARNESS_DIR=$(dirname "$0")
cp "${HARNESS_DIR}/create-node.sh" "${NODES_ROOT}/harness-ctl/create-node"

# Reorg script
# TODO: Make this.

# Shutdown script
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:1 C-c
tmux send-keys -t $SESSION:2 C-c
tmux kill-session
EOF
chmod +x "${NODES_ROOT}/harness-ctl/quit"

################################################################################
# Start harness
################################################################################

echo "Starting harness"
tmux new-session -d -s $SESSION "${SHELL}"
tmux rename-window -t $SESSION:0 'harness-ctl'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${NODES_ROOT}/harness-ctl" C-m

################################################################################
# Eth nodes
################################################################################

echo "Starting simnet alpha node"
"${HARNESS_DIR}/create-node.sh" "$SESSION:1" "alpha" "$ALPHA_NODE_PORT" \
	"$CHAIN_ADDRESS" "$PASSWORD" "$CHAIN_ADDRESS_JSON" \
	"$CHAIN_ADDRESS_JSON_FILE_NAME" "$ALPHA_ADDRESS_JSON" "$ALPHA_ADDRESS_JSON_FILE_NAME" \
	"$ALPHA_NODE_KEY" "snap"

echo "Starting simnet beta node"
"${HARNESS_DIR}/create-node.sh" "$SESSION:2" "beta" "$BETA_NODE_PORT" \
	"$CHAIN_ADDRESS" "$PASSWORD" "$CHAIN_ADDRESS_JSON" \
	"$CHAIN_ADDRESS_JSON_FILE_NAME" "$BETA_ADDRESS_JSON" "$BETA_ADDRESS_JSON_FILE_NAME" \
	"$BETA_NODE_KEY" "snap"

echo "Starting simnet gamma node"
"${HARNESS_DIR}/create-node.sh" "$SESSION:3" "gamma" "$GAMMA_NODE_PORT" \
	"_" "_" "_" "_" "$GAMMA_ADDRESS_JSON" "$GAMMA_ADDRESS_JSON_FILE_NAME" \
	"$GAMMA_NODE_KEY" "light"

echo "Starting simnet delta node"
"${HARNESS_DIR}/create-node.sh" "$SESSION:4" "delta" "$DELTA_NODE_PORT" \
	"_" "_" "_" "_" "$DELTA_ADDRESS_JSON" "$DELTA_ADDRESS_JSON_FILE_NAME" \
	"$DELTA_NODE_KEY" "light"

sleep 1

# NOTE: Connecting a node will add for both. Also, light nodes take longer to
# set up. They will show 0 peers for some amount of time even after adding here.
echo "Connecting nodes"
"${NODES_ROOT}/harness-ctl/alpha" "attach --exec admin.addPeer('enode://${BETA_ENODE}@127.0.0.1:$BETA_NODE_PORT')"
"${NODES_ROOT}/harness-ctl/alpha" "attach --exec admin.addPeer('enode://${GAMMA_ENODE}@127.0.0.1:$GAMMA_NODE_PORT')"
"${NODES_ROOT}/harness-ctl/alpha" "attach --exec admin.addPeer('enode://${DELTA_ENODE}@127.0.0.1:$DELTA_NODE_PORT')"
"${NODES_ROOT}/harness-ctl/beta" "attach --exec admin.addPeer('enode://${GAMMA_ENODE}@127.0.0.1:$GAMMA_NODE_PORT')"
"${NODES_ROOT}/harness-ctl/beta" "attach --exec admin.addPeer('enode://${DELTA_ENODE}@127.0.0.1:$DELTA_NODE_PORT')"
"${NODES_ROOT}/harness-ctl/gamma" "attach --exec admin.addPeer('enode://${DELTA_ENODE}@127.0.0.1:$DELTA_NODE_PORT')"

echo "Mining some blocks"
"${NODES_ROOT}/harness-ctl/mine-beta" "2"
"${NODES_ROOT}/harness-ctl/mine-alpha" "2"

SEND_AMT=5000000000000000000000
echo "Sending 5000 eth to delta and gamma and testing."
"${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"${ALPHA_ADDRESS}\",\"${GAMMA_ADDRESS}\",${SEND_AMT})"
"${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"${ALPHA_ADDRESS}\",\"${DELTA_ADDRESS}\",${SEND_AMT})"
"${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"${ALPHA_ADDRESS}\",\"${TESTING_ADDRESS}\",${SEND_AMT})"

echo "Deploying ETHSwapV0 contract."
CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deploy(\"${ALPHA_ADDRESS}\",\"${ETH_SWAP_V0}\")" | sed 's/"//g')

# Initial sync for light nodes takes quite a while. Wait for them to show
# blocks on the network.
while true
do
  N=$("${NODES_ROOT}/harness-ctl/gamma" "attach --exec eth.blockNumber")
  if [ "$N" -gt 0 ]; then
    break
  fi
  echo "Waiting for light nodes to sync."
  # Although not necessary here, mine while waiting so that transactions are
  # mined if not mined yet.
  "${NODES_ROOT}/harness-ctl/mine-beta" "5"
done

while true
do
  TXSLEN=$("${NODES_ROOT}/harness-ctl/alpha" "attach --exec eth.pendingTransactions.length")
  if [ "$TXSLEN" -eq 0 ]; then
    break
  fi
  echo "Waiting for transactions to be mined."
  "${NODES_ROOT}/harness-ctl/mine-beta" "5"
done

CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${CONTRACT_HASH}\")" | sed 's/"//g')
echo "Contract address is ${CONTRACT_ADDR}. Saving to ${NODES_ROOT}/contract_hash.txt"
cat > "${NODES_ROOT}/contract_addr.txt" <<EOF
${CONTRACT_ADDR}
EOF

# Reenable history and attach to the control session.
tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
