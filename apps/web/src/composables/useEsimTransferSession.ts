import {
  TRANSFER_CLIENT_ERROR,
  TRANSFER_MESSAGE,
  TRANSFER_PEER_VERSION,
} from '@/constants/esimTransfer'
import { resolveAPIWebSocketURL } from '@/lib/apiUrl'
import { parseTransferMessage } from '@/types/esimPeer'
import type { EsimDownloadPreview, EsimTransferProfile } from '@/types/esim'
import type { CarrierWebsheetInfo } from '@/types/websheet'

export type TransferUserInput = {
  text: string
  acceptLabel?: string
  rejectLabel?: string
  freeText: boolean
  freeTextHint?: string
}

type TransferStartMessage = {
  seId: string
  sourceType: string
  sourceId: string
  profileId: string
  sourceImei: string
}

type Handlers = {
  onSourceSelected?: (profile: EsimTransferProfile) => void
  onProgress: (stage: string) => void
  onPreview: (profile?: EsimDownloadPreview) => void
  onUserInput: (input: TransferUserInput | null) => void
  onSourceDeletion: (iccid: string) => void
  onWebsheet: (websheet: CarrierWebsheetInfo | null) => void
  onCompleted: () => void
  onError: (message: string) => void
}

export const useEsimTransferSession = (handlers: Handlers) => {
  let ws: WebSocket | null = null

  const close = () => {
    if (!ws) return
    const current = ws
    ws = null
    current.onclose = null
    current.close()
  }

  const send = (payload: object) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return
    ws.send(JSON.stringify(payload))
  }

  const buildWsUrl = (id: string) => {
    return resolveAPIWebSocketURL(`modems/${id}/esim-transfers/sessions`)
  }

  const connect = (modemId: string, initial: object, remote = false) => {
    close()
    const conn = new WebSocket(buildWsUrl(modemId))
    let sourceSelected = false
    ws = conn
    conn.onopen = () => {
      if (ws !== conn) return
      send(initial)
    }
    conn.onmessage = (event) => {
      if (ws !== conn) return
      let message: ReturnType<typeof parseTransferMessage>
      try {
        const value: unknown = JSON.parse(event.data)
        if (
          !value ||
          typeof value !== 'object' ||
          !('type' in value) ||
          typeof value.type !== 'string'
        )
          throw new Error('invalid message')
        message = parseTransferMessage(value)
      } catch (err) {
        console.error('[useEsimTransferSession] Failed to parse message:', err)
        handlers.onError(TRANSFER_CLIENT_ERROR.invalidResponse)
        close()
        return
      }
      switch (message.type) {
        case 'source_selected':
          if (
            !remote ||
            sourceSelected ||
            message.version !== TRANSFER_PEER_VERSION ||
            !message.sourceProfile?.id ||
            !message.sourceProfile.iccid ||
            !message.sourceProfile.supported ||
            message.sourceProfile.type !== 'esim'
          ) {
            handlers.onError(TRANSFER_CLIENT_ERROR.invalidResponse)
            close()
            return
          }
          sourceSelected = true
          handlers.onSourceSelected?.(message.sourceProfile)
          return
        case TRANSFER_MESSAGE.progress:
          handlers.onProgress(message.stage ?? '')
          return
        case TRANSFER_MESSAGE.preview:
          handlers.onPreview(message.profile)
          return
        case TRANSFER_MESSAGE.userInput:
          handlers.onUserInput(message.input ?? null)
          return
        case TRANSFER_MESSAGE.sourceDeletion:
          handlers.onSourceDeletion(message.iccid ?? '')
          return
        case TRANSFER_MESSAGE.websheet:
          handlers.onWebsheet(message.websheet ?? null)
          return
        case TRANSFER_MESSAGE.completed:
          handlers.onCompleted()
          close()
          return
        case TRANSFER_MESSAGE.error:
          handlers.onError(message.code || message.message || '')
          close()
          return
        default:
          console.error('[useEsimTransferSession] Unknown message type:', message.type)
          handlers.onError(TRANSFER_CLIENT_ERROR.invalidResponse)
          close()
      }
    }
    conn.onerror = () => {
      if (ws !== conn) return
      handlers.onError(sourceSelected ? 'transfer_result_uncertain' : '')
      close()
    }
    conn.onclose = () => {
      if (ws !== conn) return
      ws = null
      handlers.onError(
        sourceSelected ? 'transfer_result_uncertain' : TRANSFER_CLIENT_ERROR.connectionClosed,
      )
    }
  }

  const start = (modemId: string, message: TransferStartMessage) => {
    const payload = { type: TRANSFER_MESSAGE.start, ...message }
    connect(modemId, payload)
  }

  const connectRemote = (modemId: string, seId: string, sourceUrl: string) => {
    connect(
      modemId,
      { type: 'source', version: TRANSFER_PEER_VERSION, sourceType: 'sigmo', sourceUrl, seId },
      true,
    )
  }

  const submitUserInput = (accept: boolean, response: string) => {
    send({
      type: TRANSFER_MESSAGE.userInput,
      accept,
      response,
    })
  }

  const confirmSourceDeletion = (accept: boolean) => {
    send({ type: TRANSFER_MESSAGE.sourceDeletion, accept })
  }

  const cancel = () => {
    send({ type: TRANSFER_MESSAGE.cancel })
    close()
  }

  return {
    start,
    connectRemote,
    submitUserInput,
    confirmSourceDeletion,
    cancel,
    close,
  }
}
