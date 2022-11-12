import Doc from '../doc'
import BasePage from '../basepage'
import { PageElement } from '../registry'
import * as intl from '../locales'
import { Core } from './core'
import { Loader } from './loader'
import { BlobLoader } from './blobloader'
import { State } from './extstate'

export class LoginPage extends BasePage {
  page: Record<string, PageElement>

  constructor (main: PageElement) {
    super()
    const page = this.page = Doc.idDescendants(main)
    Doc.bind(page.submitBttn, 'click', () => this.submit())
    Doc.bind(page.pw, 'keyup', (e: KeyboardEvent) => {
      if (e.key !== 'Enter' && e.key !== 'NumpadEnter') return
      this.submit()
    })
  }

  focus () {
    this.page.pw.focus()
  }

  async submit () {
    const page = this.page
    Doc.hide(page.errMsg)
    const pw = page.pw.value || ''
    page.pw.value = ''
    const rememberPass = page.rememberPass.checked ?? false
    if (pw === '') {
      page.errMsg.textContent = intl.prep(intl.ID_NO_PASS_ERROR_MSG)
      Doc.show(page.errMsg)
      return
    }
    const loader = new BlobLoader(document.body)
    const res = await Core.login(pw, rememberPass)
    loader.stop()
    if (!res.requestSuccessful) {
      page.errMsg.textContent = res.msg
      Doc.show(page.errMsg)
      return
    }
    await State.fetchUser()
    Loader.loadPage('landing')
  }
}
