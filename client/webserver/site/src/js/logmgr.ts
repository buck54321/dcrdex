interface LogMessage {
  time: string
  msg: string
}

declare global {
    interface Window {
      log: (...args: any) => void
      enableLogger: (loggerID: string, enable: boolean) => void
      recordLogger: (loggerID: string, enable: boolean) => void
      dumpLogger: (loggerID: string) => void
    }
  }

const loggersKey = 'loggers'
const recordersKey = 'recorders'

export class LogManager {
  static loggers: Record<string, boolean>
  // Recorders can record log messages, and then save them to file on request.
  static recorders: Record<string, LogMessage[]>

  static enableLogger (loggerID: string, state: boolean) {
    if (state) LogManager.loggers[loggerID] = true
    else delete LogManager.loggers[loggerID]
    window.localStorage.setItem(loggersKey, JSON.stringify(LogManager.loggers))
    return `${loggerID} logger ${state ? 'enabled' : 'disabled'}`
  }

  static recordLogger (loggerID: string, on: boolean) {
    if (on) LogManager.recorders[loggerID] = []
    else delete LogManager.recorders[loggerID]
    window.localStorage.setItem(recordersKey, JSON.stringify(Object.keys(LogManager.recorders)))
    return `${loggerID} recorder ${on ? 'enabled' : 'disabled'}`
  }

  static dumpLogger (loggerID: string) {
    const record = LogManager.recorders[loggerID]
    if (!record) return `no recorder for logger ${loggerID}`
    const a = document.createElement('a')
    a.href = `data:application/octet-stream;base64,${window.btoa(JSON.stringify(record, null, 4))}`
    a.download = `${loggerID}.json`
    document.body.appendChild(a)
    a.click()
    setTimeout(() => {
      document.body.removeChild(a)
    }, 0)
  }

  /*
   * log prints to the console if a logger has been enabled. Loggers are created
   * implicitly by passing a loggerID to log. i.e. you don't create a logger,
   * you just log to it. Loggers are enabled by invoking a global function,
   * enableLogger(loggerID, onOffBoolean), from the browser's js console. Your
   * choices are stored across sessions. Some common and useful loggers are
   * listed below, but this list is not meant to be comprehensive.
   *
   * LoggerID   Description
   * --------   -----------
   * notes      Notifications of all levels.
   * book       Order book feed.
   * ws.........Websocket connection status changes.
   */
  static log (loggerID: string, ...msg: any) {
    if (LogManager.loggers[loggerID]) console.log(`${nowString()}[${loggerID}]:`, ...msg)
    if (LogManager.recorders[loggerID]) {
      LogManager.recorders[loggerID].push({
        time: nowString(),
        msg: msg
      })
    }
  }

  static run () {
    window.enableLogger = LogManager.enableLogger
    window.recordLogger = LogManager.recordLogger
    window.dumpLogger = LogManager.dumpLogger
    window.log = LogManager.log
  }
}
LogManager.loggers = JSON.parse(window.localStorage.getItem(loggersKey) ?? 'null') ?? {}
LogManager.recorders = {}
for (const loggerID of JSON.parse(window.localStorage.getItem(recordersKey) ?? 'null') ?? []) {
  console.log('recording', loggerID)
  LogManager.recorders[loggerID] = []
}

function nowString (): string {
  const stamp = new Date()
  const h = stamp.getHours().toString().padStart(2, '0')
  const m = stamp.getMinutes().toString().padStart(2, '0')
  const s = stamp.getSeconds().toString().padStart(2, '0')
  const ms = stamp.getMilliseconds().toString().padStart(3, '0')
  return `${h}:${m}:${s}.${ms}`
}
