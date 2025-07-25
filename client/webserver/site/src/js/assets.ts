import Doc from './doc'
import {
  app,
  SupportedAsset,
  UnitInfo,
  WalletInfo
} from './registry'

export class ChainAsset {
  assetID: number
  symbol: string
  ui: UnitInfo
  chainName: string
  chainLogo: string
  ticker: string
  token?: {
    parentMade: boolean
    parentID: number
    feeUI: UnitInfo
  }

  constructor (a: SupportedAsset) {
    const { id: assetID, symbol, name, token, unitInfo: ui, unitInfo: { conventional: { unit: ticker } } } = a
    this.assetID = assetID
    this.ticker = ticker
    this.symbol = symbol
    this.ui = ui
    this.chainName = token ? app().assets[token.parentID].name : name
    this.chainLogo = token ? Doc.logoPath(app().assets[token.parentID].symbol) : Doc.logoPath(symbol)
    if (token) this.token = { parentID: token.parentID, feeUI: app().unitInfo(token.parentID), parentMade: Boolean(app().assets[token.parentID].wallet) }
  }

  get bal () {
    const w = app().assets[this.assetID].wallet
    return w?.balance ?? { available: 0, locked: 0, immature: 0 }
  }

  updateTokenParentMade () {
    if (!this.token) return false
    this.token.parentMade = Boolean(app().assets[this.token.parentID].wallet)
  }
}

export class TickerAsset {
  ticker: string // normalized e.g. WETH -> ETH
  hasWallets: boolean
  cFactor: number
  bestID: number
  logoSymbol: string
  logoSrc: string
  name: string
  chainAssets: ChainAsset[]
  chainAssetLookup: Record<number, ChainAsset>
  haveAllFiatRates: boolean
  isMultiNet: boolean
  hasTokens: boolean
  ui: UnitInfo

  constructor (a: SupportedAsset) {
    const { id: assetID, name, symbol, unitInfo: ui, unitInfo: { conventional: { conversionFactor: cFactor } } } = a
    this.ticker = normalizedTicker(a)
    this.cFactor = cFactor
    this.chainAssets = []
    this.chainAssetLookup = {}
    this.bestID = assetID
    this.name = name
    this.logoSymbol = symbol
    this.logoSrc = Doc.logoPath(symbol)
    this.ui = ui
    this.addChainAsset(a)
  }

  addChainAsset (a: SupportedAsset) {
    const { id: assetID, symbol, name, wallet: w, token, unitInfo: ui } = a
    const xcRate = app().fiatRatesMap[assetID]
    if (!xcRate) this.haveAllFiatRates = false
    this.hasTokens = this.hasTokens || Boolean(token)
    if (!token) { // prefer the native asset data, e.g. weth.polygon -> eth}
      this.bestID = assetID
      this.logoSymbol = symbol
      this.name = name
      this.ui = ui
    }
    const ca = new ChainAsset(a)
    this.hasWallets = this.hasWallets || Boolean(w) || Boolean(ca.token?.parentMade)
    this.chainAssets.push(ca)
    this.chainAssetLookup[a.id] = ca
    this.chainAssets.sort((a: ChainAsset, b: ChainAsset) => {
      if (a.token && !b.token) return 1
      if (!a.token && b.token) return -1
      return a.ticker.localeCompare(b.ticker)
    })
    this.isMultiNet = this.chainAssets.length > 1
  }

  walletInfo (): WalletInfo | undefined {
    for (const { assetID } of this.chainAssets) {
      const { info } = app().assets[assetID]
      if (info) return info
    }
  }

  updateHasWallets () {
    for (const ta of this.chainAssets) {
      ta.updateTokenParentMade()
      const { assetID, token } = ta
      if (app().walletMap[assetID] || token?.parentMade) {
        this.hasWallets = true
        return
      }
    }
  }

  /*
    * blockchainWallet returns the assetID and wallet for the blockchain for
    * which this ticker is a native asset, if it exists.
    */
  blockchainWallet () {
    for (const { assetID } of this.chainAssets) {
      const { wallet, token } = app().assets[assetID]
      if (!token) return { assetID, wallet }
    }
  }

  get avail () {
    return this.chainAssets.reduce((sum: number, ca: ChainAsset) => sum + ca.bal.available, 0)
  }

  get immature () {
    return this.chainAssets.reduce((sum: number, ca: ChainAsset) => sum + ca.bal.immature, 0)
  }

  get locked () {
    return this.chainAssets.reduce((sum: number, ca: ChainAsset) => sum + ca.bal.locked, 0)
  }

  get total () {
    return this.chainAssets.reduce((sum: number, { bal: { available, locked, immature } }: ChainAsset) => {
      return sum + available + locked + immature
    }, 0)
  }

  get xcRate () {
    return app().fiatRatesMap[this.bestID]
  }
}

export function normalizedTicker (a: SupportedAsset): string {
  const ticker = a.unitInfo.conventional.unit
  return ticker === 'WETH' ? 'ETH' : ticker === 'WBTC' ? 'BTC' : ticker
}

export function prepareTickerAssets (): Record<string, TickerAsset> {
  const tickerList: TickerAsset[] = []
  const tickerMap: Record<string, TickerAsset> = {}

  for (const a of Object.values(app().user.assets)) {
    const normedTicker = normalizedTicker(a)
    let ta = tickerMap[normedTicker]
    if (ta) {
      ta.addChainAsset(a)
      continue
    }
    ta = new TickerAsset(a)
    tickerList.push(ta)
    tickerMap[normedTicker] = ta
  }
  return tickerMap
}
