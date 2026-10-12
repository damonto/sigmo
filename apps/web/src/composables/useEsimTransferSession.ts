import {
  TRANSFER_CLIENT_ERROR,
  TRANSFER_MESSAGE,
} from '@/constants/esimTransfer'
import { resolveAPIWebSocketURL } from '@/lib/apiUrl'
import { getStoredToken } from '@/lib/authStorage'
import type { EsimDownloadPreview } from '@/types/esim'
import type { CarrierWebsheetInfo } from '@/types/websheet'

export type TransferUserInput = {
  text: string
  acceptLabel?: string
  rejectLabel?: string
  freeText: boolean
  freeTextHint?: string
}

type TransferServerMessage = {
  type: string
  stage?: string
  message?: string
  iccid?: string
  profile?: EsimDownloadPreview
  input?: TransferUserInput
  websheet?: CarrierWebsheetInfo
}

type TransferStartMessage = {
  seId: string
  sourceType: string
  sourceId: string
  profileId: string
  sourceImei: string
}

type Handlers = {
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
    return resolveAPIWebSocketURL(`modems/${id}/esim-transfers/sessions`, getStoredToken())
  }

  const start = (modemId: string, message: TransferStartMessage) => {
    close()
    const conn = new WebSocket(buildWsUrl(modemId))
    ws = conn
    conn.onopen = () => {
      if (ws !== conn) return
      send({
        type: TRANSFER_MESSAGE.start,
        seId: message.seId,
        sourceType: message.sourceType,
        sourceId: message.sourceId,
        profileId: message.profileId,
        sourceImei: message.sourceImei,
      })
    }
    conn.onmessage = (event) => {
      if (ws !== conn) return
      let message: TransferServerMessage
      try {
        message = JSON.parse(event.data) as TransferServerMessage
      } catch (err) {
        console.error('[useEsimTransferSession] Failed to parse message:', err)
        handlers.onError(TRANSFER_CLIENT_ERROR.invalidResponse)
        close()
        return
      }
      switch (message.type) {
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
          handlers.onError(message.message ?? '')
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
      handlers.onError('')
      close()
    }
    conn.onclose = () => {
      if (ws !== conn) return
      ws = null
      handlers.onError(TRANSFER_CLIENT_ERROR.connectionClosed)
    }
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
    submitUserInput,
    confirmSourceDeletion,
    cancel,
    close,
  }
}
