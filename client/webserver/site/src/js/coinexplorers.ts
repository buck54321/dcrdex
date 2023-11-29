import {
  app,
  PageElement
} from './registry'
import * as intl from './locales'

export const Mainnet = 0
export const Testnet = 1
export const Simnet = 2

const coinIDTakerFoundMakerRedemption = 'TakerFoundMakerRedemption:'

/* ethBasedExplorerArg returns the explorer argument for ETH, ERC20 and EVM
Compatible assets and whether the return value is an address. */
function ethBasedExplorerArg (cid: string): [string, boolean] {
  if (cid.startsWith(coinIDTakerFoundMakerRedemption)) return [cid.substring(coinIDTakerFoundMakerRedemption.length), true]
  else if (cid.length === 42) return [cid, true]
  else return [cid, false]
}

const ethExplorers: Record<number, (cid: string) => string> = {
  [Mainnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://etherscan.io/address/${arg}` : `https://etherscan.io/tx/${arg}`
  },
  [Testnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://goerli.etherscan.io/address/${arg}` : `https://goerli.etherscan.io/tx/${arg}`
  },
  [Simnet]: (cid: string) => {
    const [arg, isAddr] = ethBasedExplorerArg(cid)
    return isAddr ? `https://etherscan.io/address/${arg}` : `https://etherscan.io/tx/${arg}`
  }
}

export const CoinExplorers: Record<number, Record<number, (cid: string) => string>> = {
  42: { // dcr
    [Mainnet]: (cid: string) => {
      const [txid, vout] = cid.split(':')
      if (vout !== undefined) return `https://explorer.dcrdata.org/tx/${txid}/out/${vout}`
      return `https://explorer.dcrdata.org/tx/${txid}`
    },
    [Testnet]: (cid: string) => {
      const [txid, vout] = cid.split(':')
      if (vout !== undefined) return `https://testnet.dcrdata.org/tx/${txid}/out/${vout}`
      return `https://testnet.dcrdata.org/tx/${txid}`
    },
    [Simnet]: (cid: string) => {
      const [txid, vout] = cid.split(':')
      if (vout !== undefined) return `http://127.0.0.1:17779/tx/${txid}/out/${vout}`
      return `https://127.0.0.1:17779/tx/${txid}`
    }
  },
  0: { // btc
    [Mainnet]: (cid: string) => `https://mempool.space/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://mempool.space/testnet/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://mempool.space/tx/${cid.split(':')[0]}`
  },
  2: { // ltc
    [Mainnet]: (cid: string) => `https://ltc.bitaps.com/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://sochain.com/tx/LTCTEST/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://ltc.bitaps.com/${cid.split(':')[0]}`
  },
  20: {
    [Mainnet]: (cid: string) => `https://digiexplorer.info/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://testnetexplorer.digibyteservers.io/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://digiexplorer.info/tx/${cid.split(':')[0]}`
  },
  60: ethExplorers,
  60001: ethExplorers,
  3: { // doge
    [Mainnet]: (cid: string) => `https://dogeblocks.com/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://blockexplorer.one/dogecoin/testnet/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://dogeblocks.com/tx/${cid.split(':')[0]}`
  },
  5: { // dash
    [Mainnet]: (cid: string) => `https://blockexplorer.one/dash/mainnet/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://blockexplorer.one/dash/testnet/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://blockexplorer.one/dash/mainnet/tx/${cid.split(':')[0]}`
  },
  133: { // zec
    [Mainnet]: (cid: string) => `https://zcashblockexplorer.com/transactions/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://blockexplorer.one/zcash/testnet/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://zcashblockexplorer.com/transactions/${cid.split(':')[0]}`
  },
  147: { // zcl
    [Mainnet]: (cid: string) => `https://explorer.zcl.zelcore.io/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://explorer.zcl.zelcore.io/tx/${cid.split(':')[0]}`
  },
  136: { // firo
    [Mainnet]: (cid: string) => `https://explorer.firo.org/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://testexplorer.firo.org/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://explorer.firo.org/tx/${cid.split(':')[0]}`
  },
  145: { // bch
    [Mainnet]: (cid: string) => `https://bch.loping.net/tx/${cid.split(':')[0]}`,
    [Testnet]: (cid: string) => `https://tbch4.loping.net/tx/${cid.split(':')[0]}`,
    [Simnet]: (cid: string) => `https://bch.loping.net/tx/${cid.split(':')[0]}`
  },
  966: { // matic
    [Mainnet]: (cid: string) => {
      const [arg, isAddr] = ethBasedExplorerArg(cid)
      return isAddr ? `https://polygonscan.com/address/${arg}` : `https://polygonscan.com/tx/${arg}`
    },
    [Testnet]: (cid: string) => {
      const [arg, isAddr] = ethBasedExplorerArg(cid)
      return isAddr ? `https://mumbai.polygonscan.com/address/${arg}` : `https://mumbai.polygonscan.com/tx/${arg}`
    },
    [Simnet]: (cid: string) => {
      const [arg, isAddr] = ethBasedExplorerArg(cid)
      return isAddr ? `https://polygonscan.com/address/${arg}` : `https://polygonscan.com/tx/${arg}`
    }
  }
}

export function formatCoinID (cid: string) {
  if (cid.startsWith(coinIDTakerFoundMakerRedemption)) {
    const makerAddr = cid.substring(coinIDTakerFoundMakerRedemption.length)
    return intl.prep(intl.ID_TAKER_FOUND_MAKER_REDEMPTION, { makerAddr: makerAddr })
  }
  return cid
}

/*
 * baseChainID returns the asset ID for the asset's parent if the asset is a
 * token, otherwise the ID for the asset itself.
 */
function baseChainID (assetID: number) {
  const asset = app().user.assets[assetID]
  return asset.token ? asset.token.parentID : assetID
}

/*
 * setCoinHref sets the hyperlink element's href attribute based on provided
 * assetID and data-explorer-coin value present on supplied link element.
 */
export function setCoinHref (assetID: number, link: PageElement) {
  // setCoinHref may get called from `richNote()` earlier than the user
  // data is fetched, therefore we need to check if the user
  // object is available.
  // If it is not then we return early.  This does no harm except
  // the tx hash in the notification will not be clickable.
  // The order details screen is unaffected because user data
  // is guaranteed to be available at the time of rendering.
  if (!app().user) return
  const net = app().user.net
  const assetExplorer = CoinExplorers[baseChainID(assetID)]
  if (!assetExplorer) return
  const formatter = assetExplorer[net]
  if (!formatter) return
  link.classList.remove('plainlink')
  link.classList.add('subtlelink')
  link.href = formatter(link.dataset.explorerCoin || '')
}
