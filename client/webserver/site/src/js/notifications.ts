import { CoreNote, PageElement } from './registry'
import * as intl from './locales'
import State from './state'
import { setCoinHref } from './coinexplorers'
import Doc from './doc'

export const IGNORE = 0
export const DATA = 1
export const POKE = 2
export const SUCCESS = 3
export const WARNING = 4
export const ERROR = 5

/*
 * make constructs a new notification. The notification structure is a mirror of
 * the structure of notifications sent from the web server.
 * NOTE: I'm hoping to make this function obsolete, since errors generated in
 * javascript should usually be displayed/cached somewhere better. For example,
 * if the error is generated during submission of a form, the error should be
 * displayed on or near the form itself, not in the notifications.
 */
export function make (subject: string, details: string, severity: number): CoreNote {
  return {
    subject: subject,
    details: details,
    severity: severity,
    stamp: new Date().getTime(),
    acked: false,
    type: 'internal',
    topic: 'internal',
    id: ''
  }
}

const NoteTypeOrder = 'order'
const NoteTypeMatch = 'match'
const NoteTypeBondPost = 'bondpost'
const NoteTypeConnEvent = 'conn'

type BrowserNtfnSettingLabel = {
  [x: string]: string
}

type BrowserNtfnSetting = {
  [x: string]: boolean
}

function browserNotificationsSettingsKey (): string {
  return `browser_notifications-${window.location.host}`
}

export const browserNtfnLabels: BrowserNtfnSettingLabel = {
  [NoteTypeOrder]: intl.ID_BROWSER_NTFN_ORDERS,
  [NoteTypeMatch]: intl.ID_BROWSER_NTFN_MATCHES,
  [NoteTypeBondPost]: intl.ID_BROWSER_NTFN_BONDS,
  [NoteTypeConnEvent]: intl.ID_BROWSER_NTFN_CONNECTIONS
}

export const defaultBrowserNtfnSettings: BrowserNtfnSetting = {
  [NoteTypeOrder]: true,
  [NoteTypeMatch]: true,
  [NoteTypeBondPost]: true,
  [NoteTypeConnEvent]: true
}

let browserNtfnSettings: BrowserNtfnSetting

export function ntfnPermissionGranted () {
  return window.Notification.permission === 'granted'
}

export function ntfnPermissionDenied () {
  return window.Notification.permission === 'denied'
}

export async function requestNtfnPermission () {
  if (!('Notification' in window)) {
    return
  }
  if (Notification.permission === 'granted') {
    showBrowserNtfn(intl.prep(intl.ID_BROWSER_NTFN_ENABLED))
  } else if (Notification.permission !== 'denied') {
    await Notification.requestPermission()
    showBrowserNtfn(intl.prep(intl.ID_BROWSER_NTFN_ENABLED))
  }
}

export function showBrowserNtfn (title: string, body?: string) {
  if (window.Notification.permission !== 'granted') return
  const ntfn = new window.Notification(title, {
    body: body,
    icon: '/img/softened-icon.png'
  })
  return ntfn
}

export function browserNotify (note: CoreNote) {
  if (!browserNtfnSettings[note.type]) return
  showBrowserNtfn(note.subject, plainNote(note.details))
}

export async function fetchBrowserNtfnSettings (): Promise<BrowserNtfnSetting> {
  if (browserNtfnSettings !== undefined) {
    return browserNtfnSettings
  }
  const k = browserNotificationsSettingsKey()
  browserNtfnSettings = (await State.fetchLocal(k) ?? {}) as BrowserNtfnSetting
  return browserNtfnSettings
}

export async function updateNtfnSetting (noteType: string, enabled: boolean) {
  await fetchBrowserNtfnSettings()
  browserNtfnSettings[noteType] = enabled
  State.storeLocal(browserNotificationsSettingsKey(), browserNtfnSettings)
}

const coinExplorerTokenRe = /\{\{\{([^|]+)\|([^}]+)\}\}\}/g
const orderTokenRe = /\{\{\{order\|([^}]+)\}\}\}/g

/*
 * insertRichNote replaces tx and order hash tokens in the input string with
 * <a> elements that link to the asset's chain explorer and order details
 * view, and inserts the resulting HTML into the supplied parent element.
 */
export function insertRichNote (parent: PageElement, inputString: string) {
  const s = inputString.replace(orderTokenRe, (_match, orderToken) => {
    const link = document.createElement('a')
    link.setAttribute('href', '/order/' + orderToken)
    link.setAttribute('class', 'subtlelink')
    link.textContent = orderToken.slice(0, 8)
    return link.outerHTML
  }).replace(coinExplorerTokenRe, (_match, assetID, hash) => {
    const link = document.createElement('a')
    link.setAttribute('data-explorer-coin', hash)
    link.setAttribute('target', '_blank')
    link.textContent = hash.slice(0, 8)
    setCoinHref(assetID, link)
    return link.outerHTML
  })
  const els = Doc.noderize(s).body
  while (els.firstChild) parent.appendChild(els.firstChild)
}

/*
 * plainNote replaces tx and order hash tokens tokens in the input string with
 * shortened hashes, for rendering in browser notifications and popups.
 */
export function plainNote (inputString: string): string {
  const replacedString = inputString.replace(coinExplorerTokenRe, (_match, _assetID, hash) => {
    return hash.slice(0, 8)
  })
  return replacedString
}
