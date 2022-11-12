import Doc from '../doc'
import { State } from './extstate'
import { PageElement, PageClass } from '../registry'
import { Dialog } from './dialog'
import { serverAddress } from './extregistry'

interface DialogClass {
  new (main: HTMLElement, data: any): Dialog;
}

const _pageHandlers: Record<string, PageClass> = {}
const _dialogHandlers: Record<string, DialogClass> = {}
let _dialogTemplate: PageElement
let _header: PageElement

export class Loader {
  static registerPageHandler (name: string, handler: PageClass) {
    _pageHandlers[name] = handler
  }

  static registerDialogHandler (name: string, handler: DialogClass) {
    _dialogHandlers[name] = handler
  }

  static async loadDialog (name: string, data: any) {
    _dialogTemplate = _dialogTemplate || await Loader.loadNode('dialog')
    const dialog = _dialogTemplate.cloneNode(true) as PageElement
    dialog.appendChild(await Loader.loadNode(name, data) as Node)
    document.body.appendChild(dialog)
    const dialogHandler = new _dialogHandlers[name](dialog, data)
    State.activeDialogs.push(dialogHandler)
    State.currentDialog = dialogHandler
  }

  static async resolveHeader (main: PageElement) {
    const headerPlaceholder = main.querySelector('header')
    if (headerPlaceholder) {
      if (!_header) {
        _header = await this.loadNode('header')
        const page = Doc.idDescendants(_header)
        Doc.bind(page.dexcLogo, 'click', () => Loader.loadPage('landing'))
        Doc.bind(page.settingsLink, 'click', () => window.open(serverAddress() + '/settings', '_blank'))
      }
      headerPlaceholder.replaceWith(_header)
    }
  }

  static async loadPage (name: string, data?: any) {
    const main = await Loader.loadNode(name, data)
    await Loader.resolveHeader(main)
    if (State.oldMain) State.oldMain.replaceWith(main)
    else document.body.appendChild(main)
    State.oldMain = main

    if (State.currentPage && State.currentPage.unload) State.currentPage.unload()
    State.currentPage = new _pageHandlers[name](main, data)
  }

  static async loadNode (name: string, data?: any): Promise<PageElement> {
    const url = new URL(`/html/${name}.html`, window.location.origin)
    const response = await window.fetch(url.toString())
    if (!response.ok) throw Error(response.statusText)
    const html = parseHTMLTemplate(await response.text(), data || {})
    const main = Doc.noderize(html)

    main.querySelectorAll('a').forEach((link: HTMLAnchorElement) => {
      Doc.bind(link, 'click', () => { link.target = '_blank' })
    })

    main.querySelectorAll('[data-link]').forEach((link: PageElement) => {
      Doc.bind(link, 'click', () => {
        Loader.loadPage(link.dataset.link || '')
      })
    })

    // Doc.bindTooltips(main)

    return main.body.firstChild as PageElement
  }
}

function parseHTMLTemplate (htmlTemplate: string, data: Record<string, string>) {
  // templateMatcher matches any text which:
  // is some {{ text }} between two brackets, and a space between them.
  // It is global, therefore it will change all occurrences found.
  // text can be anything, but brackets '{}' and space '\s'
  const pattern = /{{\s?([^{}\s]*)\s?}}/g
  return htmlTemplate.replace(pattern, (_, value) => data[value])
}
