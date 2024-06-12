import {
  app,
  PageElement,
  MMBotStatus,
  RunStatsNote,
  ExchangeBalance,
  StartConfig,
  OrderPlacement,
  AutoRebalanceConfig
} from './registry'
import {
  MM,
  CEXDisplayInfo,
  CEXDisplayInfos,
  botTypeBasicArb,
  botTypeArbMM,
  botTypeBasicMM,
  setMarketElements,
  setCexElements,
  PlacementsChart,
  BotMarket,
  hostedMarketID,
  RunningMarketMakerDisplay
} from './mmutil'
import Doc, { MiniSlider } from './doc'
import State from './state'
import BasePage from './basepage'
import * as OrderUtil from './orderutil'
import { Forms, CEXConfigurationForm } from './forms'
import * as intl from './locales'

interface FundingSlider {
  left: {
    cex: number
    dex: number
  }
  right: {
    cex: number
    dex: number
  }
  cexRange: number
  dexRange: number
}

const newSlider = () => {
  return {
    left: {
      cex: 0,
      dex: 0
    },
    right: {
      cex: 0,
      dex: 0
    },
    cexRange: 0,
    dexRange: 0
  }
}

interface FundingSource {
  avail: number
  req: number
  funded: boolean
}

interface FundingOutlook {
  dex: FundingSource
  cex: FundingSource
  transferable: number
  fees: {
    avail: number
    req: number
    funded: boolean
  },
  fundedAndBalanced: boolean
  fundedAndNotBalanced: boolean
}

function parseFundingOptions (f: FundingOutlook): [number, number, FundingSlider | undefined] {
  const { cex: { avail: cexAvail, req: cexReq }, dex: { avail: dexAvail, req: dexReq }, transferable } = f

  let proposedDex = Math.min(dexAvail, dexReq)
  let proposedCex = Math.min(cexAvail, cexReq)
  let slider: FundingSlider | undefined
  if (f.fundedAndNotBalanced) {
    // We have everything we need, but not where we need it, and we can
    // deposit and withdraw.
    if (dexAvail > dexReq) {
      // We have too much dex-side, so we'll have to draw on dex to balance
      // cex's shortcomings.
      const cexShort = cexReq - cexAvail
      const dexRemain = dexAvail - dexReq
      if (dexRemain < cexShort) {
        // We did something really bad with math to get here.
        throw Error('bad math has us with dex surplus + cex underfund invalid remains')
      }
      proposedDex += cexShort
    } else {
      // We don't have enough on dex, but we have enough on cex to cover the
      // short.
      const dexShort = dexReq - dexAvail
      const cexRemain = cexAvail - cexReq
      if (cexRemain < dexShort) {
        throw Error('bad math got us with cex surplus + dex underfund invalid remains')
      }
      proposedCex += dexShort
    }
  } else if (f.fundedAndBalanced && transferable > 0) {
    // This asset is fully funded, but the user may choose to fund order
    // reserves either cex or dex.
    const dexRemain = dexAvail - dexReq
    const cexRemain = cexAvail - cexReq

    slider = newSlider()

    if (cexRemain > transferable && dexRemain > transferable) {
      // Either one could fully fund order reserves. Let the user choose.
      slider.left.cex = transferable + cexReq
      slider.left.dex = dexReq
      slider.right.cex = cexReq
      slider.right.dex = transferable + dexReq
    } else if (dexRemain < transferable && cexRemain < transferable) {
      // => implied that cexRemain + dexRemain > transferable.
      // CEX can contribute SOME and DEX can contribute SOME.
      slider.left.cex = transferable - dexRemain + cexReq
      slider.left.dex = dexRemain + dexReq
      slider.right.cex = cexRemain + cexReq
      slider.right.dex = transferable - cexRemain + dexReq
    } else if (dexRemain > transferable) {
      // So DEX has enough to cover reserves, but CEX could potentially
      // constribute SOME. NOT ALL.
      slider.left.cex = cexReq
      slider.left.dex = transferable + dexReq
      slider.right.cex = cexRemain + cexReq
      slider.right.dex = transferable - cexRemain + dexReq
    } else {
      // CEX has enough to cover reserves, but DEX could contribute SOME,
      // NOT ALL.
      slider.left.cex = transferable - dexRemain + cexReq
      slider.left.dex = dexRemain + dexReq
      slider.right.cex = transferable + cexReq
      slider.right.dex = dexReq
    }
    // We prefer the slider right in the center.
    slider.cexRange = slider.right.cex - slider.left.cex
    slider.dexRange = slider.right.dex - slider.left.dex
    proposedDex = slider.left.dex + (slider.dexRange / 2)
    proposedCex = slider.left.cex + (slider.cexRange / 2)
  }
  return [proposedDex, proposedCex, slider]
}

interface CEXRow {
  cexName: string
  tr: PageElement
  tmpl: Record<string, PageElement>
  dinfo: CEXDisplayInfo
}

export default class MarketMakerPage extends BasePage {
  page: Record<string, PageElement>
  forms: Forms
  currentForm: HTMLElement
  keyup: (e: KeyboardEvent) => void
  cexConfigForm: CEXConfigurationForm
  bots: Record<string, Bot>
  cexes: Record<string, CEXRow>

  constructor (main: HTMLElement) {
    super()

    this.bots = {}
    this.cexes = {}

    const page = this.page = Doc.idDescendants(main)

    Doc.cleanTemplates(page.botTmpl, page.botRowTmpl, page.exchangeRowTmpl)

    this.forms = new Forms(page.forms)
    this.cexConfigForm = new CEXConfigurationForm(page.cexConfigForm, (cexName: string) => this.cexConfigured(cexName))

    Doc.bind(page.newBot, 'click', () => { this.newBot() })
    Doc.bind(page.archivedLogsBtn, 'click', () => { app().loadPage('mmarchives') })
    // bindForm(page.pwForm, page.pwSubmit, () => this.startBots())

    for (const [cexName, dinfo] of Object.entries(CEXDisplayInfos)) {
      const tr = page.exchangeRowTmpl.cloneNode(true) as PageElement
      page.cexRows.appendChild(tr)
      const tmpl = Doc.parseTemplate(tr)
      Doc.bind(tmpl.configureBttn, 'click', () => {
        this.cexConfigForm.setCEX(cexName)
        this.forms.show(page.cexConfigForm)
      })
      const row = this.cexes[cexName] = { tr, tmpl, dinfo, cexName }
      this.updateCexRow(row)
    }

    this.setup()
  }

  async setup () {
    const page = this.page
    const mmStatus = app().mmStatus

    const botConfigs = mmStatus.bots.map((s: MMBotStatus) => s.config)
    app().registerNoteFeeder({
      runstats: (note: RunStatsNote) => { this.handleRunStatsNote(note) }
      // TODO bot start-stop notification
    })

    const noBots = !botConfigs || botConfigs.length === 0
    Doc.setVis(noBots, page.noBots)
    if (noBots) return

    const sortedBots = [...mmStatus.bots].sort((a: MMBotStatus, b: MMBotStatus) => {
      if (a.running && !b.running) return -1
      if (b.running && !a.running) return 1
      // If none are running, just do something to get a resonably reproducible
      // sort.
      if (!a.running && !b.running) return (a.config.baseID + a.config.quoteID) - (b.config.baseID + b.config.quoteID)
      // Both are running. Sort by run time.
      return (b.runStats?.startTime ?? 0) - (a.runStats?.startTime ?? 0)
    })

    const startupBalanceCache: Record<number, Promise<ExchangeBalance>> = {}

    for (const botStatus of sortedBots) this.addBot(botStatus, startupBalanceCache)
  }

  async handleRunStatsNote (note: RunStatsNote) {
    const { baseID, quoteID, host } = note
    const bot = this.bots[hostedMarketID(host, baseID, quoteID)]
    if (bot) return bot.handleRunStats()
    this.addBot(app().botStatus(host, baseID, quoteID) as MMBotStatus)
  }

  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }

  addBot (botStatus: MMBotStatus, startupBalanceCache?: Record<number, Promise<ExchangeBalance>>) {
    const { page, bots } = this
    const bot = new Bot(this, botStatus, startupBalanceCache)
    page.box.appendChild(bot.div)
    page.botRows.appendChild(bot.row.tr)
    bots[bot.id] = bot
  }

  showBot (botID: string) {
    this.page.overview.after(this.bots[botID].div)
  }

  newBot () {
    app().loadPage('mmsettings')
  }

  async cexConfigured (cexName: string) {
    await app().fetchMMStatus()
    this.updateCexRow(this.cexes[cexName])
    this.forms.close()
  }

  updateCexRow (row: CEXRow) {
    const { tmpl, dinfo, cexName } = row
    tmpl.logo.src = dinfo.logo
    tmpl.name.textContent = dinfo.name
    const status = app().mmStatus.cexes[cexName]
    Doc.setVis(!status, tmpl.unconfigured)
    Doc.setVis(status && !status.connectErr, tmpl.configured)
    Doc.setVis(status?.connectErr, tmpl.connectErrBox)
    if (status?.connectErr) tmpl.connectErr.textContent = `connect error: ${status.connectErr}`
    tmpl.logo.classList.toggle('greyscale', !status)
    if (!status) return
    let usdBal = 0
    for (const [assetIDStr, bal] of Object.entries(status.balances)) {
      const assetID = parseInt(assetIDStr)
      const { unitInfo } = app().assets[assetID]
      const fiatRate = app().fiatRatesMap[assetID]
      if (fiatRate) usdBal += fiatRate * (bal.available + bal.locked) / unitInfo.conventional.conversionFactor
    }
    tmpl.usdBalance.textContent = Doc.formatFourSigFigs(usdBal)
  }

  percentageBalanceStr (assetID: number, balance: number, percentage: number): string {
    const asset = app().assets[assetID]
    const unitInfo = asset.unitInfo
    const assetValue = Doc.formatCoinValue((balance * percentage) / 100, unitInfo)
    return `${Doc.formatFourSigFigs(percentage)}% - ${assetValue} ${asset.symbol.toUpperCase()}`
  }

  /*
   * walletBalanceStr returns a string like "50% - 0.0001 BTC" representing
   * the percentage of a wallet's balance selected in the market maker setting,
   * and the amount of that asset in the wallet.
   */
  walletBalanceStr (assetID: number, percentage: number): string {
    const { wallet: { balance: { available } } } = app().assets[assetID]
    return this.percentageBalanceStr(assetID, available, percentage)
  }
}

interface BotRow {
  tr: PageElement
  tmpl: Record<string, PageElement>
}

class Bot extends BotMarket {
  pg: MarketMakerPage
  div: PageElement
  page: Record<string, PageElement>
  placementsChart: PlacementsChart
  baseAllocSlider: MiniSlider
  quoteAllocSlider: MiniSlider
  row: BotRow
  runDisplay: RunningMarketMakerDisplay

  constructor (pg: MarketMakerPage, status: MMBotStatus, startupBalanceCache?: Record<number, Promise<ExchangeBalance>>) {
    super(status.config)
    startupBalanceCache = startupBalanceCache ?? {}
    this.pg = pg
    const { baseID, quoteID, host, botType, nBuyPlacements, nSellPlacements, cexName } = this

    const div = this.div = pg.page.botTmpl.cloneNode(true) as PageElement
    const page = this.page = Doc.parseTemplate(div)
    this.runDisplay = new RunningMarketMakerDisplay(page.onBox)

    setMarketElements(div, baseID, quoteID, host)
    if (cexName) setCexElements(div, cexName)

    if (botType === botTypeArbMM) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
    } else if (botType === botTypeBasicArb) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
    } else if (botType === botTypeBasicMM) {
      page.botTypeDisplay.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
    }

    Doc.setVis(botType !== botTypeBasicArb, page.placementsChartBox, page.baseTokenSwapFeesBox)
    if (botType !== botTypeBasicArb) {
      this.placementsChart = new PlacementsChart(page.placementsChart)
      page.buyPlacementCount.textContent = String(nBuyPlacements)
      page.sellPlacementCount.textContent = String(nSellPlacements)
    }

    Doc.bind(page.startBttn, 'click', () => this.start())
    Doc.bind(page.allocationBttn, 'click', () => this.allocate())
    Doc.bind(page.reconfigureBttn, 'click', () => this.reconfigure())
    Doc.bind(page.goBackFromAllocation, 'click', () => this.hideAllocationDialog())
    Doc.bind(page.marketLink, 'click', () => app().loadPage('markets', { host, baseID, quoteID }))

    this.baseAllocSlider = new MiniSlider(page.baseAllocSlider, () => { /* callback set later */ })
    this.quoteAllocSlider = new MiniSlider(page.quoteAllocSlider, () => { /* callback set later */ })

    const tr = pg.page.botRowTmpl.cloneNode(true) as PageElement
    setMarketElements(tr, baseID, quoteID, host)
    const tmpl = Doc.parseTemplate(tr)
    this.row = { tr, tmpl }
    Doc.bind(tmpl.allocateBttn, 'click', (e: MouseEvent) => {
      e.stopPropagation()
      this.allocate()
      pg.showBot(this.id)
    })
    Doc.bind(tr, 'click', () => pg.showBot(this.id))

    this.initialize(startupBalanceCache)
  }

  async initialize (startupBalanceCache: Record<number, Promise<ExchangeBalance>>) {
    await super.initialize(startupBalanceCache)
    this.runDisplay.setBotMarket(this)
    const {
      page, host, cexName, botType,
      cfg: { arbMarketMakingConfig, basicMarketMakingConfig }, mktID,
      baseFactor, quoteFactor, marketReport: { baseFiatRate }
    } = this

    if (botType !== botTypeBasicArb) {
      let buyPlacements: OrderPlacement[] = []
      let sellPlacements: OrderPlacement[] = []
      let profit = 0
      if (arbMarketMakingConfig) {
        buyPlacements = arbMarketMakingConfig.buyPlacements.map((p) => ({ lots: p.lots, gapFactor: p.multiplier }))
        sellPlacements = arbMarketMakingConfig.sellPlacements.map((p) => ({ lots: p.lots, gapFactor: p.multiplier }))
        profit = arbMarketMakingConfig.profit
      } else if (basicMarketMakingConfig) {
        buyPlacements = basicMarketMakingConfig.buyPlacements
        sellPlacements = basicMarketMakingConfig.sellPlacements
        const bestBuy = basicMarketMakingConfig.buyPlacements.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
        const bestSell = basicMarketMakingConfig.sellPlacements.reduce((prev: OrderPlacement, curr: OrderPlacement) => curr.gapFactor < prev.gapFactor ? curr : prev)
        profit = (bestBuy.gapFactor + bestSell.gapFactor) / 2
      }
      const marketConfig = { cexName: cexName as string, botType, baseFiatRate: baseFiatRate, buyPlacements, sellPlacements, profit }
      this.placementsChart.setMarket(marketConfig)
    }

    Doc.setVis(botType !== botTypeBasicMM, page.cexDataBox)
    if (botType !== botTypeBasicMM) {
      const cex = app().mmStatus.cexes[cexName]
      if (cex) {
        const mkt = cex.markets ? cex.markets[mktID] : undefined
        Doc.setVis(mkt?.day, page.cexDataBox)
        if (mkt?.day) {
          const day = mkt.day
          page.cexPrice.textContent = Doc.formatFourSigFigs(day.lastPrice)
          page.cexVol.textContent = Doc.formatFourSigFigs(baseFiatRate * day.vol)
        }
      }
    }

    const { spot } = app().exchanges[host].markets[mktID]
    if (spot) {
      Doc.show(page.dexDataBox)
      const c = OrderUtil.RateEncodingFactor / baseFactor * quoteFactor
      page.dexPrice.textContent = Doc.formatFourSigFigs(spot.rate / c)
      page.dexVol.textContent = Doc.formatFourSigFigs(spot.vol24 / baseFactor * baseFiatRate)
    }

    this.updateDisplay()
    this.updateTableRow()
    Doc.hide(page.loadingBg)
  }

  updateTableRow () {
    const { row: { tmpl } } = this
    const { running, runStats } = this.status()
    Doc.setVis(running, tmpl.profitLossBox)
    Doc.setVis(!running, tmpl.allocateBttnBox)
    if (runStats) {
      tmpl.profitLoss.textContent = Doc.formatFourSigFigs(runStats.profitLoss.profit)
    }
  }

  updateDisplay () {
    const { page } = this
    const { running } = this.status()
    Doc.setVis(running, page.onBox)
    Doc.setVis(!running, page.offBox)
    if (running) this.updateRunningDisplay()
    else this.updateIdleDisplay()
  }

  updateRunningDisplay () {
    this.runDisplay.update()
  }

  updateIdleDisplay () {
    const {
      page, proj: { alloc, qProj, bProj }, baseID, quoteID, cexName, bui, qui, needQuoteFeeAsset,
      needBaseFeeAsset, baseFeeID, quoteFeeID, baseFactor, quoteFactor, baseFeeFactor, quoteFeeFactor,
      marketReport: { baseFiatRate, quoteFiatRate }, cfg: { uiConfig: { baseConfig, quoteConfig } },
      quoteFeeUI, baseFeeUI
    } = this
    page.baseAlloc.textContent = Doc.formatFullPrecision(alloc[baseID], bui)
    const baseUSD = alloc[baseID] / baseFactor * baseFiatRate
    let totalUSD = baseUSD
    page.baseAllocUSD.textContent = Doc.formatFourSigFigs(baseUSD)
    page.baseBookAlloc.textContent = Doc.formatFullPrecision(bProj.book * baseFactor, bui)
    page.baseOrderReservesAlloc.textContent = Doc.formatFullPrecision(bProj.orderReserves * baseFactor, bui)
    page.baseOrderReservesPct.textContent = String(Math.round(baseConfig.orderReservesFactor * 100))
    Doc.setVis(cexName, page.baseCexAllocBox)
    if (cexName) page.baseCexAlloc.textContent = Doc.formatFullPrecision(bProj.cex * baseFactor, bui)
    Doc.setVis(baseFeeID === baseID, page.baseBookingFeesAllocBox)
    Doc.setVis(needBaseFeeAsset, page.baseTokenFeesAllocBox)
    if (baseFeeID === baseID) {
      const bookingFees = baseID === quoteFeeID ? bProj.bookingFees + qProj.bookingFees : bProj.bookingFees
      page.baseBookingFeesAlloc.textContent = Doc.formatFullPrecision(bookingFees * baseFeeFactor, baseFeeUI)
    } else if (needBaseFeeAsset) {
      page.baseTokenFeeAlloc.textContent = Doc.formatFullPrecision(alloc[baseFeeID], baseFeeUI)
      const baseFeeUSD = alloc[baseFeeID] / baseFeeFactor * app().fiatRatesMap[baseFeeID]
      totalUSD += baseFeeUSD
      page.baseTokenAllocUSD.textContent = Doc.formatFourSigFigs(baseFeeUSD)
      page.baseTokenBookingFees.textContent = Doc.formatFullPrecision(bProj.bookingFees * baseFeeFactor, baseFeeUI)
      page.baseTokenSwapFeeN.textContent = String(baseConfig.swapFeeN)
      page.baseTokenSwapFees.textContent = Doc.formatFullPrecision(bProj.swapFeeReserves * baseFactor, baseFeeUI)
    }

    page.quoteAlloc.textContent = Doc.formatFullPrecision(alloc[quoteID], qui)
    const quoteUSD = alloc[quoteID] / quoteFactor * quoteFiatRate
    totalUSD += quoteUSD
    page.quoteAllocUSD.textContent = Doc.formatFourSigFigs(quoteUSD)
    page.quoteBookAlloc.textContent = Doc.formatFullPrecision(qProj.book * quoteFactor, qui)
    page.quoteOrderReservesAlloc.textContent = Doc.formatFullPrecision(qProj.orderReserves * quoteFactor, qui)
    page.quoteOrderReservesPct.textContent = String(Math.round(quoteConfig.orderReservesFactor * 100))
    page.quoteSlippageAlloc.textContent = Doc.formatFullPrecision(qProj.slippageBuffer * quoteFactor, qui)
    page.slippageBufferFactor.textContent = String(Math.round(quoteConfig.slippageBufferFactor * 100))
    Doc.setVis(cexName, page.quoteCexAllocBox)
    if (cexName) page.quoteCexAlloc.textContent = Doc.formatFullPrecision(qProj.cex * quoteFactor, qui)
    const needQuoteFeesDisplay = needQuoteFeeAsset && quoteFeeID !== baseFeeID
    Doc.setVis(quoteID === quoteFeeID, page.quoteBookingFeesAllocBox)
    Doc.setVis(needQuoteFeesDisplay, page.quoteTokenFeesAllocBox)
    if (quoteID === quoteFeeID) {
      const bookingFees = quoteID === baseFeeID ? bProj.bookingFees + qProj.bookingFees : qProj.bookingFees
      page.quoteBookingFeesAlloc.textContent = Doc.formatFullPrecision(bookingFees * quoteFeeFactor, quoteFeeUI)
    } else if (needQuoteFeesDisplay) {
      page.quoteTokenFeeAlloc.textContent = Doc.formatFullPrecision(alloc[quoteFeeID], quoteFeeUI)
      const quoteFeeUSD = alloc[quoteFeeID] / quoteFeeFactor * app().fiatRatesMap[quoteFeeID]
      totalUSD += quoteFeeUSD
      page.quoteTokenAllocUSD.textContent = Doc.formatFourSigFigs(quoteFeeUSD)
      page.quoteTokenBookingFees.textContent = Doc.formatFullPrecision(qProj.bookingFees * quoteFeeFactor, quoteFeeUI)
      page.quoteTokenSwapFeeN.textContent = String(quoteConfig.swapFeeN)
      page.quoteTokenSwapFees.textContent = Doc.formatFullPrecision(qProj.swapFeeReserves * quoteFeeFactor, quoteFeeUI)
    }
    page.totalAllocUSD.textContent = Doc.formatFourSigFigs(totalUSD)
  }

  /*
   * allocate opens a dialog to choose funding sources (if applicable) and
   * confirm allocations and start the bot.
   */
  allocate () {
    const f = this.fundingState()
    const {
      page, marketReport: { baseFiatRate, quoteFiatRate }, baseID, quoteID,
      baseFeeID, quoteFeeID, baseFeeFiatRate, quoteFeeFiatRate, cexName,
      baseFactor, quoteFactor, baseFeeFactor, quoteFeeFactor, needBaseFeeAsset,
      needQuoteFeeAsset
    } = this

    page.appPW.value = ''
    Doc.setVis(!State.passwordIsCached(), page.appPWBox)

    const [proposedDexBase, proposedCexBase, baseSlider] = parseFundingOptions(f.base)
    const [proposedDexQuote, proposedCexQuote, quoteSlider] = parseFundingOptions(f.quote)

    const alloc = this.alloc = {
      dex: {
        [baseID]: proposedDexBase * baseFactor,
        [quoteID]: proposedDexQuote * quoteFactor
      },
      cex: {
        [baseID]: proposedCexBase * baseFactor,
        [quoteID]: proposedCexQuote * quoteFactor
      }
    }
    if (f.base.fees.req > 0) alloc.dex[baseFeeID] = f.base.fees.req * baseFeeFactor
    // special handling here because quoteFeeID could also be baseFeeID
    if (f.quote.fees.req > 0) alloc.dex[quoteFeeID] = ((alloc.dex[quoteFeeID] ?? 0) + f.quote.fees.req) * quoteFeeFactor

    let totalUSD = (alloc.dex[baseID] / baseFactor * baseFiatRate) + (alloc.dex[quoteID] / quoteFactor * quoteFiatRate)
    totalUSD += (alloc.cex[baseID] / baseFactor * baseFiatRate) + (alloc.cex[quoteID] / quoteFactor * quoteFiatRate)
    if (needBaseFeeAsset) totalUSD += alloc.dex[baseFeeID] / baseFeeFactor * baseFeeFiatRate
    if (needQuoteFeeAsset && quoteFeeID !== baseFeeID) totalUSD += alloc.dex[quoteFeeID] / quoteFeeFactor * quoteFeeFiatRate
    page.allocUSD.textContent = Doc.formatFourSigFigs(totalUSD)

    Doc.setVis(cexName, ...Doc.applySelector(page.allocationDialog, '[data-cex-only]'))
    Doc.setVis(f.fundedAndBalanced, page.fundedAndBalancedBox)
    Doc.setVis(f.base.transferable + f.quote.transferable > 0, page.hasTransferable)
    Doc.setVis(f.fundedAndNotBalanced, page.fundedAndNotBalancedBox)
    Doc.setVis(f.starved, page.starvedBox)
    page.startBttn.classList.toggle('go', f.fundedAndBalanced)

    const setBaseProposal = (dex: number, cex: number) => {
      page.proposedDexBaseAlloc.textContent = Doc.formatFourSigFigs(dex)
      page.proposedDexBaseAllocUSD.textContent = Doc.formatFourSigFigs(dex * baseFiatRate)
      page.proposedCexBaseAlloc.textContent = Doc.formatFourSigFigs(cex)
      page.proposedCexBaseAllocUSD.textContent = Doc.formatFourSigFigs(cex * baseFiatRate)
    }
    setBaseProposal(proposedDexBase, proposedCexBase)

    Doc.setVis(baseSlider, page.baseAllocSlider)
    if (baseSlider) {
      const dexRange = baseSlider.right.dex - baseSlider.left.dex
      const cexRange = baseSlider.right.cex - baseSlider.left.cex
      this.baseAllocSlider.setValue(0.5)
      this.baseAllocSlider.changed = (r: number) => {
        const dexAlloc = baseSlider.left.dex + r * dexRange
        const cexAlloc = baseSlider.left.cex + r * cexRange
        alloc.dex[baseID] = dexAlloc * baseFeeFactor
        alloc.cex[baseID] = cexAlloc * baseFeeFactor
        setBaseProposal(dexAlloc, cexAlloc)
      }
    }

    const setQuoteProposal = (dex: number, cex: number) => {
      page.proposedDexQuoteAlloc.textContent = Doc.formatFourSigFigs(dex)
      page.proposedDexQuoteAllocUSD.textContent = Doc.formatFourSigFigs(dex * quoteFiatRate)
      page.proposedCexQuoteAlloc.textContent = Doc.formatFourSigFigs(cex)
      page.proposedCexQuoteAllocUSD.textContent = Doc.formatFourSigFigs(cex * quoteFiatRate)
    }
    setQuoteProposal(proposedDexQuote, proposedCexQuote)

    Doc.setVis(quoteSlider, page.quoteAllocSlider)
    if (quoteSlider) {
      const dexRange = quoteSlider.right.dex - quoteSlider.left.dex
      const cexRange = quoteSlider.right.cex - quoteSlider.left.cex
      this.quoteAllocSlider.setValue(0.5)
      this.quoteAllocSlider.changed = (r: number) => {
        const dexAlloc = quoteSlider.left.dex + r * dexRange
        const cexAlloc = quoteSlider.left.cex + r * cexRange
        alloc.dex[quoteID] = dexAlloc * quoteFeeFactor
        alloc.cex[quoteID] = cexAlloc * quoteFeeFactor
        setQuoteProposal(dexAlloc, cexAlloc)
      }
    }

    const needBaseTokenFees = baseID !== baseFeeID && baseFeeID !== quoteID
    Doc.setVis(needBaseTokenFees, ...Doc.applySelector(page.allocationDialog, '[data-base-token-fees]'))
    if (needBaseTokenFees) {
      const feeReq = f.base.fees.req
      page.proposedDexBaseFeeAlloc.textContent = Doc.formatFourSigFigs(feeReq)
      page.proposedDexBaseFeeAllocUSD.textContent = Doc.formatFourSigFigs(feeReq * baseFeeFiatRate)
    }

    const needQuoteTokenFees = quoteID !== quoteFeeID && quoteFeeID !== baseID
    Doc.setVis(needQuoteTokenFees, ...Doc.applySelector(page.allocationDialog, '[data-quote-token-fees]'))
    if (needQuoteTokenFees) {
      const feeReq = f.quote.fees.req
      page.proposedDexQuoteFeeAlloc.textContent = Doc.formatFourSigFigs(feeReq)
      page.proposedDexQuoteFeeAllocUSD.textContent = Doc.formatFourSigFigs(feeReq * quoteFeeFiatRate)
    }

    Doc.show(page.allocationDialog)
    const closeDialog = (e: MouseEvent) => {
      if (Doc.mouseInElement(e, page.allocationDialog)) return
      this.hideAllocationDialog()
      Doc.unbind(document, 'click', closeDialog)
    }
    Doc.bind(document, 'click', closeDialog)
  }

  hideAllocationDialog () {
    this.page.appPW.value = ''
    Doc.hide(this.page.allocationDialog)
  }

  async start () {
    const { page, alloc, baseID, quoteID, host, cfg: { uiConfig: { cexRebalance } } } = this

    Doc.hide(page.errMsg)
    const appPW = page.appPW.value
    if (!appPW && !State.passwordIsCached()) {
      page.errMsg.textContent = intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG)
      Doc.show(page.errMsg)
      return
    }

    // round allocations values.
    for (const m of [alloc.dex, alloc.cex]) {
      for (const [assetID, v] of Object.entries(m)) m[parseInt(assetID)] = Math.round(v)
    }

    const startConfig: StartConfig = {
      base: baseID,
      quote: quoteID,
      host: host,
      alloc: alloc
    }
    if (cexRebalance) startConfig.autoRebalance = this.autoRebalanceSettings()

    try {
      app().log('mm', 'starting mm bot', startConfig)
      await MM.startBot(appPW, startConfig)
    } catch (e) {
      page.errMsg.textContent = intl.prep(intl.ID_API_ERROR, e.msg)
      Doc.show(page.errMsg)
      return
    }
    this.hideAllocationDialog()
  }

  autoRebalanceSettings (): AutoRebalanceConfig {
    const {
      proj: { bProj, qProj, alloc }, baseFeeID, quoteFeeID, cfg: { uiConfig: { baseConfig, quoteConfig } },
      lotSize: minBase, quoteLot: minQuote, baseID, quoteID
    } = this

    const totalBase = alloc[baseID]
    let dexMinBase = bProj.book
    if (baseID === baseFeeID) dexMinBase += bProj.bookingFees
    if (baseID === quoteFeeID) dexMinBase += qProj.bookingFees
    let dexMinQuote = qProj.book
    if (quoteID === quoteFeeID) dexMinQuote += qProj.bookingFees
    if (quoteID === baseFeeID) dexMinQuote += bProj.bookingFees
    const maxBase = Math.max(totalBase - dexMinBase, totalBase - bProj.cex)
    const totalQuote = alloc[quoteID]
    const maxQuote = Math.max(totalQuote - dexMinQuote, totalQuote - qProj.cex)
    if (maxBase < 0 || maxQuote < 0) {
      throw Error(`rebalance math doesn't work: ${JSON.stringify({ bProj, qProj, maxBase, maxQuote })}`)
    }
    const baseRange = maxBase - minBase
    const quoteRange = maxQuote - minQuote
    return {
      minBaseTransfer: Math.round(minBase + baseConfig.transferFactor * baseRange),
      minQuoteTransfer: Math.round(minQuote + quoteConfig.transferFactor * quoteRange)
    }
  }

  reconfigure () {
    const { host, baseID, quoteID, cexName, botType } = this
    app().loadPage('mmsettings', { host, baseID, quoteID, cexName, botType })
  }

  handleRunStats () {
    this.updateDisplay()
    this.updateTableRow()
  }
}
