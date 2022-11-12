import { PipeToBackground } from './docpipe.js'

const pipe = new PipeToBackground()
const page = {}
for (const el of document.querySelectorAll('[id]')) page[el.id] = el

(async () => {
  page.begBttn.addEventListener('click', () => {
    const resp = pipe.request('send', {
      assetID: 42,
      amt: 1e8,
    })
  })

  let extensionStatus
  try {
    extensionStatus = await pipe.request('status', null, 1000)
    if (!extensionStatus) {
      page.errMsg.textContent = 'failed to connect to extension'
      page.errMsg.classList.remove('d-hide')
      return
    }
  } catch (err) {
    page.errMsg.textContent = 'error retrieving extension status'
    page.errMsg.classList.remove('d-hide')
    console.error(err)
    return
  }
})();