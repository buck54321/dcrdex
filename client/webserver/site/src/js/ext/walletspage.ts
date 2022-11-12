import Doc from '../doc'
import BasePage from '../basepage'
import {
  PageElement,
  SupportedAsset
} from '../registry'
import { State } from './extstate'
import { serverAddress } from './extregistry'
import { Loader } from './loader'

export class WalletsPage extends BasePage {
  page: Record<string, PageElement>

  constructor (main: PageElement) {
    super()
    const page = this.page = Doc.idDescendants(main)

    Doc.cleanTemplates(page.assetTemplate)

    const u = State.cachedUser
    const walletAssets: SupportedAsset[] = []
    const nowalletAssets: SupportedAsset[] = []
    for (const asset of Object.values(u.assets)) {
      if (asset.wallet) walletAssets.push(asset)
      else nowalletAssets.push(asset)
    }

    for (const a of walletAssets) {
      const div = page.assetTemplate.cloneNode(true) as PageElement
      page.created.appendChild(div)
      const tmpl = Doc.parseTemplate(div)
      tmpl.logo.src = serverAddress() + `/img/coins/${a.symbol}.png`
      tmpl.name.textContent = a.name
      const b = a.wallet.balance
      const bal = b.available + b.locked + b.immature
      const convVal = bal / a.unitInfo.conventional.conversionFactor
      tmpl.balance.textContent = Doc.formatCompact(convVal)
      tmpl.unit.textContent = a.unitInfo.conventional.unit
      const xcRate = u.fiatRates[a.id]
      if (xcRate) {
        Doc.show(tmpl.fiatRow)
        tmpl.fiat.textContent = Doc.formatCompact(convVal * xcRate, 2)
      } else Doc.hide(tmpl.fiatRow)
      Doc.bind(div, 'click', () => Loader.loadPage('wallet', { assetID: a.id }))
    }
  }
}
