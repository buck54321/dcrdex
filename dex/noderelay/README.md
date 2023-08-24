# NodeRelay

NodeRelay is a system for connecting to nodes on remote private machines.
This is done through a WebSockets-based reverse tunnel.

1. Run a full node on a home or otherwise private server. If your node has
credentials for RPC requests (`rcpuser`, `rpcpassword`), give those to the server
operator.

2. The server operator will expose the NodeRelay, which is running on port
`17537`, through an external IP address or host name.

1. The server operator generates a **relay ID**, which can be any string without
whitespace. The **relay ID** itself acts as a password, so don't share it
widely. A good **relay ID** might look like `btc_0405f1069d352a0f`. Each asset
backend that will connect through NodeRelay will need its own **relay ID**.

1. The server operator modifies the asset configuration (the one at `configPath`)
to point to the NodeRelay. Use the special string `noderelay:btc_0405f1069d352a0f`
(substitute your **relay ID**) as the address. For Bitcoin, that means
setting `rpcbind=noderelay:btc_0405f1069d352a0f`. Set `rpcuser` and `rpcpassword`
from the private server in step 1.

1. The server operator starts `dcrdex`, passing in the relay IDs `--noderelay`
and external IP or domain (from step 2), with port if necessary `--noderelayaddr`.
    ```
    ./dcrdex --noderelay btc_0405f1069d352a0f --noderelayaddr myhost.tld:17537
    ```

1. Upon starting, the server will generate and store a **relayfile** to a
directory located by default at `~/.dcrdex/data/mainnet/noderelay/relay-files/`,
with a file name of e.g. `btc_0405f1069d352a0f.relayfile`. Send this file to
the private server.

1. On the private server, run `nodesource`, pointing at the relay file with
`--relayfile` and setting the node RPC port with `--port`.
    ```
    ./nodesource --relayfile btc_0405f1069d352a0f.relayfile --port 8332
    ```


