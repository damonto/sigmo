import { ref } from 'vue'

import type { WiFiProxySettings } from '@/types/modem'

export const DEFAULT_WIFI_PROXY_PORT = 1080

const SOCKS_SCHEME = /^socks5h?:\/\//i
const AUTHORITY = /^(?:[a-z0-9._~!$&'()*+,;=:%-]+@)?[a-z0-9.:[\]-]+$/i
const HOST_LABEL = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i
const UTF8 = new TextEncoder()

type WiFiProxyAddress = Omit<WiFiProxySettings, 'name'>

const unbracket = (host: string) =>
  host.startsWith('[') && host.endsWith(']') ? host.slice(1, -1) : host

const isPlainHost = (text: string) => {
  const colon = text.indexOf(':')
  return (
    colon === -1 ||
    (colon !== text.lastIndexOf(':') && /^[a-f0-9:.]+(?:%[\w.-]+)?$/i.test(unbracket(text)))
  )
}

const validCredential = (value: string) =>
  value.length > 0 && !/[\r\n]/.test(value) && UTF8.encode(value).length <= 255

const parseAddress = (text: string): WiFiProxyAddress | undefined => {
  const hasScheme = SOCKS_SCHEME.test(text)
  const authority = hasScheme ? text.replace(SOCKS_SCHEME, '').replace(/\/$/, '') : text
  // URL parsers normalize some malformed authorities instead of rejecting them.
  if (!AUTHORITY.test(authority) || authority.endsWith(':')) return
  const hasCredentials = authority.includes('@')

  try {
    const url = new URL(`socks5://${authority}`)
    const port = url.port ? Number(url.port) : DEFAULT_WIFI_PROXY_PORT
    if (port < 1 || port > 65535 || (!hasScheme && (!url.port || hasCredentials))) return

    const host = unbracket(url.hostname)
    if (!host.includes(':')) {
      const hostname = host.replace(/\.$/, '')
      if (hostname.length > 253 || !hostname.split('.').every((label) => HOST_LABEL.test(label))) {
        return
      }
    }
    const username = decodeURIComponent(url.username)
    const password = decodeURIComponent(url.password)
    if (hasCredentials && (!validCredential(username) || !validCredential(password))) return
    return { host, port, username, password }
  } catch {
    return undefined
  }
}

/** Reads a proxy address from a host field's paste event and tracks invalid address pastes. */
export const useWiFiProxyPaste = () => {
  const invalidPaste = ref(false)
  const clearPasteError = () => {
    invalidPaste.value = false
  }

  const readProxyPaste = (event: ClipboardEvent): WiFiProxyAddress | undefined => {
    clearPasteError()
    const text = event.clipboardData?.getData('text/plain').trim()
    if (!text || isPlainHost(text)) return

    event.preventDefault()
    const address = parseAddress(text)
    invalidPaste.value = address === undefined
    return address
  }

  return { invalidPaste, clearPasteError, readProxyPaste }
}
