import { onMounted, onUnmounted, type Ref } from 'vue'

import { fetchJson } from '@/lib/fetch'
import type { CarrierWebsheetInfo } from '@/types/websheet'

interface CarrierCallback {
  event?: string
  method?: string
  resultCode?: string
}

interface Options {
  iframe: Readonly<Ref<HTMLIFrameElement | null>>
  websheet: () => CarrierWebsheetInfo | null
  open: () => boolean
  closeOnStatusChange: () => boolean
  onDone: () => void
}

const isCarrierCallback = (value: unknown): value is CarrierCallback =>
  typeof value === 'object' &&
  value !== null &&
  !Array.isArray(value) &&
  (!('event' in value) || typeof value.event === 'string') &&
  (!('method' in value) || typeof value.method === 'string') &&
  (!('resultCode' in value) || typeof value.resultCode === 'string') &&
  (('event' in value && !!value.event) ||
    ('method' in value && !!value.method) ||
    ('resultCode' in value && !!value.resultCode))

const isTerminalCallback = (callback: CarrierCallback, closeOnStatusChange: boolean): boolean => {
  const name = (callback.event || callback.method || callback.resultCode || '')
    .replace(/[^a-z0-9]/gi, '')
    .toLowerCase()
  switch (name) {
    case 'deletetoken':
    case 'checkprofileservicestatus':
    case 'profilereadywithactivationcode':
    case 'profilereadywithdefaultsmdp':
      return false
    case 'phoneservicesaccountstatuschanged':
      return closeOnStatusChange
    default:
      return true
  }
}

const relayCallback = async (id: string, callback: CarrierCallback): Promise<boolean> => {
  try {
    await fetchJson<void>(`websheets/${id}/callback`, {
      method: 'POST',
      body: JSON.stringify(callback),
    })
    return true
  } catch (error) {
    console.error('[CarrierWebsheet] Failed to relay callback:', error)
    return false
  }
}

/** Hosts carrier callbacks for the active iframe and cleans up its message listener. */
export const useCarrierWebsheetCallbacks = (options: Options): void => {
  let mounted = false
  let completedSheetID: string | undefined

  const onMessage = async (event: MessageEvent<unknown>) => {
    if (!mounted || !options.open() || event.source !== options.iframe.value?.contentWindow) return
    const id = options.websheet()?.id
    if (!id || id === completedSheetID) return
    const data = event.data
    if (
      typeof data !== 'object' ||
      data === null ||
      !('type' in data) ||
      data.type !== 'sigmo-websheet-callback' ||
      !('callback' in data) ||
      !isCarrierCallback(data.callback)
    ) {
      return
    }
    if ('submitted' in data && typeof data.submitted !== 'boolean') return
    // The bridge posts before notifying the host. Legacy pages still need
    // relaying; a failed delivery must leave the carrier page open.
    if ('submitted' in data && !data.submitted) return
    const submitted =
      ('submitted' in data && data.submitted) || (await relayCallback(id, data.callback))
    if (
      submitted &&
      mounted &&
      options.open() &&
      id === options.websheet()?.id &&
      id !== completedSheetID &&
      isTerminalCallback(data.callback, options.closeOnStatusChange())
    ) {
      completedSheetID = id
      options.onDone()
    }
  }

  onMounted(() => {
    mounted = true
    window.addEventListener('message', onMessage)
  })
  onUnmounted(() => {
    mounted = false
    window.removeEventListener('message', onMessage)
  })
}
