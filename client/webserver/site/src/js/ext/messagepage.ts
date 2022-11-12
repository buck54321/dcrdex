import Doc from '../doc'
import BasePage from '../basepage'
import { PageElement } from '../registry'

interface MessageData {
    msg: string
}

export class MessagePage extends BasePage {
  constructor (main: PageElement, data: MessageData) {
    super()
    const page = Doc.idDescendants(main)
    page.msg.textContent = data.msg
  }
}
