import State from './state'
import { postJSON } from './http'

type Locale = Record<string, string>

export const ID_NO_PASS_ERROR_MSG = 'NO_PASS_ERROR_MSG'
export const ID_NO_APP_PASS_ERROR_MSG = 'NO_APP_PASS_ERROR_MSG'
export const ID_SET_BUTTON_BUY = 'SET_BUTTON_BUY'
export const ID_SET_BUTTON_SELL = 'SET_BUTTON_SELL'
export const ID_OFF = 'OFF'
export const ID_MAX = 'MAX'
export const ID_READY = 'READY'
export const ID_NO_WALLET = 'NO_WALLET'
export const ID_DISABLED_MSG = 'DISABLED_MSG'
export const ID_WALLET_SYNC_PROGRESS = 'WALLET_SYNC_PROGRESS'
export const ID_HIDE_ADDITIONAL_SETTINGS = 'HIDE_ADDITIONAL_SETTINGS'
export const ID_SHOW_ADDITIONAL_SETTINGS = 'SHOW_ADDITIONAL_SETTINGS'
export const ID_BUY = 'BUY'
export const ID_SELL = 'SELL'
export const ID_NOT_SUPPORTED = 'NOT_SUPPORTED'
export const ID_VERSION_NOT_SUPPORTED = 'VERSION_NOT_SUPPORTED'
export const ID_CONNECTION_FAILED = 'CONNECTION_FAILED'
export const ID_ORDER_PREVIEW = 'ORDER_PREVIEW'
export const ID_CALCULATING = 'CALCULATING'
export const ID_ESTIMATE_UNAVAILABLE = 'ESTIMATE_UNAVAILABLE'
export const ID_NO_ZERO_RATE = 'NO_ZERO_RATE'
export const ID_NO_ZERO_QUANTITY = 'NO_ZERO_QUANTITY'
export const ID_TRADE = 'TRADE'
export const ID_NO_ASSET_WALLET = 'NO_ASSET_WALLET'
export const ID_EXECUTED = 'EXECUTED'
export const ID_BOOKED = 'BOOKED'
export const ID_CANCELING = 'CANCELING'
export const ID_PASSWORD_NOT_MATCH = 'PASSWORD_NOT_MATCH'
export const ID_ACCT_UNDEFINED = 'ACCT_UNDEFINED'
export const ID_KEEP_WALLET_PASS = 'KEEP_WALLET_PASS'
export const ID_NEW_WALLET_PASS = 'NEW_WALLET_PASS'
export const ID_LOT = 'LOT'
export const ID_LOTS = 'LOTS'
export const ID_UNKNOWN = 'UNKNOWN'
export const ID_EPOCH = 'EPOCH'
export const ID_ORDER_SUBMITTING = 'ORDER_SUBMITTING'
export const ID_SETTLING = 'SETTLING'
export const ID_NO_MATCH = 'NO_MATCH'
export const ID_CANCELED = 'CANCELED'
export const ID_REVOKED = 'REVOKED'
export const ID_WAITING_FOR_CONFS = 'WAITING_FOR_CONFS'
export const ID_NONE_SELECTED = 'NONE_SELECTED'
export const ID_REGISTRATION_FEE_SUCCESS = 'REGISTRATION_FEE_SUCCESS'
export const ID_API_ERROR = 'API_ERROR'
export const ID_ADD = 'ADD'
export const ID_CREATE = 'CREATE'
export const ID_SETUP_WALLET = 'SETUP_WALLET'
export const ID_WALLET_READY = 'WALLET_READY'
export const ID_CHANGE_WALLET_TYPE = 'CHANGE_WALLET_TYPE'
export const ID_KEEP_WALLET_TYPE = 'KEEP_WALLET_TYPE'
export const ID_WALLET_PENDING = 'WALLET_PENDING'
export const ID_SETUP_NEEDED = 'SETUP_NEEDED'
export const ID_SEND_SUCCESS = 'SEND_SUCCESS'
export const ID_RECONFIG_SUCCESS = 'RECONFIG_SUCCESS'
export const ID_RESCAN_STARTED = 'RESCAN_STARTED'
export const ID_NEW_WALLET_SUCCESS = 'NEW_WALLET_SUCCESS'
export const ID_WALLET_UNLOCKED = 'WALLET_UNLOCKED'
export const ID_SELLING = 'SELLING'
export const ID_BUYING = 'BUYING'
export const ID_WALLET_DISABLED_MSG = 'WALLET_DISABLED'
export const ID_WALLET_ENABLED_MSG = 'WALLET_ENABLED'
export const ID_ACTIVE_ORDERS_ERR_MSG = 'ACTIVE_ORDERS_ERR_MSG'
export const ID_AVAILABLE = 'AVAILABLE'
export const ID_LOCKED = 'LOCKED'
export const ID_IMMATURE = 'IMMATURE'
export const ID_FEE_BALANCE = 'FEE_BALANCE'
export const ID_CANDLES_LOADING = 'CANDLES_LOADING'
export const ID_DEPTH_LOADING = 'DEPTH_LOADING'
export const ID_INVALID_ADDRESS_MSG = 'INVALID_ADDRESS_MSG'
export const ID_TXFEE_UNSUPPORTED = 'TXFEE_UNSUPPORTED'
export const ID_TXFEE_ERR_MSG = 'TXFEE_ERR_MSG'
export const ID_ACTIVE_ORDERS_LOGOUT_ERR_MSG = 'ACTIVE_ORDERS_LOGOUT_ERR_MSG'
export const ID_INVALID_DATE_ERR_MSG = 'INVALID_DATE_ERR_MSG'
export const ID_NO_ARCHIVED_RECORDS = 'NO_ARCHIVED_RECORDS'
export const ID_DELETE_ARCHIVED_RECORDS_RESULT = 'DELETE_ARCHIVED_RECORDS_RESULT'
export const ID_ARCHIVED_RECORDS_PATH = 'ARCHIVED_RECORDS_PATH'
export const ID_DEFAULT = 'DEFAULT'
export const ID_ADDED = 'USER_ADDED'
export const ID_DISCOVERED = 'DISCOVERED'
export const ID_UNSUPPORTED_ASSET_INFO_ERR_MSG = 'UNSUPPORTED_ASSET_INFO_ERR_MSG'
export const ID_LIMIT_ORDER = 'LIMIT_ORDER'
export const ID_LIMIT_ORDER_IMMEDIATE_TIF = 'LIMIT_ORDER_IMMEDIATE_TIF'
export const ID_MARKET_ORDER = 'MARKET_ORDER'
export const ID_MATCH_STATUS_NEWLY_MATCHED = 'MATCH_STATUS_NEWLY_MATCHED'
export const ID_MATCH_STATUS_MAKER_SWAP_CAST = 'MATCH_STATUS_MAKER_SWAP_CAST'
export const ID_MATCH_STATUS_TAKER_SWAP_CAST = 'MATCH_STATUS_TAKER_SWAP_CAST'
export const ID_MATCH_STATUS_MAKER_REDEEMED = 'MATCH_STATUS_MAKER_REDEEMED'
export const ID_MATCH_STATUS_REDEMPTION_SENT = 'MATCH_STATUS_REDEMPTION_SENT'
export const ID_MATCH_STATUS_REDEMPTION_CONFIRMED = 'MATCH_REDEMPTION_CONFIRMED'
export const ID_MATCH_STATUS_REVOKED = 'MATCH_STATUS_REVOKED'
export const ID_MATCH_STATUS_REFUNDED = 'MATCH_STATUS_REFUNDED'
export const ID_MATCH_STATUS_REFUND_PENDING = 'MATCH_STATUS_REFUND_PENDING'
export const ID_MATCH_STATUS_REDEEM_PENDING = 'MATCH_STATUS_REDEEM_PENDING'
export const ID_MATCH_STATUS_COMPLETE = 'MATCH_STATUS_COMPLETE'
export const ID_TAKER_FOUND_MAKER_REDEMPTION = 'TAKER_FOUND_MAKER_REDEMPTION'
export const ID_OPEN_WALLET_ERR_MSG = 'OPEN_WALLET_ERR_MSG'
export const ID_ORDER_ACCELERATION_FEE_ERR_MSG = 'ORDER_ACCELERATION_FEE_ERR_MSG'
export const ID_ORDER_ACCELERATION_ERR_MSG = 'ORDER_ACCELERATION_ERR_MSG'
export const ID_CONNECTED = 'CONNECTED'
export const ID_DISCONNECTED = 'DISCONNECTED'
export const ID_INVALID_CERTIFICATE = 'INVALID_CERTIFICATE'
export const ID_CONFIRMATIONS = 'CONFIRMATIONS'
export const ID_TAKER = 'TAKER'
export const ID_MAKER = 'MAKER'
export const ID_EMPTY_DEX_ADDRESS_MSG = 'EMPTY_DEX_ADDRESS_MSG'
export const ID_SELECT_WALLET_FOR_FEE_PAYMENT = 'SELECT_WALLET_FOR_FEE_PAYMENT'
export const ID_UNAVAILABLE = 'UNAVAILABLE'
export const ID_WALLET_SYNC_FINISHING_UP = 'WALLET_SYNC_FINISHING_UP'
export const ID_CONNECT_WALLET_ERR_MSG = 'CONNECTING_WALLET_ERR_MSG'
export const ID_REFUND_IMMINENT = 'REFUND_IMMINENT'
export const ID_REFUND_WILL_HAPPEN_AFTER = 'REFUND_WILL_HAPPEN_AFTER'
export const ID_AVAILABLE_TITLE = 'AVAILABLE_TITLE'
export const ID_LOCKED_TITLE = 'LOCKED_TITLE'
export const ID_IMMATURE_TITLE = 'IMMATURE_TITLE'
export const ID_SWAPPING = 'SWAPPING'
export const ID_BONDED = 'BONDED'
export const ID_LOCKED_BAL_MSG = 'LOCKED_BAL_MSG'
export const ID_IMMATURE_BAL_MSG = 'IMMATURE_BAL_MSG'
export const ID_LOCKED_SWAPPING_BAL_MSG = 'LOCKED_SWAPPING_BAL_MSG'
export const ID_LOCKED_BOND_BAL_MSG = 'LOCKED_BOND_BAL_MSG'
export const ID_RESERVES_DEFICIT = 'RESERVES_DEFICIT'
export const ID_RESERVES_DEFICIT_MSG = 'RESERVES_DEFICIT_MSG'
export const ID_BOND_RESERVES = 'BOND_RESERVES'
export const ID_BOND_RESERVES_MSG = 'BOND_RESERVES_MSG'
export const ID_SHIELDED = 'SHIELDED'
export const ID_TRANSPARENT = 'TRANSPARENT'
export const ID_SHIELDED_MSG = 'SHIELDED_MSG'
export const ID_ORDER = 'ORDER'
export const ID_LOCKED_ORDER_BAL_MSG = 'LOCKED_ORDER_BAL_MSG'
export const ID_CREATING_WALLETS = 'CREATING_WALLETS'
export const ID_ADDING_SERVERS = 'ADDING_SERVER'
export const ID_WALLET_RECOVERY_SUPPORT_MSG = 'WALLET_RECOVERY_SUPPORT_MSG'
export const ID_TICKETS_PURCHASED = 'TICKETS_PURCHASED'
export const ID_TICKET_STATUS_UNKNOWN = 'TICKET_STATUS_UNKNOWN'
export const ID_TICKET_STATUS_UNMINED = 'TICKET_STATUS_UNMINED'
export const ID_TICKET_STATUS_IMMATURE = 'TICKET_STATUS_IMMATURE'
export const ID_TICKET_STATUS_LIVE = 'TICKET_STATUS_LIVE'
export const ID_TICKET_STATUS_VOTED = 'TICKET_STATUS_VOTED'
export const ID_TICKET_STATUS_MISSED = 'TICKET_STATUS_MISSED'
export const ID_TICKET_STATUS_EXPIRED = 'TICKET_STATUS_EXPIRED'
export const ID_TICKET_STATUS_UNSPENT = 'TICKET_STATUS_UNSPENT'
export const ID_TICKET_STATUS_REVOKED = 'TICKET_STATUS_REVOKED'
export const ID_INVALID_SEED = 'INVALID_SEED'
export const ID_PASSWORD_RESET_SUCCESS_MSG = 'PASSWORD_RESET_SUCCESS_MSG '
export const ID_BROWSER_NTFN_ENABLED = 'BROWSER_NTFN_ENABLED'
export const ID_BROWSER_NTFN_ORDERS = 'BROWSER_NTFN_ORDERS'
export const ID_BROWSER_NTFN_MATCHES = 'BROWSER_NTFN_MATCHES'
export const ID_BROWSER_NTFN_BONDS = 'BROWSER_NTFN_BONDS'
export const ID_BROWSER_NTFN_CONNECTIONS = 'BROWSER_NTFN_CONNECTIONS'
export const ID_ORDER_BUTTON_BUY_BALANCE_ERROR = 'ORDER_BUTTON_BUY_BALANCE_ERROR'
export const ID_ORDER_BUTTON_SELL_BALANCE_ERROR = 'ORDER_BUTTON_SELL_BALANCE_ERROR'
export const ID_ORDER_BUTTON_QTY_ERROR = 'ORDER_BUTTON_QTY_ERROR'
export const ID_ORDER_BUTTON_QTY_RATE_ERROR = 'ORDER_BUTTON_QTY_RATE_ERROR'
export const ID_CREATE_ASSET_WALLET_MSG = 'CREATE_ASSET_WALLET_MSG'
export const ID_NO_WALLET_MSG = 'NO_WALLET_MSG'
export const ID_TRADING_TIER_UPDATED = 'TRADING_TIER_UPDATED'
export const ID_INVALID_TIER_VALUE = 'INVALID_TIER_VALUE'
export const ID_INVALID_COMPS_VALUE = 'INVALID_COMPS_VALUE'
export const ID_TX_TYPE_UNKNOWN = 'TX_TYPE_UNKNOWN'
export const ID_TX_TYPE_SEND = 'TX_TYPE_SEND'
export const ID_TX_TYPE_RECEIVE = 'TX_TYPE_RECEIVE'
export const ID_TX_TYPE_SWAP = 'TX_TYPE_SWAP'
export const ID_TX_TYPE_REDEEM = 'TX_TYPE_REDEEM'
export const ID_TX_TYPE_REFUND = 'TX_TYPE_REFUND'
export const ID_TX_TYPE_SPLIT = 'TX_TYPE_SPLIT'
export const ID_TX_TYPE_CREATE_BOND = 'TX_TYPE_CREATE_BOND'
export const ID_TX_TYPE_REDEEM_BOND = 'TX_TYPE_REDEEM_BOND'
export const ID_TX_TYPE_APPROVE_TOKEN = 'TX_TYPE_APPROVE_TOKEN'
export const ID_TX_TYPE_ACCELERATION = 'TX_TYPE_ACCELERATION'
export const ID_TX_TYPE_SELF_TRANSFER = 'TX_TYPE_SELF_TRANSFER'
export const ID_TX_TYPE_REVOKE_TOKEN_APPROVAL = 'TX_TYPE_REVOKE_TOKEN_APPROVAL'
export const ID_TX_TYPE_TICKET_PURCHASE = 'TX_TYPE_TICKET_PURCHASE'
export const ID_TX_TYPE_TICKET_VOTE = 'TX_TYPE_TICKET_VOTE'
export const ID_TX_TYPE_TICKET_REVOCATION = 'TX_TYPE_TICKET_REVOCATION'
export const ID_MISSING_CEX_CREDS = 'MISSING_CEX_CREDS'
export const ID_MATCH_BUFFER = 'MATCH_BUFFER'
export const ID_NO_PLACEMENTS = 'NO_PLACEMENTS'
export const ID_INVALID_VALUE = 'INVALID_VALUE'
export const ID_NO_ZERO = 'NO_ZERO'
export const ID_BOTTYPE_BASIC_MM = 'BOTTYPE_BASIC_MM'
export const ID_BOTTYPE_ARB_MM = 'BOTTYPE_ARB_MM'
export const ID_BOTTYPE_SIMPLE_ARB = 'BOTTYPE_SIMPLE_ARB'
export const ID_NO_BOTTYPE = 'NO_BOTTYPE'
export const ID_NO_CEX = 'NO_CEX'
export const ID_CEXBALANCE_ERR = 'CEXBALANCE_ERR'
export const ID_PENDING = 'PENDING'
export const ID_COMPLETE = 'COMPLETE'
export const ID_ARCHIVED_SETTINGS = 'ARCHIVED_SETTINGS'

let locale: Locale

export async function loadLocale (lang: string, commitHash: string, skipCache: boolean) {
  if (!skipCache) {
    const specs = State.fetchLocal(State.localeSpecsKey)
    if (specs && specs.lang === lang && specs.commitHash === commitHash) {
      locale = State.fetchLocal(State.localeKey)
      return
    }
  }
  locale = await postJSON('/api/locale', lang)
  State.storeLocal(State.localeSpecsKey, { lang, commitHash })
  State.storeLocal(State.localeKey, locale)
}

/* prep will format the message to the current locale. */
export function prep (k: string, args?: Record<string, string>) {
  return stringTemplateParser(locale[k], args || {})
}

window.clearLocale = () => {
  State.removeLocal(State.localeSpecsKey)
  State.removeLocal(State.localeKey)
}

/*
 * stringTemplateParser is a template string matcher, where expression is any
 * text. It switches what is inside double brackets (e.g. 'buy {{ asset }}')
 * for the value described into args. args is an object with keys
 * equal to the placeholder keys. (e.g. {"asset": "dcr"}).
 * So that will be switched for: 'asset dcr'.
 */
function stringTemplateParser (expression: string, args: Record<string, string>) {
  // templateMatcher matches any text which:
  // is some {{ text }} between two brackets, and a space between them.
  // It is global, therefore it will change all occurrences found.
  // text can be anything, but brackets '{}' and space '\s'
  const templateMatcher = /{{\s?([^{}\s]*)\s?}}/g
  return expression.replace(templateMatcher, (_, value) => args[value])
}
