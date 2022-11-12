import Doc from '../doc'
import { State } from './extstate'
import { PageElement, CoreNote } from '../registry'

export class Dialog {
  div: PageElement

  constructor (div: PageElement) {
    this.div = div

    const closer = div.querySelector('.dialog-closer') as PageElement

    Doc.bind(closer, 'click', () => this.close())

    Doc.bind(document, 'keyup', (e: KeyboardEvent) => this.esc(e))
  }

  close () {
    State.removeDialog(this)
  }

  esc (e: KeyboardEvent) {
    if (e.key === 'Escape') this.div.remove()
  }

  unload () {
    Doc.unbind(document, 'keyup', this.esc)
  }

  notify (n: CoreNote) {
    console.warn('unhandled notification', n)
  }
}
