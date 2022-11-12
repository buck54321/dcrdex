export class Request {
  route: string
  payload: any
  id: string

  constructor (route: string, payload?: any) {
    this.route = route
    this.payload = payload ?? null
    this.id = newCommandID()
  }
}

interface RequestWaiter {
  timeout: number | null
  resolve: (value: unknown) => void
}

class DocumentPipe {
  outID: string
  requestWaiters: Record<string, RequestWaiter>
  messageHandler: (req: Request) => Promise<void>

  constructor (
    inID: string,
    outID: string,
    messageHandler: (req: Request) => Promise<void>
  ) {
    this.outID = outID
    this.messageHandler = messageHandler
    this.requestWaiters = {}
    window.addEventListener(inID, (e: CustomEvent) => {
      this.handleMessage(e.detail)
    }, false)
  }

  send (req: Request) {
    window.dispatchEvent(new CustomEvent(this.outID, { detail: req }))
  }

  async request (route: string, payload?: any) {
    const req = new Request(route, payload ?? null)
    return new Promise((resolve, reject) => {
      this.requestWaiters[req.id] = {
        timeout: window.setTimeout(() => {
          delete this.requestWaiters[req.id]
          reject(new Error('timed out'))
        }, 60_000 * 60 * 5), // 5 minute timeout
        resolve: resolve
      }
      this.send(req)
    })
  }

  async handleMessage (req: Request) {
    // See if this is for a pending request.
    if (req.route === 'response') {
      this.handleResponse(req.id, req.payload)
      return
    }
    // This is an incoming request.
    const respPayload = await this.messageHandler(req)
    const resp = new Request('response', respPayload)
    resp.id = req.id
    this.send(resp)
  }

  handleResponse (reqID: string, payload: any) {
    const waiter = this.requestWaiters[reqID]
    if (waiter) {
      clearTimeout(waiter.timeout || 0)
      waiter.resolve(payload)
      delete this.requestWaiters[reqID]
    }
  }
}

function newCommandID () {
  return Math.random().toString(36).substring(2, 10)
}

const background2page = 'background2page'
const page2background = 'page2background'

export class PipeToBackground extends DocumentPipe {
  constructor (requestHandler?: (req: Request) => Promise<any>) {
    super(background2page, page2background, requestHandler ?? (async () => { /* dummy */ }))
  }
}

export class PipeToPage extends DocumentPipe {
  constructor (requestHandler?: (req: Request) => Promise<any>) {
    super(page2background, background2page, requestHandler ?? (async () => { /* dummy */ }))
  }
}
