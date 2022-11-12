import Doc from '../doc'
import BasePage from '../basepage'
import {
  PageElement,
  UnitInfo
} from '../registry'
import { State } from './extstate'
import { serverAddress } from './extregistry'
import { Loader } from './loader'
import { SendRequest } from './appconnector'
import { BlobLoader } from './blobloader'
import { postJSON } from '../http'

interface WalletPageArgs {
  assetID: number
  sendRequest?: SendRequest
}

interface Confirming {
  addr: string
  amt: number // atomic
}

const traitTxFeeEstimator = 1 << 10

export class WalletPage extends BasePage {
  page: Record<string, PageElement>
  assetID: number
  confirming: Confirming

  constructor (main: PageElement, args: WalletPageArgs) {
    super()
    this.assetID = args.assetID
    const u = State.cachedUser
    const a = u.assets[args.assetID]
    const page = this.page = Doc.idDescendants(main)

    Doc.cleanTemplates(page.balanceTemplate)
    page.logo.src = serverAddress() + `/img/coins/${a.symbol}.png`
    page.name.textContent = a.name
    page.assetSettingsName.textContent = a.name
    this.updateBalances()

    Doc.bind(page.back, 'click', () => Loader.loadPage('wallets'))
    Doc.bind(page.walletSettings, 'click', () => window.open(serverAddress() + `/wallets?asset_id=${a.id}`, '_blank'))

    const setTab = (tab: PageElement, el: PageElement) => {
      for (const t of page.tabs.children) t.classList.remove('selected')
      tab.classList.add('selected')
      Doc.hide(page.sendPane, page.balanceBreakdown)
      Doc.show(el)
    }
    Doc.bind(page.balancesTab, 'click', () => setTab(page.balancesTab, page.balanceBreakdown))
    Doc.bind(page.sendTab, 'click', () => setTab(page.sendTab, page.sendPane))
    Doc.bind(page.sendBttn, 'click', () => this.sendClicked())
    Doc.bind(page.vSend, 'click', () => this.send())

    if (args.sendRequest) this.processSendRequest(args.sendRequest)
  }

  updateBalances () {
    const u = State.cachedUser
    const a = u.assets[this.assetID]
    const { unitInfo, wallet: { balance: b } } = a
    const convFactor = a.unitInfo.conventional.conversionFactor
    const page = this.page
    page.assetBalance.textContent = Doc.formatCompact(b.available / convFactor)
    page.unit.textContent = unitInfo.conventional.unit
    const xcRate = u.fiatRates[a.id]
    if (xcRate) {
      Doc.show(page.fiatRow)
      page.fiatBalance.textContent = Doc.formatCompact(b.available / convFactor * xcRate, 2)
    } else Doc.hide(page.fiatRow)

    const addBalance = (lbl: string, atoms: number) => {
      const row = page.balanceTemplate.cloneNode(true) as PageElement
      page.balanceRows.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      tmpl.label.textContent = lbl
      if (xcRate) tmpl.fiatBalance.textContent = Doc.formatCompact(atoms / convFactor * xcRate)
      tmpl.balance.textContent = Doc.formatCoinValue(atoms, unitInfo)
      tmpl.unit.textContent = unitInfo.conventional.unit
    }

    Doc.empty(page.balanceRows)
    addBalance('Available', b.available)
    addBalance('Locked (orders)', b.orderlocked)
    addBalance('Locked (other)', b.locked - b.orderlocked)
    if (b.immature > 0) addBalance('Immature', b.immature)
    for (const [lbl, atoms] of Object.entries(b.other ?? {})) if (atoms > 0) addBalance(lbl, atoms)
  }

  processSendRequest (req: SendRequest) {
    this.confirmSend(req.addr, req.amt)
  }

  async confirmSend (addr: string, amt: number /* atomic units */) {
    this.confirming = { addr, amt }
    const u = State.cachedUser
    const a = u.assets[this.assetID]
    const { unitInfo: ui, wallet: { balance: bal, traits }, symbol } = a
    const page = this.page
    Doc.hide(page.vSendErr, page.sendErr, page.vSendEstimates, page.txFeeNotAvailable)
    const assetID = parseInt(page.sendForm.dataset.assetID || '')
    const subtract = page.subtractCheckBox.checked || false
    if (addr === '') return Doc.showFormError(page.sendErr, 'invalid address')

    // txfee will not be available if wallet is not a fee estimator or the
    // request failed.
    let txfee = 0
    const invalidAddrMsg = 'invalid address: ' + addr
    if ((traits & traitTxFeeEstimator) !== 0) {
      const open = {
        addr: page.sendAddr.value,
        assetID: assetID,
        subtract: subtract,
        value: amt
      }
      const loader = new BlobLoader(page.sendForm)
      const res = await postJSON('/api/txfee', open)
      loader.stop()
      if (!checkResponse(res)) {
        page.txFeeNotAvailable.dataset.tooltip = 'fee estimation failed: ' + res.msg
        Doc.show(page.txFeeNotAvailable)
        // We still want to ensure user address is valid before proceeding to send
        // confirm form if there's an error while calculating the transaction fee.
        const valid = await this.validateSendAddress(addr, assetID)
        if (!valid) return Doc.showFormError(page.sendErr, invalidAddrMsg)
      } else if (res.ok) {
        if (!res.validaddress) return Doc.showFormError(page.sendErr, invalidAddrMsg)
        txfee = res.txfee
        Doc.show(page.vSendEstimates)
      }
    } else {
      // Validate only the send address for assets that are not fee estimators.
      const valid = await this.validateSendAddress(addr, assetID)
      if (!valid) return Doc.showFormError(page.sendErr, invalidAddrMsg)
    }

    page.vSendSymbol.textContent = symbol.toUpperCase()
    page.vSendLogo.src = Doc.logoPath(symbol)

    page.vSendFee.textContent = Doc.formatFullPrecision(txfee, ui)
    this.showFiatValue(assetID, txfee, page.vSendFeeFiat, ui)
    page.vSendDestinationAmt.textContent = Doc.formatFullPrecision(amt - txfee, ui)
    page.vTotalSend.textContent = Doc.formatFullPrecision(amt, ui)
    this.showFiatValue(assetID, amt, page.vTotalSendFiat, ui)
    page.vSendAddr.textContent = page.sendAddr.value || ''
    const remain = bal.available - amt
    page.balanceAfterSend.textContent = Doc.formatFullPrecision(remain, ui)
    this.showFiatValue(assetID, remain, page.balanceAfterSendFiat, ui)
    Doc.show(page.approxSign)
    if (!subtract) {
      Doc.hide(page.approxSign)
      page.vSendDestinationAmt.textContent = Doc.formatFullPrecision(amt, ui)
      const totalSend = amt + txfee
      page.vTotalSend.textContent = Doc.formatFullPrecision(totalSend, ui)
      this.showFiatValue(assetID, totalSend, page.vTotalSendFiat, ui)
      const remain = bal.available - totalSend
      // handle edge cases where bal is not enough to cover totalSend.
      // we don't want a minus display of user bal.
      if (remain <= 0) {
        page.balanceAfterSend.textContent = Doc.formatFullPrecision(0, ui)
        this.showFiatValue(assetID, 0, page.balanceAfterSendFiat, ui)
      } else {
        page.balanceAfterSend.textContent = Doc.formatFullPrecision(remain, ui)
        this.showFiatValue(assetID, remain, page.balanceAfterSendFiat, ui)
      }
    }
    Doc.hide(page.sendForm)
    this.showOverlay(page.sendConfirm)
  }

  sendClicked () {
    const { assetID, page } = this
    const { unitInfo: ui } = State.cachedUser.assets[assetID]
    const convFactor = ui.conventional.conversionFactor
    const amt = Math.round(parseFloat(page.sendAmt.value ?? '') * convFactor)
    const addr = page.sendAddr.value || ''
    this.confirmSend(addr, amt)
  }

  async validateSendAddress (addr: string, assetID: number): Promise<boolean> {
    const resp = await postJSON('/api/validateaddress', { addr: addr, assetID: assetID })
    return checkResponse(resp)
  }

  showFiatValue (assetID: number, amount: number, display: PageElement, ui: UnitInfo): void {
    const rate = State.cachedUser.fiatRates[assetID]
    if (rate) {
      display.textContent = Doc.formatFiatConversion(amount, rate, ui)
      Doc.show(display.parentElement as Element)
    } else Doc.hide(display.parentElement as Element)
  }

  async send (): Promise<void> {
    const { page, assetID, confirming: { addr, amt } } = this
    const subtract = page.subtractCheckBox.checked ?? false
    const pw = page.vSendPw.value || ''
    page.vSendPw.value = ''
    if (pw === '') {
      Doc.showFormError(page.vSendErr, 'password cannot be empty')
      return
    }
    const open = {
      assetID: assetID,
      address: addr,
      subtract: subtract,
      value: amt,
      pw: pw
    }
    const loader = new BlobLoader(page.vSendForm)
    const res = await postJSON('/api/send', open)
    loader.stop()
    if (!checkResponse(res)) {
      Doc.showFormError(page.vSendErr, res.msg)
      return
    }
    this.hideOverlays()
  }

  showOverlay (overlay: PageElement) {
    overlay.style.left = '0%'
  }

  hideOverlays () {
    Doc.hide(this.page.sendConfirm)
  }
}

function checkResponse (res: any) {
  return typeof res === 'object' && res !== null && (!res.ok || !res.requestSuccessful)
}
