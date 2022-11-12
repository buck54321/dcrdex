import { PageElement, Page, User, CoreNote } from '../registry'
import { Dialog } from './dialog'
import BaseState from '../state'
import { Core } from './core'
import { updateUser } from './extregistry'

export class State extends BaseState {
  static activeDialogs: Dialog[]
  static currentDialog: Dialog | null
  static cachedUser: User
  static currentPage: Page
  static oldMain: PageElement

  static removeDialog (dialog: Dialog) {
    State.activeDialogs = State.activeDialogs.filter(d => d !== dialog)
    dialog.div.remove()
    dialog.unload()
    State.currentDialog = State.activeDialogs.length ? State.activeDialogs[State.activeDialogs.length - 1] : null
  }

  static async fetchUser (): Promise<User> {
    State.cachedUser = await Core.user()
    return State.cachedUser
  }

  static notify (n: CoreNote) {
    if (this.cachedUser) updateUser(this.cachedUser, n)
    if (State.currentPage) State.currentPage.notify(n)
    for (const d of State.activeDialogs) d.notify(n)
  }
}
State.activeDialogs = []
