import {
  app,
  PageElement,
  Market,
  Exchange,
  OrderOption,
  XYRange,
  BotReport,
  BotNote
} from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { setOptionTemplates, XYRangeOption } from './opts'
import State from './state'
import { bind as bindForm } from './forms'

interface HostedMarket extends Market {
  host: string
}

interface LiveProgram extends BotReport {
  tmpl: Record<string, PageElement>
  div: PageElement
}

interface BaseOption {
  key: string
  displayname: string
  description: string
  default: number
  min: number
  max: number
}

// Oracle Weighting
const oracleWeightBaseOption: BaseOption = {
  key: 'oracleWeighting',
  displayname: 'Oracle Weight',
  description: 'Enable the oracle and set its weight in calculating our target price.',
  default: 0.1,
  max: 1.0,
  min: 0.0
}
const oracleWeightRange: XYRange = {
  start: {
    label: '0%',
    x: 0,
    y: 0
  },
  end: {
    label: '100%',
    x: 1,
    y: 100
  },
  xUnit: '',
  yUnit: '%'
}
const oracleWeightOption: OrderOption = createXYRange(oracleWeightBaseOption, oracleWeightRange)

const oracleBiasBaseOption: BaseOption = {
  key: 'oracleBias',
  displayname: 'Oracle Bias',
  description: 'Apply an adjustment to the oracles rate, up to +/-1%.',
  default: 0.0,
  max: 0.01,
  min: -0.01
}
const oracleBiasRange: XYRange = {
  start: {
    label: '-1%',
    x: -0.01,
    y: -1
  },
  end: {
    label: '1%',
    x: 0.01,
    y: 1
  },
  xUnit: '',
  yUnit: '%'
}
const oracleBiasOption: OrderOption = createXYRange(oracleBiasBaseOption, oracleBiasRange)

const driftToleranceBaseOption: BaseOption = {
  key: 'driftTolerance',
  displayname: 'Drift Tolerance',
  description: 'How far from the ideal price will we allow orders to drift? Typically a fraction of a percent.',
  default: 0.001,
  max: 0.01,
  min: 0
}
const driftToleranceRange: XYRange = {
  start: {
    label: '0%',
    x: 0,
    y: 0
  },
  end: {
    label: '1%',
    x: 0.01,
    y: 1
  },
  xUnit: '',
  yUnit: '%'
}
const driftToleranceOption: OrderOption = createXYRange(driftToleranceBaseOption, driftToleranceRange)

const spreadMultBaseOption: BaseOption = {
  key: 'spreadMultiplier',
  displayname: 'Spread Multiplier',
  description: 'Increase the spread for reduced risk and higher potential profits, but with a lower fill rate. ' +
    'The baseline value, 1, is the break even value, where tx fees and profits are a wash in a static market.',
  default: 2,
  max: 10,
  min: 1
}
const spreadMultRange: XYRange = {
  start: {
    label: '0%',
    x: 0,
    y: 0
  },
  end: {
    label: '1000%',
    x: 10,
    y: 1000
  },
  xUnit: 'X',
  yUnit: '%'
}
const spreadMultOption: OrderOption = createXYRange(spreadMultBaseOption, spreadMultRange)

const animationLength = 300

export default class MarketMakerPage extends BasePage {
  page: Record<string, PageElement>
  data: any
  createOpts: Record<string, any>
  currentMarket: HostedMarket
  currentForm: PageElement
  keyup: (e: KeyboardEvent) => void
  exitEdit: () => void
  programs: Record<number, LiveProgram>
  editProgram: BotReport | null
  spreadMultOpt: XYRangeOption
  driftToleranceOpt: XYRangeOption
  biasOpt: XYRangeOption
  weightOpt: XYRangeOption
  pwHandler: ((pw: string) => Promise<void>) | null

  constructor (main: HTMLElement, data: any) {
    super()
    const page = this.page = Doc.idDescendants(main)
    this.data = data
    this.programs = {}
    this.editProgram = null
    this.pwHandler = null
    this.createOpts = {
      [oracleBiasBaseOption.key]: oracleBiasBaseOption.default,
      [oracleWeightBaseOption.key]: oracleWeightBaseOption.default,
      [driftToleranceOption.key]: driftToleranceOption.default
    }

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })

    setOptionTemplates(page)

    Doc.cleanTemplates(page.assetRowTmpl, page.booleanOptTmpl, page.rangeOptTmpl,
      page.orderOptTmpl, page.runningProgramTmpl)

    const selectClicked = (isBase: boolean): void => {
      const select = isBase ? page.baseSelect : page.quoteSelect
      const m = Doc.descendentMetrics(main, select)
      page.assetDropdown.style.left = `${m.bodyLeft}px`
      page.assetDropdown.style.top = `${m.bodyTop}px`

      const counterAsset = isBase ? this.currentMarket.quoteid : this.currentMarket.baseid
      const clickedSymbol = isBase ? this.currentMarket.basesymbol : this.currentMarket.quotesymbol

      // Look through markets for other base assets for the counter asset.
      const matches: Set<string> = new Set()
      const otherAssets: Set<string> = new Set()

      for (const mkt of sortedMarkets()) {
        otherAssets.add(mkt.basesymbol)
        otherAssets.add(mkt.quotesymbol)
        const [firstID, secondID] = isBase ? [mkt.quoteid, mkt.baseid] : [mkt.baseid, mkt.quoteid]
        const [firstSymbol, secondSymbol] = isBase ? [mkt.quotesymbol, mkt.basesymbol] : [mkt.basesymbol, mkt.quotesymbol]
        if (firstID === counterAsset) matches.add(secondSymbol)
        else if (secondID === counterAsset) matches.add(firstSymbol)
      }

      const options = Array.from(matches)
      options.sort((a: string, b: string) => a.localeCompare(b))
      for (const symbol of options) otherAssets.delete(symbol)
      const nonOptions = Array.from(otherAssets)
      nonOptions.sort((a: string, b: string) => a.localeCompare(b))

      Doc.empty(page.assetDropdown)
      const addOptions = (symbols: string[], avail: boolean): void => {
        for (const symbol of symbols) {
          const row = this.assetRow(symbol)
          Doc.bind(row, 'click', (e: MouseEvent) => {
            e.stopPropagation()
            if (symbol === clickedSymbol) return this.hideAssetDropdown() // no change
            this.leaveEditMode()
            if (isBase) this.setCreationBase(symbol)
            else this.setCreationQuote(symbol)
          })
          if (!avail) row.classList.add('ghost')
          page.assetDropdown.appendChild(row)
        }
      }
      addOptions(options, true)
      addOptions(nonOptions, false)

      Doc.show(page.assetDropdown)

      const clicker = (e: MouseEvent): void => {
        if (Doc.mouseInElement(e, page.assetDropdown)) return
        this.hideAssetDropdown()
        Doc.unbind(document, 'click', clicker)
      }
      Doc.bind(document, 'click', clicker)
    }

    Doc.bind(page.baseSelect, 'click', () => selectClicked(true))
    Doc.bind(page.quoteSelect, 'click', () => selectClicked(false))

    Doc.bind(page.marketSelect, 'change', () => {
      const [host, name] = page.marketSelect.value?.split(' ') as string[]
      this.setMarketSubchoice(host, name)
    })

    this.spreadMultOpt = new XYRangeOption(spreadMultOption, '', this.createOpts, () => this.createOptsUpdated())
    this.driftToleranceOpt = new XYRangeOption(driftToleranceOption, '', this.createOpts, () => this.createOptsUpdated())
    this.biasOpt = new XYRangeOption(oracleBiasOption, '', this.createOpts, () => this.createOptsUpdated())
    this.weightOpt = new XYRangeOption(oracleWeightOption, '', this.createOpts, () => this.createOptsUpdated())

    page.options.appendChild(this.spreadMultOpt.node)
    page.options.appendChild(this.driftToleranceOpt.node)
    page.options.appendChild(this.weightOpt.node)
    page.options.appendChild(this.biasOpt.node)

    Doc.bind(page.showAdvanced, 'click', () => {
      Doc.hide(page.showAdvanced)
      Doc.show(page.hideAdvanced, page.options)
    })

    Doc.bind(page.hideAdvanced, 'click', () => {
      Doc.hide(page.hideAdvanced, page.options)
      Doc.show(page.showAdvanced)
    })

    Doc.bind(page.runBttn, 'click', this.authedRoute(async (pw: string): Promise<void> => this.createBot(pw)))

    Doc.bind(page.lotsInput, 'change', () => {
      page.lotsInput.value = String(Math.round(parseFloat(page.lotsInput.value ?? '0')))
    })

    Doc.bind(page.exitEditMode, 'click', () => this.leaveEditMode())

    const submitPasswordForm = async () => {
      const pw = page.pwInput.value ?? ''
      if (pw === '') return
      const handler = this.pwHandler
      if (handler === null) return
      await handler(pw)
      this.pwHandler = null
      this.closePopups()
    }

    bindForm(page.pwForm, page.pwSubmit, () => submitPasswordForm())

    Doc.bind(page.pwInput, 'keyup', (e: KeyboardEvent) => {
      if (e.key !== 'Enter' && e.key !== 'NumpadEnter') return
      submitPasswordForm()
    })

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { this.closePopups() }
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        this.closePopups()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)

    this.exitEdit = () => this.leaveEditMode()

    app().registerNoteFeeder({
      bot: (n: BotNote) => this.handleBotNote(n)
    })

    this.setMarket([sortedMarkets()[0]])
    this.populateRunningPrograms()
    page.createBox.classList.remove('invisible')
    page.programsBox.classList.remove('invisible')
  }

  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: HTMLElement): Promise<void> {
    this.currentForm = form
    const page = this.page
    Doc.hide(page.pwForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  closePopups (): void {
    this.pwHandler = null
    this.page.pwInput.value = ''
    Doc.hide(this.page.forms)
  }

  hideAssetDropdown (): void {
    const page = this.page
    page.assetDropdown.scrollTop = 0
    Doc.hide(page.assetDropdown)
  }

  assetRow (symbol: string): PageElement {
    const row = this.page.assetRowTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(row)
    tmpl.logo.src = Doc.logoPath(symbol)
    tmpl.symbol.textContent = symbol.toUpperCase()
    return row
  }

  setMarket (mkts: HostedMarket[]): void {
    const mkt = mkts[0]
    this.currentMarket = mkt
    const page = this.page
    Doc.empty(page.baseSelect, page.quoteSelect)
    page.baseSelect.appendChild(this.assetRow(mkt.basesymbol))
    page.quoteSelect.appendChild(this.assetRow(mkt.quotesymbol))
    this.hideAssetDropdown()

    Doc.hide(page.marketSelect, page.marketOneChoice)
    if (mkts.length === 1) {
      Doc.show(page.marketOneChoice)
      page.marketOneChoice.textContent = `${mkt.basesymbol.toUpperCase()}-${mkt.quotesymbol.toUpperCase()} @ ${mkt.host}`
    } else {
      Doc.show(page.marketSelect)
      Doc.empty(page.marketSelect)
      for (const mkt of mkts) {
        const opt = document.createElement('option')
        opt.value = `${mkt.host} ${mkt.name}`
        opt.textContent = `${mkt.basesymbol.toUpperCase()}-${mkt.quotesymbol.toUpperCase()} @ ${mkt.host}`
        page.marketSelect.appendChild(opt)
      }
    }
  }

  setEditProgram (report: BotReport): void {
    const { createOpts, page } = this
    const pgm = report.program
    const [b, q] = [app().assets[pgm.baseID], app().assets[pgm.quoteID]]
    const mkt = app().exchanges[pgm.host].markets[`${b.symbol}_${q.symbol}`]
    this.setMarket([{
      host: pgm.host,
      ...mkt
    }])
    for (const p of Object.values(this.programs)) {
      if (p.programID !== report.programID) Doc.hide(p.div)
    }
    this.editProgram = report
    page.createBox.classList.add('edit')
    page.programsBox.classList.add('edit')
    page.lotsInput.value = String(pgm.lots)
    createOpts.oracleWeighting = pgm.oracleWeighting
    this.weightOpt.setValue(pgm.oracleWeighting)
    createOpts.oracleBias = pgm.oracleBias
    this.biasOpt.setValue(pgm.oracleBias)
    createOpts.driftTolerance = pgm.driftTolerance
    this.driftToleranceOpt.setValue(pgm.driftTolerance)
    createOpts.spreadMult = pgm.spreadMultiplier
    this.spreadMultOpt.setValue(pgm.spreadMultiplier)
    Doc.bind(page.programsBox, 'click', this.exitEdit)
  }

  leaveEditMode (): void {
    const page = this.page
    Doc.unbind(page.programsBox, 'click', this.exitEdit)
    for (const p of Object.values(this.programs)) Doc.show(p.div)
    page.createBox.classList.remove('edit')
    page.programsBox.classList.remove('edit')
    this.editProgram = null
  }

  populateRunningPrograms (): void {
    const page = this.page
    const bots = app().user.bots
    Doc.empty(page.runningPrograms)
    this.programs = {}
    for (const report of bots) page.runningPrograms.appendChild(this.programDiv(report))
    this.setProgramsHeader()
  }

  setProgramsHeader (): void {
    const page = this.page
    if (page.runningPrograms.children.length > 0) {
      Doc.show(page.programsHeader)
      Doc.hide(page.noProgramsMessage)
    } else {
      Doc.hide(page.programsHeader)
      Doc.show(page.noProgramsMessage)
    }
  }

  programDiv (report: BotReport): PageElement {
    const page = this.page
    const div = page.runningProgramTmpl.cloneNode(true) as PageElement
    const tmpl = Doc.parseTemplate(div)
    const startStop = async (endpoint: string, pw?: string): Promise<void> => {
      Doc.hide(tmpl.startErr)
      const loaded = app().loading(div)
      const res = await postJSON(endpoint, { programID: report.programID, appPW: pw })
      loaded()
      if (!app().checkResponse(res, true)) {
        tmpl.startErr.textContent = res.msg
        Doc.show(tmpl.startErr)
      }
    }
    Doc.bind(tmpl.pauseBttn, 'click', () => startStop('/api/stopbot'))
    Doc.bind(tmpl.startBttn, 'click', this.authedRoute(async (pw: string) => startStop('/api/startbot', pw)))
    Doc.bind(tmpl.retireBttn, 'click', () => startStop('/api/retirebot'))
    Doc.bind(tmpl.configureBttn, 'click', (e: MouseEvent) => {
      e.stopPropagation()
      this.setEditProgram(this.programs[report.programID])
    })
    const [b, q] = [app().assets[report.program.baseID], app().assets[report.program.quoteID]]
    tmpl.base.appendChild(this.assetRow(b.symbol))
    tmpl.quote.appendChild(this.assetRow(q.symbol))
    tmpl.baseSymbol.textContent = b.symbol.toUpperCase()
    tmpl.quoteSymbol.textContent = q.symbol.toUpperCase()
    tmpl.host.textContent = report.program.host
    this.updateProgramDiv(tmpl, report)
    this.programs[report.programID] = Object.assign({ tmpl, div }, report)
    return div
  }

  authedRoute (handler: (pw: string) => Promise<void>): () => void {
    return async () => {
      if (State.passwordIsCached()) return await handler('')
      this.pwHandler = handler
      await this.showForm(this.page.pwForm)
    }
  }

  updateProgramDiv (tmpl: Record<string, PageElement>, report: BotReport): void {
    const pgm = report.program
    Doc.hide(tmpl.programRunning, tmpl.programPaused)
    if (report.running) Doc.show(tmpl.programRunning)
    else Doc.show(tmpl.programPaused)
    tmpl.lots.textContent = String(pgm.lots)
    tmpl.boost.textContent = `${(pgm.spreadMultiplier * 100).toFixed(1)}%`
    tmpl.driftTolerance.textContent = `${(pgm.driftTolerance * 100).toFixed(2)}%`
    tmpl.oracleWeight.textContent = `${(pgm.oracleWeighting * 100).toFixed(0)}%`
    tmpl.oracleBias.textContent = `${(pgm.oracleBias * 100).toFixed(1)}%`
  }

  setMarketSubchoice (host: string, name: string): void {
    if (host !== this.currentMarket.host || name !== this.currentMarket.name) this.leaveEditMode()
    this.currentMarket = Object.assign({ host }, app().exchanges[host].markets[name])
  }

  createOptsUpdated (): void {
    const opts = this.createOpts
    if (opts.oracleWeighting) Doc.show(this.biasOpt.node)
    else Doc.hide(this.biasOpt.node)
  }

  handleBotNote (n: BotNote): void {
    const page = this.page
    const r = n.report
    switch (n.topic) {
      case 'BotCreated':
        page.runningPrograms.prepend(this.programDiv(r))
        this.setProgramsHeader()
        break
      case 'BotRetired':
        this.programs[r.programID].div.remove()
        delete this.programs[r.programID]
        this.setProgramsHeader()
        break
      default: {
        const p = this.programs[r.programID]
        Object.assign(p, r)
        this.updateProgramDiv(p.tmpl, r)
      }
    }
  }

  setCreationBase (symbol: string): void {
    const counterAsset = this.currentMarket.quotesymbol
    const markets = sortedMarkets()
    const options: HostedMarket[] = []
    // Best option: find an exact match.
    for (const mkt of markets) if (mkt.basesymbol === symbol && mkt.quotesymbol === counterAsset) options.push(mkt)
    // Next best option: same assets, reversed order.
    for (const mkt of markets) if (mkt.quotesymbol === symbol && mkt.basesymbol === counterAsset) options.push(mkt)
    // If we have exact matches, we're done.
    if (options.length > 0) return this.setMarket(options)
    // No exact matches. Must have selected a ghost-class market. Next best
    // option will be the first market where the selected asset is a base asset.
    for (const mkt of markets) if (mkt.basesymbol === symbol) return this.setMarket([mkt])
    // Last option: Market where this is the quote asset.
    for (const mkt of markets) if (mkt.quotesymbol === symbol) return this.setMarket([mkt])
  }

  setCreationQuote (symbol: string): void {
    const counterAsset = this.currentMarket.basesymbol
    const markets = sortedMarkets()
    const options: HostedMarket[] = []
    for (const mkt of markets) if (mkt.quotesymbol === symbol && mkt.basesymbol === counterAsset) options.push(mkt)
    for (const mkt of markets) if (mkt.basesymbol === symbol && mkt.quotesymbol === counterAsset) options.push(mkt)
    if (options.length > 0) return this.setMarket(options)
    for (const mkt of markets) if (mkt.quotesymbol === symbol) return this.setMarket([mkt])
    for (const mkt of markets) if (mkt.basesymbol === symbol) return this.setMarket([mkt])
  }

  async createBot (appPW: string): Promise<void> {
    const { page, currentMarket } = this
    Doc.hide(page.createErr)
    const lots = parseInt(page.lotsInput.value ?? '0')
    if (lots === 0) return
    const makerProgram = Object.assign({
      host: currentMarket.host,
      baseID: currentMarket.baseid,
      quoteID: currentMarket.quoteid
    }, this.createOpts, { lots })

    const req = {
      botType: 'MakerV0',
      program: makerProgram,
      programID: 0,
      appPW: appPW
    }

    let endpoint = '/api/createbot'

    if (this.editProgram !== null) {
      req.programID = this.editProgram.programID
      endpoint = '/api/updatebotprogram'
    }

    const loaded = app().loading(page.botCreator)
    const res = await postJSON(endpoint, req)
    loaded()

    if (!app().checkResponse(res, true)) {
      page.createErr.textContent = res.msg
      Doc.show(page.createErr)
      return
    }

    this.leaveEditMode()

    page.lotsInput.value = ''
  }
}

function sortedMarkets (): HostedMarket[] {
  const mkts: HostedMarket[] = []
  const convertMarkets = (xc: Exchange): HostedMarket[] => {
    return Object.values(xc.markets).map((mkt: Market) => Object.assign({ host: xc.host }, mkt))
  }
  for (const xc of Object.values(app().user.exchanges)) mkts.push(...convertMarkets(xc))
  mkts.sort((a: Market, b: Market) => {
    if (!a.spot) {
      if (!b.spot) return a.name.localeCompare(b.name)
      return -1
    }
    if (!b.spot) return 1
    // Sort by lots.
    return b.spot.vol24 / b.lotsize - a.spot.vol24 / a.lotsize
  })
  return mkts
}

function createOption (opt: BaseOption): OrderOption {
  return {
    key: opt.key,
    displayname: opt.displayname,
    description: opt.description,
    default: opt.default,
    max: opt.max,
    min: opt.min,
    noecho: false,
    isboolean: false,
    isdate: false,
    disablewhenactive: false,
    isBirthdayConfig: false,
    noauth: false
  }
}

function createXYRange (baseOpt: BaseOption, xyRange: XYRange): OrderOption {
  const opt = createOption(baseOpt)
  opt.xyRange = xyRange
  return opt
}
