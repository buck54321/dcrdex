import { PipeToPage, Request } from './docpipe'
import { Browser } from '../browser'
import ws from '../ws'
import { setServerAddress, socketAddress } from './extregistry'
import { Core } from './core'
import { LogManager } from '../logmgr'

const lastConnectionKey = 'lastConnection'

interface LastConnection {
  addr: string
  net: number
}

export interface InitStatus {
  initialized: boolean
  authed: boolean
  net: number
}

export interface SendRequest {
  assetID: number
  amt: number
  addr: string
}

export class AppConnector {
  appPipe: PipeToPage
  floatingDialog: HTMLElement | null
  initPromise: Promise<InitStatus | null>
  socketPromise: Promise<boolean>

  start () {
    LogManager.run()
    this.appPipe = new PipeToPage(req => this.handleRequestFromPage(req))
    // We don't attempt any connections or storage retrievals until something is
    // requested. This will run on every tab the user opens, so for most people,
    // the vast majority of the time this code will never be used. Keep the
    // initialization load small.
  }

  async prepareConnection (): Promise<InitStatus | null> {
    const lastConnection = await Browser.fetch(lastConnectionKey) as LastConnection
    if (!lastConnection) return null
    setServerAddress(lastConnection.addr)
    try {
      return await Core.isInited()
    } catch (error) {
      console.error(error)
      return null
    }
  }

  async socketIsReady (): Promise<boolean> {
    return new Promise((resolve /* , reject */) => {
      ws.registerRoute('error', (/* err: any */) => {
        // reject(err)
        ws.close('didn\'t connect')
        resolve(false)
      })
      ws.registerRoute('open', () => {
        resolve(true)
      })
      ws.connect(socketAddress(), () => { /* console.log('reloaded') */ })
    })
  }

  async initStatus (): Promise<InitStatus | null> {
    if (!this.initPromise) this.initPromise = this.prepareConnection()
    return this.initPromise
  }

  async handleRequestFromPage (req: Request) {
    switch (req.route) {
      case 'status':
        return this.initStatus()
      case 'openwindow': {
        console.log("--openwindow")
        this.showFloatingDialog(req)
        break
      }
      case 'send':
        return this.showFloatingDialog(req)
    }
  }

  async showFloatingDialog (data: any) {
    if (this.floatingDialog) return
    const dialog = this.floatingDialog = classyElement('div', 'dexc-ext-floatbox')
    const dragOverlay = classyElement('div', 'dexc-ext-drag-overlay')
    dialog.appendChild(dragOverlay)
    const header = classyElement('div', 'dexc-ext-header')
    if (!dialog) return
    dialog.appendChild(header)
    const dragBar = classyElement('div', 'dexc-ext-drag-bar')
    header.appendChild(dragBar)
    const closer = classyElement('div', 'dexc-ext-closer')
    closer.textContent = 'X'
    header.appendChild(closer)

    const url = new URL(chrome.runtime.getURL('/popup.html'))
    url.searchParams.append('req', encodeURIComponent(JSON.stringify(data)))
    const iFrame = classyElement('iframe', 'dexc-ext-iframe') as HTMLIFrameElement
    iFrame.src = url.toString()
    dialog.appendChild(iFrame)

    closer.addEventListener('click', e => {
      e.stopPropagation()
      this.hideFloatingDialog()
    })

    dragBar.addEventListener('mousedown', e => {
      dragOverlay.style.display = 'block'
      document.body.style.userSelect = 'none'
      const startX = e.pageX
      const startY = e.pageY
      const startRight = window.getComputedStyle(dialog, null).getPropertyValue('right')
      const startTop = window.getComputedStyle(dialog, null).getPropertyValue('top')
      let removeListeners = () => { /* placeholder */ }
      const mouseMove = (e: MouseEvent) => {
        // Check that the mouse is still down. They could have released
        // off-screen.
        // https://developer.mozilla.org/en-US/docs/Web/API/MouseEvent/buttons
        if (e.buttons % 2 !== 1) {
          removeListeners()
        }
        const deltaX = startX - e.pageX
        const deltaY = e.pageY - startY
        dialog.style.top = `calc(${startTop} + ${deltaY}px)`
        dialog.style.right = `calc(${startRight} + ${deltaX}px)`
      }
      removeListeners = () => {
        dragOverlay.style.display = 'none'
        document.body.style.userSelect = 'auto'
        document.removeEventListener('mouseup', removeListeners)
        document.removeEventListener('mousemove', mouseMove)
      }
      document.addEventListener('mouseup', removeListeners)
      document.addEventListener('mousemove', mouseMove)
    })
    document.body.appendChild(dialog)
  }

  async hideFloatingDialog () {
    if (this.floatingDialog) this.floatingDialog.remove()
    this.floatingDialog = null
  }
}

function classyElement (elType: string, ...classes: string[]): HTMLElement {
  const el = document.createElement(elType)
  if (classes.length) el.classList.add(...classes)
  return el
}
