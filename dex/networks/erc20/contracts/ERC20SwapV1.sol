// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.18;

// ERC20Swap creates a contract to be deployed on an ethereum network. In
// order to save on gas fees, a separate ERC20Swap contract is deployed
// for each ERC20 token. After deployed, it keeps a map of swaps that
// facilitates atomic swapping of ERC20 tokens with other crypto currencies
// that support time locks.
//
// It accomplishes this by holding tokens acquired during a swap initiation
// until conditions are met. Prior to initiating a swap, the initiator must
// approve the ERC20Swap contract to be able to spend the initiator's tokens.
// When calling initiate, the necessary tokens for swaps are transferred to
// the swap contract. At this point the funds belong to the contract, and
// cannot be accessed by anyone else, not even the contract's deployer. The
// initiator commits to the swap parameters, including a locktime after which
// the funds will be accessible for refund should they not be redeemed. The
// participant can redeem at any time after the initiation transaction is mined
// if they have the secret that hashes to the secret hash. Otherwise, the
// initiator can refund funds any time after the locktime.
//
// This contract has no limits on gas used for any transactions.
//
// This contract cannot be used by other contracts or by a third party mediating
// the swap or multisig wallets.
contract ERC20Swap {
    bytes4 private constant TRANSFER_FROM_SELECTOR = bytes4(keccak256("transferFrom(address,address,uint256)"));
    bytes4 private constant TRANSFER_SELECTOR = bytes4(keccak256("transfer(address,uint256)"));

    address public immutable token_address;

    // Step is a type that hold's a contract's current step. Empty is the
    // uninitiated or null value.
    enum Step { Empty, Filled, Redeemed, Refunded }

    struct Status {
        Step step;
        bytes32 secret;
        uint256 blockNumber;
    }

    bytes32 constant RefundRecord = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF;
    bytes32 constant RefundRecordHash = 0xAF9613760F72635FBDB44A5A0A63C39F12AF30F950A6EE5C971BE188E89C4051;

    // swaps is a map of contract hashes to the "swap record". The swap record
    // has the following interpretation.
    //   if (record == bytes32(0x00)): contract is uninitiated
    //   else if (uint256(record) < block.number && sha256(record) != contract.secretHash):
    //      contract is initiated and redeemable by the participant with the secret.
    //   else if (sha256(record) == contract.secretHash): contract has been redeemed
    //   else if (record == RefundRecord): contract has been refunded
    //   else: invalid record. Should be impossible by construction
    mapping(bytes32 => bytes32) public swaps;

    // Vector is the information necessary for initialization and redemption
    // or refund. The Vector itself is not stored on-chain. Instead, a key
    // unique to the Vector is generated from the Vector data and keys
    // the swap record.
    struct Vector {
        bytes32 secretHash;
        uint256 value;
        address initiator;
        uint64 refundTimestamp;
        address participant;
    }

    // contractKey generates a key hash which commits to the contract data. The
    // generated hash is used as a key in the swaps map.
    function contractKey(Vector calldata v) public pure returns (bytes32) {
        return sha256(bytes.concat(v.secretHash, bytes20(v.initiator), bytes20(v.participant), bytes32(v.value), bytes8(v.refundTimestamp)));
    }

    // Redemption is the information necessary to redeem a Vector. Since we
    // don't store the Vector itself, it must be provided as part of the
    // redemption.
    struct Redemption {
        Vector v;
        bytes32 secret;
    }

    function secretValidates(bytes32 secret, bytes32 secretHash) public pure returns (bool) {
        return sha256(bytes.concat(secret)) == secretHash;
    }

    constructor(address token) {
        token_address = token;
    }

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors.
    modifier senderIsOrigin() {
        require(tx.origin == msg.sender, "sender != origin");
        _;
    }

    // retrieveStatus retrieves the current swap record for the contract.
    function retrieveStatus(Vector calldata v)
        private view returns (bytes32, bytes32, uint256)
    {
        bytes32 k = contractKey(v);
        bytes32 record = swaps[k];
        return (k, record, uint256(record));
    }

    // state returns the current state of the swap.
    function status(Vector calldata v)
        public view returns(Status memory)
    {
        (, bytes32 record, uint256 blockNum) = retrieveStatus(v);
        Status memory r;
        if (blockNum == 0) {
            r.step = Step.Empty;
        } else if (record == RefundRecord) {
            r.step = Step.Refunded;
        } else if (secretValidates(record, v.secretHash)) {
            r.step = Step.Redeemed;
            r.secret = record;
        } else {
            r.step = Step.Filled;
            r.blockNumber =  blockNum;
        }
        return r;
    }

    // initiate initiates an array of Vectors.
    function initiate(Vector[] calldata contracts)
        public
        payable
        senderIsOrigin()
    {
        uint initVal = 0;
        for (uint i = 0; i < contracts.length; i++) {
            Vector calldata v = contracts[i];

            require(v.value > 0, "0 val");
            require(v.refundTimestamp > 0, "0 refundTimestamp");
            require(v.secretHash != RefundRecordHash, "illegal secret hash (refund record hash)");

            bytes32 k = contractKey(v);
            bytes32 record = swaps[k];
            require(record == bytes32(0), "swap not empty");

            record = bytes32(block.number);
            require(!secretValidates(record, v.secretHash), "hash collision");

            swaps[k] = record;

            initVal += v.value;
        }

        bool success;
        bytes memory data;
        (success, data) = token_address.call(abi.encodeWithSelector(TRANSFER_FROM_SELECTOR, msg.sender, address(this), initVal));
        require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer from failed');
    }

    // isRedeemable returns whether or not a swap identified by vector
    // can be redeemed using secret.
    function isRedeemable(Vector calldata v)
        public
        view
        returns (bool)
    {
        (, bytes32 record, uint256 blockNum) = retrieveStatus(v);
        return blockNum != 0 && !secretValidates(record, v.secretHash);
    }

    // redeem redeems a Vector. It checks that the sender is not a contract,
    // and that the secret hashes to secretHash. msg.value is tranfered
    // from ETHSwap to the sender.
    //
    // To prevent reentry attack, it is very important to check the state of the
    // contract first, and change the state before proceeding to send. That way,
    // the nested attacking function will throw upon trying to call redeem a
    // second time. Currently, reentry is also not possible because contracts
    // cannot use this contract.
    function redeem(Redemption[] calldata redemptions)
        public
        senderIsOrigin()
    {
        uint amountToRedeem = 0;
        for (uint i = 0; i < redemptions.length; i++) {
            Redemption calldata r = redemptions[i];

            require(r.v.participant == msg.sender, "not authed");

            (bytes32 k, bytes32 record, uint256 blockNum) = retrieveStatus(r.v);

            // To be redeemable, the record needs to represent a valid block
            // number.
            require(blockNum > 0 && blockNum < block.number, "unfilled swap");

            // Can't already be redeemed.
            require(!secretValidates(record, r.v.secretHash), "already redeemed");

            // Are they presenting the correct secret?
            require(secretValidates(r.secret, r.v.secretHash), "invalid secret");

            swaps[k] = r.secret;
            amountToRedeem += r.v.value;
        }

        bool success;
        bytes memory data;
        (success, data) = token_address.call(abi.encodeWithSelector(TRANSFER_SELECTOR, msg.sender, amountToRedeem));
        require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer failed');
    }


    // refund refunds a Vector. It checks that the sender is not a contract
    // and that the refund time has passed. msg.value is transfered from the
    // contract to the sender = Vector.participant.
    //
    // It is important to note that this also uses call.value which comes with
    // no restrictions on gas used. See redeem for more info.
    function refund(Vector calldata v)
        public
        senderIsOrigin()
    {
        // Is this contract even in a refundable state?
        require(block.timestamp >= v.refundTimestamp, "locktime not expired");

        // Retrieve the record.
        (bytes32 k, bytes32 record, uint256 blockNum) = retrieveStatus(v);

        // Is this swap initialized?
        // This check also guarantees that the swap has not already been
        // refunded i.e. record != RefundRecord, since RefundRecord is certainly
        // greater than block.number.
        require(blockNum > 0 && blockNum <= block.number, "swap not active");

        // Is it already redeemed?
        require(!secretValidates(record, v.secretHash), "swap already redeemed");

        swaps[k] = RefundRecord;

        bool success;
        bytes memory data;
        (success, data) = token_address.call(abi.encodeWithSelector(TRANSFER_SELECTOR, msg.sender, v.value));
        require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer failed');
    }
}
