import Doc from '../doc'
import BasePage from '../basepage'
import { PageElement } from '../registry'
import { serverAddress } from './extregistry'
import { Loader } from './loader'
import { Core } from './core'

export class LandingPage extends BasePage {
  page: Record<string, PageElement>

  constructor (main: PageElement) {
    super()
    const page = this.page = Doc.idDescendants(main)
    Doc.bind(page.lock, 'click', () => this.signOut())
    Doc.bind(page.trade, 'click', () => window.open(serverAddress(), '_blank'))
    Doc.bind(page.wallets, 'click', () => Loader.loadPage('wallets'))
    // Doc.bind(page.history, 'click', () => console.log("histry"))
  }

  async signOut () {
    await Core.signOut()
    Loader.loadPage('login')
  }
}
