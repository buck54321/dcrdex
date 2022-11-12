import Application from './js/app'
import { registerApplication } from './js/registry'
import './css/dexc.scss'

const app = new Application()
registerApplication(app)
app.start()
