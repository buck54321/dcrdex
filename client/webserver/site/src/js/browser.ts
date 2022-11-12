export class Browser {
  static async store (k: string, v: any) {
    if (chrome && chrome.storage) {
      return await new Promise((resolve, reject) => {
        chrome.storage.sync.set({ [k]: JSON.stringify(v) }, () => {
          if (chrome.runtime.lastError) {
            return reject(chrome.runtime.lastError)
          }
          resolve(true)
        })
      })
    }
    // window.localStorage.setItem(k, JSON.stringify(v))
    throw Error('unknown browser')
  }

  static async fetch (k: string) {
    if (chrome && chrome.storage) {
      return await new Promise((resolve, reject) => {
        chrome.storage.sync.get([k], items => {
          if (chrome.runtime.lastError) {
            return reject(chrome.runtime.lastError)
          }
          const v = items[k]
          resolve(typeof v !== 'undefined' ? JSON.parse(v) : null)
        })
      })
    }
    // const v = window.localStorage.getItem(k)
    // if (v !== null) {
    //   return JSON.parse(v)
    // }
    // return null
    throw Error('unknown browser')
  }
}
