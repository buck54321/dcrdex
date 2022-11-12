import { getJSON, postJSON } from '../http'
import {
  User,
  InitStatus
} from '../registry'
import { serverAddress } from './extregistry'

async function get (path: string) {
  const res = await getJSON(serverAddress() + path)
  checkResponse(res)
  return res
}

async function post (path: string, req?: any) {
  const res = await postJSON(serverAddress() + path, req)
  checkResponse(res)
  return res
}

function checkResponse (res: any) {
  if (typeof res === 'object' && res !== null && (!res.ok || !res.requestSuccessful)) throw Error(res.msg)
}

export class Core {
  static async user (): Promise<User> {
    return get('/api/user')
  }

  static async isInited (): Promise<InitStatus> {
    return get('/api/isinitialized')
  }

  static async login (pw: string, rememberPass: boolean): Promise<any> {
    return post('/api/login', { pass: pw, rememberPass: rememberPass })
  }

  static async signOut (): Promise<void> {
    return post('/api/logout')
  }
}
