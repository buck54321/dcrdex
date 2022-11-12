import { LandingPage } from './landingpage'
import { LoginPage } from './loginpage'
import { MessagePage } from './messagepage'
import { WalletsPage } from './walletspage'
import { WalletPage } from './walletpage'
import { Loader } from './loader'
import { Core } from './core'
import { State } from './extstate'
import ws from '../ws'
import { CoreNote, InitStatus, PageElement } from '../registry'
import { setServerAddress, socketAddress, serverAddress } from './extregistry'
import Doc from '../doc'
import { BlobLoader } from './blobloader'
import { Browser } from '../browser'
import { LogManager } from '../logmgr'

interface RequestFromPage {
  route: string
  payload: any
}

const notificationRoute = 'notify'

const lastConnectionKey = 'lastConnection'

const Mainnet = 0
// const Testnet = 1
// const Simnet = 2

Loader.registerPageHandler('landing', LandingPage)
Loader.registerPageHandler('login', LoginPage)
Loader.registerPageHandler('message', MessagePage)
Loader.registerPageHandler('wallets', WalletsPage)
Loader.registerPageHandler('wallet', WalletPage)

export async function run () {
  LogManager.run()
  const lastConnection = State.fetch(lastConnectionKey) ?? {
    addr: 'http://127.0.0.1:5758',
    net: Mainnet
  }
  setServerAddress(lastConnection.addr)
  bindConnectionPane()
  const [connected, status] = await initStatus()
  if (!connected || !status) {
    showConnectionPane()
    return
  }
  const connectionDetails = {
    addr: serverAddress(),
    net: status.net
  }
  Browser.store(lastConnectionKey, connectionDetails)
  State.store(lastConnectionKey, connectionDetails)
  // Handle WebSocket stuff
  loadLandingPage(status)
}

async function initStatus (): Promise<[boolean, InitStatus | null]> {
  let initStatus: InitStatus
  try {
    initStatus = await Core.isInited()
  } catch (err) {
    return [false, null]
  }
  ws.connect(socketAddress(), () => { /* console.log('reconnected') */ })
  ws.registerRoute(notificationRoute, (n: CoreNote) => State.notify(n))
  return [true, initStatus]
}

async function loadLandingPage (status: InitStatus) {
  if (!status.initialized) {
    Loader.loadPage('message', {
      msg: 'Client is not initialized'
    })
    return
  }
  if (!status.authed) {
    Loader.loadPage('login')
    return
  }
  const u = await State.fetchUser()
  const raw = new URL(window.location.href).searchParams.get('req')
  if (raw) {
    const req = JSON.parse(decodeURIComponent(raw)) as RequestFromPage
    switch (req.route) {
      case 'openwindow':
        console.log(`Extension window opened by ${req.payload}`)
        break
      case 'send': {
        const { assetID, amt, addr } = req.payload
        const w = u.assets[assetID]?.wallet
        if (!w) {
          Loader.loadPage('message', {
            msg: 'No wallet for requested asset'
          })
          return
        }
        Loader.loadPage('wallet', { assetID, sendRequest: { amt, addr } })
      }
    }
  }
  Loader.loadPage('landing')
}

let connectionPane: PageElement

function bindConnectionPane () {
  const page = Doc.idDescendants(document.body)
  connectionPane = page.connectionPane
  const tryAddr = async (addr: string) => {
    Doc.hide(page.errMsg)
    if (!addr.startsWith('http')) addr = 'http://' + addr
    setServerAddress(addr)
    const loader = new BlobLoader(document.body)
    const [connected, status] = await initStatus()
    loader.stop()
    if (!connected || !status) {
      page.errMsg.textContent = `could not connect to '${addr}'`
      Doc.show(page.errMsg)
      return
    }
    const connectionDetails = {
      addr,
      net: status.net
    }
    State.store(lastConnectionKey, connectionDetails)
    Browser.store(lastConnectionKey, connectionDetails)
    Doc.hide(connectionPane)
    loadLandingPage(status)
  }

  Doc.bind(page.mainnetDefault, 'click', () => tryAddr('127.0.0.1:5758'))
  Doc.bind(page.testnetDefault, 'click', () => tryAddr('127.0.0.2:5758'))
  Doc.bind(page.simnetDefault, 'click', () => tryAddr('127.0.0.3:5758'))
  Doc.bind(page.customAddrSubmit, 'click', () => tryAddr(page.customAddr.value ?? '127.0.0.1:5758'))
}

async function showConnectionPane () {
  Doc.show(connectionPane)
}
