import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { reduceEsimTransferStatus, useEsimTransfer } from '@/composables/useEsimTransfer'
import {
  TRANSFER_CLIENT_ERROR,
  TRANSFER_MESSAGE,
  TRANSFER_STAGE,
  TRANSFER_STATE,
} from '@/constants/esimTransfer'

const api = vi.hoisted(() => ({
  getTransferSources: vi.fn(),
  getTransferProfiles: vi.fn(),
}))

vi.mock('@/apis/esim', () => ({
  useEsimApi: () => api,
}))

class FakeWebSocket {
  static OPEN = 1
  static instances: FakeWebSocket[] = []

  readyState = FakeWebSocket.OPEN
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this)
  }

  send(message: string) {
    this.sent.push(message)
  }

  close() {
    this.readyState = 3
  }

  message(payload: unknown) {
    const data = typeof payload === 'string' ? payload : JSON.stringify(payload)
    this.onmessage?.({ data } as MessageEvent<string>)
  }
}

describe('useEsimTransfer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  it('records the preview profile and completes the transfer progress', () => {
    const onCompleted = vi.fn()
    const transfer = useEsimTransfer(ref('modem-1'), { onCompleted })

    transfer.selectedSource.value = {
      type: 'modem',
      id: 'source-1',
      name: 'Source',
      requiresSourceImei: false,
    }
    transfer.selectedProfile.value = {
      id: 'profile-1',
      type: 'esim',
      name: 'Source line',
      iccid: '8901',
      enabled: true,
      supported: true,
    }

    transfer.startTransfer('default')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return
    ws.onopen?.()

    expect(JSON.parse(ws.sent[0] ?? '{}')).toMatchObject({
      type: TRANSFER_MESSAGE.start,
      sourceType: 'modem',
      sourceId: 'source-1',
      profileId: 'profile-1',
    })

    ws.message({
      type: TRANSFER_MESSAGE.preview,
      profile: {
        iccid: '8902',
        serviceProviderName: 'Carrier',
        profileName: 'Transferred line',
        profileState: 'disabled',
        profileOwner: { mcc: '208', mnc: '09' },
      },
    })
    ws.message({ type: TRANSFER_MESSAGE.completed })

    expect(transfer.state.value).toBe(TRANSFER_STATE.completed)
    expect(transfer.previewProfile.value?.profileName).toBe('Transferred line')
    expect(transfer.downloadedName.value).toBe('Transferred line')
    expect(transfer.progress.value).toBe(100)
    expect(onCompleted).toHaveBeenCalledOnce()
  })

  it('defaults required CCID source IMEI to the current modem id', () => {
    const transfer = useEsimTransfer(ref('target-imei'))

    transfer.selectSource({
      type: 'ccid',
      id: 'reader-1',
      name: 'Reader 1',
      requiresSourceImei: true,
    })
    transfer.selectedProfile.value = {
      id: 'profile-1',
      type: 'esim',
      name: 'Source line',
      iccid: '8901',
      enabled: true,
      supported: true,
    }

    expect(transfer.sourceImei.value).toBe('target-imei')
    expect(transfer.canStartTransfer.value).toBe(true)

    transfer.startTransfer('default')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return
    ws.onopen?.()

    expect(JSON.parse(ws.sent[0] ?? '{}')).toMatchObject({
      type: TRANSFER_MESSAGE.start,
      sourceType: 'ccid',
      sourceId: 'reader-1',
      profileId: 'profile-1',
      sourceImei: 'target-imei',
    })
  })

  it('enters an error state for malformed websocket messages', () => {
    const transfer = useEsimTransfer(ref('modem-1'))

    transfer.selectedSource.value = {
      type: 'modem',
      id: 'source-1',
      name: 'Source',
      requiresSourceImei: false,
    }
    transfer.selectedProfile.value = {
      id: 'profile-1',
      type: 'esim',
      name: 'Source line',
      iccid: '8901',
      enabled: true,
      supported: true,
    }

    transfer.startTransfer('default')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return
    ws.message('{')

    expect(transfer.state.value).toBe(TRANSFER_STATE.error)
    expect(transfer.errorMessage.value).toBe(TRANSFER_CLIENT_ERROR.invalidResponse)
  })

  it('enters websheet state when carrier setup is required', () => {
    const transfer = useEsimTransfer(ref('modem-1'))

    transfer.selectedSource.value = {
      type: 'modem',
      id: 'source-1',
      name: 'Source',
      requiresSourceImei: false,
    }
    transfer.selectedProfile.value = {
      id: 'profile-1',
      type: 'esim',
      name: 'Source line',
      iccid: '8901',
      enabled: true,
      supported: true,
    }

    transfer.startTransfer('default')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return
    ws.message({
      type: TRANSFER_MESSAGE.websheet,
      websheet: {
        id: 'sheet-1',
        embedUrl: '/api/v1/websheets/sheet-1',
        title: 'Carrier',
        url: 'https://example.com/setup',
        method: 'GET',
      },
    })

    expect(transfer.state.value).toBe(TRANSFER_STATE.websheet)
    expect(transfer.carrierWebsheet.value?.id).toBe('sheet-1')

    transfer.completeWebsheet()
    expect(transfer.state.value).toBe(TRANSFER_STATE.progress)
  })

  it('enters an error state for unknown websocket message types', () => {
    const transfer = useEsimTransfer(ref('modem-1'))

    transfer.selectedSource.value = {
      type: 'modem',
      id: 'source-1',
      name: 'Source',
      requiresSourceImei: false,
    }
    transfer.selectedProfile.value = {
      id: 'profile-1',
      type: 'esim',
      name: 'Source line',
      iccid: '8901',
      enabled: true,
      supported: true,
    }

    transfer.startTransfer('default')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return
    ws.message({ type: 'future_message' })

    expect(transfer.state.value).toBe(TRANSFER_STATE.error)
    expect(transfer.errorMessage.value).toBe(TRANSFER_CLIENT_ERROR.invalidResponse)
  })
})

describe('reduceEsimTransferStatus', () => {
  it('maps events to transfer states and progress', () => {
    const initial = {
      state: TRANSFER_STATE.idle,
      stage: '',
      progress: 0,
      errorMessage: '',
    }

    const connecting = reduceEsimTransferStatus(initial, { type: 'connecting' })
    expect(connecting).toMatchObject({
      state: TRANSFER_STATE.connecting,
      stage: TRANSFER_STAGE.preparing,
      progress: 10,
      errorMessage: '',
    })

    const downloading = reduceEsimTransferStatus(connecting, {
      type: 'progress',
      stage: TRANSFER_STAGE.downloading,
    })
    expect(downloading).toMatchObject({
      state: TRANSFER_STATE.progress,
      stage: TRANSFER_STAGE.downloading,
      progress: 45,
    })

    const completed = reduceEsimTransferStatus(downloading, { type: 'completed' })
    expect(completed).toMatchObject({
      state: TRANSFER_STATE.completed,
      progress: 100,
      errorMessage: '',
    })
  })

  it('ignores late errors after terminal or idle states', () => {
    const completed = {
      state: TRANSFER_STATE.completed,
      stage: TRANSFER_STAGE.completing,
      progress: 100,
      errorMessage: '',
    }
    expect(
      reduceEsimTransferStatus(completed, {
        type: 'error',
        message: TRANSFER_CLIENT_ERROR.connectionClosed,
      }),
    ).toEqual(completed)

    const idle = {
      state: TRANSFER_STATE.idle,
      stage: '',
      progress: 0,
      errorMessage: '',
    }
    expect(
      reduceEsimTransferStatus(idle, {
        type: 'error',
        message: TRANSFER_CLIENT_ERROR.connectionClosed,
      }),
    ).toEqual(idle)
  })
})
