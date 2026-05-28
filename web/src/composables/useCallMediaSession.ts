import { computed, getCurrentInstance, onBeforeUnmount, ref, type Ref } from 'vue'

import { buildCallMediaUrl } from '@/apis/call'
import type { CallMediaErrorMessage, CallMediaInfo, CallMediaReadyMessage } from '@/types/call'

type MediaStatus = 'idle' | 'connecting' | 'ready' | 'closed' | 'error'

type MediaPacket = ArrayBuffer | Uint8Array<ArrayBuffer>

type Options = {
  onRtpPacket?: (packet: ArrayBuffer) => void
}

const isReadyMessage = (value: unknown): value is CallMediaReadyMessage => {
  if (!value || typeof value !== 'object') return false
  const record = value as Record<string, unknown>
  return record.type === 'ready' && typeof record.media === 'object' && record.media !== null
}

const isErrorMessage = (value: unknown): value is CallMediaErrorMessage => {
  if (!value || typeof value !== 'object') return false
  const record = value as Record<string, unknown>
  if (record.type !== 'error' || typeof record.error !== 'object' || record.error === null) {
    return false
  }
  const error = record.error as Record<string, unknown>
  return typeof error.message === 'string'
}

const dataAsArrayBuffer = async (data: unknown) => {
  if (data instanceof ArrayBuffer) {
    return data
  }
  if (ArrayBuffer.isView(data)) {
    const view = new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
    const copy = new Uint8Array(view.byteLength)
    copy.set(view)
    return copy.buffer
  }
  if (data instanceof Blob) {
    return data.arrayBuffer()
  }
  return null
}

export const useCallMediaSession = (
  modemId: Ref<string>,
  options: Options = {},
) => {
  const status = ref<MediaStatus>('idle')
  const mediaInfo = ref<CallMediaInfo | null>(null)
  const errorMessage = ref('')

  let ws: WebSocket | null = null

  const isReady = computed(() => status.value === 'ready')

  const disconnect = () => {
    if (!ws) {
      status.value = status.value === 'error' ? 'error' : 'closed'
      return
    }
    const current = ws
    ws = null
    current.onclose = null
    current.close()
    status.value = status.value === 'error' ? 'error' : 'closed'
  }

  const connect = (callID: string) => {
    const id = modemId.value
    if (!id || id === 'unknown' || !callID) return
    disconnect()
    mediaInfo.value = null
    errorMessage.value = ''
    status.value = 'connecting'

    const conn = new WebSocket(buildCallMediaUrl(id, callID))
    conn.binaryType = 'arraybuffer'
    ws = conn

    conn.onmessage = (event) => {
      if (ws !== conn) return
      if (typeof event.data === 'string') {
        let message: unknown
        try {
          message = JSON.parse(event.data)
        } catch (err) {
          console.error('[useCallMediaSession] parse media message:', err)
          errorMessage.value = 'Invalid media response'
          status.value = 'error'
          disconnect()
          return
        }
        if (isErrorMessage(message)) {
          errorMessage.value = message.error.message
          status.value = 'error'
          disconnect()
          return
        }
        if (!isReadyMessage(message)) {
          errorMessage.value = 'Invalid media response'
          status.value = 'error'
          disconnect()
          return
        }
        mediaInfo.value = message.media
        status.value = 'ready'
        return
      }

      void dataAsArrayBuffer(event.data).then((packet) => {
        if (!packet || ws !== conn) return
        options.onRtpPacket?.(packet)
      })
    }
    conn.onerror = () => {
      if (ws !== conn) return
      errorMessage.value = 'Call media connection failed'
      status.value = 'error'
      disconnect()
    }
    conn.onclose = () => {
      if (ws !== conn) return
      ws = null
      status.value = status.value === 'error' ? 'error' : 'closed'
    }
  }

  const sendRtpPacket = (packet: MediaPacket) => {
    if (!ws || ws.readyState !== WebSocket.OPEN || status.value !== 'ready') {
      return false
    }
    ws.send(
      packet instanceof ArrayBuffer
        ? packet
        : packet.buffer.slice(packet.byteOffset, packet.byteOffset + packet.byteLength),
    )
    return true
  }

  if (getCurrentInstance()) {
    onBeforeUnmount(disconnect)
  }

  return {
    status,
    isReady,
    mediaInfo,
    errorMessage,
    connect,
    disconnect,
    sendRtpPacket,
  }
}
