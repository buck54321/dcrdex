import {
  User,
  CoreNote,
  RateNote,
  OrderNote,
  Market,
  Order,
  BalanceNote,
  WalletConfigNote,
  FeePaymentNote,
  MatchNote,
  ConnEventNote,
  SpotPriceNote,
  BotNote,
  BotReport,
  Match
} from '../registry'

let dexcAddr: string

export function setServerAddress (addr: string) {
  dexcAddr = addr
}

export function serverAddress (): string {
  return dexcAddr
}

export function socketAddress (): string {
  return `ws${dexcAddr.substring(4)}/ws`
}

export function updateUser (user: User, note: CoreNote) {
  if (note.type === 'fiatrateupdate') {
    this.fiatRatesMap = (note as RateNote).fiatRates
    return
  }
  // Some notes can be received before we get a User during login.
  if (!user) return
  switch (note.type) {
    case 'order': {
      const order = (note as OrderNote).order
      const mkt = user.exchanges[order.host].markets[order.market]
      // Updates given order in market's orders list if it finds it.
      // Returns a bool which indicates if order was found.
      const updateOrder = (mkt: Market, ord: Order) => {
        for (const i in mkt.orders || []) {
          if (mkt.orders[i].id === ord.id) {
            mkt.orders[i] = ord
            return true
          }
        }
        return false
      }
      // If the notification order already exists we update it.
      // In case market's orders list is empty or the notification order isn't
      // part of it we add it manually as this means the order was
      // just placed.
      if (!mkt.orders) mkt.orders = [order]
      else if (!updateOrder(mkt, order)) mkt.orders.push(order)
      break
    }
    case 'balance': {
      const n: BalanceNote = note as BalanceNote
      const asset = user.assets[n.assetID]
      // Balance updates can come before the user is fetched after login.
      if (!asset) break
      const w = asset.wallet
      if (w) w.balance = n.balance
      break
    }
    case 'feepayment':
      this.handleFeePaymentNote(note as FeePaymentNote)
      break
    case 'walletstate':
    case 'walletconfig': {
      // assets can be null if failed to connect to dex server.
      if (!user.assets) return
      const wallet = (note as WalletConfigNote).wallet
      const asset = user.assets[wallet.assetID]
      asset.wallet = wallet
      break
    }
    case 'match': {
      const n = note as MatchNote
      const ord = this.order(n.orderID)
      if (ord) updateMatch(ord, n.match)
      break
    }
    case 'conn': {
      const n = note as ConnEventNote
      const xc = user.exchanges[n.host]
      if (xc) xc.connectionStatus = n.connectionStatus
      break
    }
    case 'spots': {
      const n = note as SpotPriceNote
      const xc = user.exchanges[n.host]
      // Spots can come before the user is fetched after login and before/while the
      // markets page reload when it receives a dex conn note.
      if (!xc || !xc.markets) break
      for (const [mktName, spot] of Object.entries(n.spots)) xc.markets[mktName].spot = spot
      break
    }
    case 'fiatrateupdate': {
      this.fiatRatesMap = (note as RateNote).fiatRates
      break
    }
    case 'bot': {
      const n = note as BotNote
      const [r, bots] = [n.report, this.user.bots]
      const idx = bots.findIndex((report: BotReport) => report.programID === r.programID)
      switch (n.topic) {
        case 'BotRetired':
          if (idx >= 0) bots.splice(idx, 1)
          break
        default:
          if (idx >= 0) bots[idx] = n.report
          else bots.push(n.report)
      }
    }
  }
}

/* updateMatch updates the match in or adds the match to the order. */
function updateMatch (order: Order, match: Match) {
  for (const i in order.matches) {
    const m = order.matches[i]
    if (m.matchID === match.matchID) {
      order.matches[i] = match
      return
    }
  }
  order.matches = order.matches || []
  order.matches.push(match)
}
