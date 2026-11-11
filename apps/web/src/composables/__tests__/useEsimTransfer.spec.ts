import { ref, type Ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { reduceEsimTransferStatus, useEsimTransfer } from '@/composables/useEsimTransfer'
import type { EsimTransferProfile, EsimTransferSource } from '@/types/esim'
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

const deferred = <T>() => {
  let resolve: ((value: T) => void) | undefined
  let reject: ((reason: unknown) => void) | undefined
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  if (!resolve || !reject) throw new Error('promise callbacks were not initialized')
  return { promise, resolve, reject }
}

describe('useEsimTransfer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  const source: EsimTransferSource = {
    type: 'modem',
    id: 'source-1',
    name: 'Source',
    requiresSourceImei: false,
  }
  const inactive: EsimTransferProfile = {
    id: 'inactive',
    type: 'esim',
    name: 'Inactive line',
    iccid: '8901',
    enabled: false,
    supported: true,
  }
  const physical: EsimTransferProfile = {
    ...inactive,
    id: 'physical',
    type: 'physical',
    enabled: true,
  }
  const unsupported: EsimTransferProfile = { ...inactive, id: 'unsupported', supported: false }

  const prepareTransfer = async (onCompleted?: () => void) => {
    api.getTransferProfiles.mockResolvedValueOnce({ data: ref([inactive]) })
    const transfer = useEsimTransfer(ref('modem-1'), { onCompleted })
    transfer.selectSource(source)
    await transfer.loadProfiles()
    transfer.selectedProfile.value = inactive
    return transfer
  }

  it('does not restart a transfer while it is running or after it completes', async () => {
    api.getTransferProfiles.mockResolvedValueOnce({ data: ref([inactive]) })
    const transfer = useEsimTransfer(ref('target'))
    transfer.selectSource(source)
    await transfer.loadProfiles()
    transfer.selectedProfile.value = inactive
    transfer.startTransfer('target-se')
    const socket = FakeWebSocket.instances[0]
    socket?.onopen?.()
    transfer.startTransfer('target-se')
    expect(FakeWebSocket.instances).toHaveLength(1)
    expect(socket?.sent).toHaveLength(1)

    socket?.message({ type: 'completed' })
    transfer.startTransfer('target-se')
    expect(transfer.state.value).toBe(TRANSFER_STATE.completed)
    expect(FakeWebSocket.instances).toHaveLength(1)
  })

  it('starts automatically with the source owner’s choice on the original connection', async () => {
    const transfer = useEsimTransfer(ref('target'))
    transfer.connectRemote('target-se', 'https://source.example/prefix')
    const socket = FakeWebSocket.instances[0]
    expect(socket).toBeDefined()
    socket?.onopen?.()
    expect(JSON.parse(socket?.sent[0] ?? '{}')).toEqual({
      type: 'source',
      version: 1,
      sourceType: 'sigmo',
      sourceUrl: 'https://source.example/prefix',
      seId: 'target-se',
    })
    expect(transfer.sources.value).toEqual([])
    socket?.message({ type: 'progress', stage: 'waiting_authorization' })
    expect(transfer.sources.value).toEqual([])
    transfer.selectSource(source)
    await transfer.loadProfiles()
    expect(api.getTransferProfiles).not.toHaveBeenCalled()
    expect(transfer.selectedSource.value).toBeNull()
    socket?.message({
      type: 'source_selected',
      version: 1,
      sourceProfile: inactive,
      sourceName: source.name,
    })
    expect(transfer.selectedProfile.value).toEqual(inactive)
    expect(transfer.sources.value).toEqual([])
    expect(transfer.state.value).toBe(TRANSFER_STATE.progress)
    transfer.startTransfer('target-se')
    expect(socket?.sent).toHaveLength(1)
    expect(FakeWebSocket.instances).toHaveLength(1)
    socket?.message({ type: 'completed' })
    expect(transfer.state.value).toBe(TRANSFER_STATE.completed)
    expect(socket?.readyState).toBe(3)
    transfer.startTransfer('target-se')
    expect(FakeWebSocket.instances).toHaveLength(1)
    transfer.cancelTransfer()
    expect(transfer.remote.value).toBe(false)
  })

  it('rejects malformed source approval and preserves uncertain results', () => {
    const transfer = useEsimTransfer(ref('target'))
    transfer.connectRemote('se', 'https://source.example')
    const socket = FakeWebSocket.instances[0]
    socket?.message({
      type: 'source_selected',
      version: 1,
      sourceProfile: { id: 'bad' },
    })
    expect(transfer.state.value).toBe(TRANSFER_STATE.error)
    expect(socket?.readyState).toBe(3)
    transfer.connectRemote('se', 'https://source.example')
    FakeWebSocket.instances[1]?.message({ type: 'error', code: 'transfer_result_uncertain' })
    expect(transfer.errorMessage.value).toBe('transfer_result_uncertain')
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it.each([undefined, 0, 2])('rejects a source approval with version %s', (version) => {
    const transfer = useEsimTransfer(ref('target'))
    transfer.connectRemote('se', 'https://source.example')
    const socket = FakeWebSocket.instances[0]
    socket?.message({ type: 'source_selected', version, sourceProfile: inactive })
    expect(transfer.state.value).toBe(TRANSFER_STATE.error)
    expect(transfer.errorMessage.value).toBe(TRANSFER_CLIENT_ERROR.invalidResponse)
    expect(transfer.selectedProfile.value).toBeNull()
    expect(socket?.readyState).toBe(3)
  })

  it.each(['error', 'close'])(
    'reports an uncertain result on socket %s after approval',
    (event) => {
      const transfer = useEsimTransfer(ref('target'))
      transfer.connectRemote('se', 'https://source.example')
      const socket = FakeWebSocket.instances[0]
      socket?.message({ type: 'source_selected', version: 1, sourceProfile: inactive })
      if (event === 'error') socket?.onerror?.()
      else socket?.onclose?.()
      expect(transfer.state.value).toBe(TRANSFER_STATE.error)
      expect(transfer.errorMessage.value).toBe('transfer_result_uncertain')
      expect(FakeWebSocket.instances).toHaveLength(1)
    },
  )

  it('rejects source approvals on a local transfer', async () => {
    const transfer = await prepareTransfer()
    transfer.startTransfer('se')
    FakeWebSocket.instances[0]?.message({
      type: 'source_selected',
      version: 1,
      sourceProfile: { ...inactive, id: 'other' },
    })
    expect(transfer.state.value).toBe(TRANSFER_STATE.error)
    expect(transfer.errorMessage.value).toBe(TRANSFER_CLIENT_ERROR.invalidResponse)
    expect(transfer.selectedProfile.value).toEqual(inactive)
  })

  it('rejects repeated approval without replacing the chosen profile', () => {
    const transfer = useEsimTransfer(ref('target'))
    transfer.connectRemote('se', 'https://source.example')
    const socket = FakeWebSocket.instances[0]
    socket?.message({ type: 'source_selected', version: 1, sourceProfile: inactive })
    socket?.message({
      type: 'source_selected',
      version: 1,
      sourceProfile: { ...inactive, id: 'other' },
    })
    expect(transfer.state.value).toBe(TRANSFER_STATE.error)
    expect(transfer.selectedProfile.value).toEqual(inactive)
    expect(socket?.readyState).toBe(3)
  })

  it('ignores old socket messages after starting another remote request', () => {
    const transfer = useEsimTransfer(ref('target'))
    transfer.connectRemote('se', 'https://source.example')
    const previous = FakeWebSocket.instances[0]
    transfer.connectRemote('se', 'https://another.example')
    previous?.message({ type: 'source_selected', version: 1, sourceProfile: inactive })
    expect(transfer.selectedProfile.value).toBeNull()
    expect(transfer.errorMessage.value).toBe('')
    FakeWebSocket.instances[1]?.message({
      type: 'source_selected',
      version: 1,
      sourceProfile: inactive,
    })
    expect(transfer.selectedProfile.value).toEqual(inactive)
  })

  it.each([
    {
      name: 'mixed support',
      profiles: [unsupported, inactive, physical],
      ids: ['inactive', 'physical'],
    },
    { name: 'all supported', profiles: [physical, inactive], ids: ['physical', 'inactive'] },
    { name: 'all unsupported', profiles: [unsupported], ids: [] },
    { name: 'empty response', profiles: [], ids: [] },
  ])('loads only transferable profiles for $name', async ({ profiles, ids }) => {
    api.getTransferProfiles.mockResolvedValueOnce({ data: ref(profiles) })
    const transfer = useEsimTransfer(ref('target'))
    transfer.selectSource(source)

    await transfer.loadProfiles()

    expect(transfer.profiles.value.map((profile) => profile.id)).toEqual(ids)
    expect(transfer.state.value).toBe(TRANSFER_STATE.ready)
    expect(transfer.selectedProfile.value).toBeNull()
    expect(transfer.canStartTransfer.value).toBe(false)
  })

  it('clears a previous selection when reloading or changing sources', async () => {
    api.getTransferProfiles
      .mockResolvedValueOnce({ data: ref([inactive]) })
      .mockResolvedValueOnce({ data: ref([unsupported]) })
    const transfer = useEsimTransfer(ref('target'))
    transfer.selectSource(source)
    await transfer.loadProfiles()
    transfer.selectedProfile.value = inactive
    expect(transfer.canStartTransfer.value).toBe(true)

    const reload = transfer.loadProfiles()
    expect(transfer.selectedProfile.value).toBeNull()
    expect(transfer.canStartTransfer.value).toBe(false)
    await reload
    expect(transfer.profiles.value).toEqual([])
    expect(transfer.selectedProfile.value).toBeNull()

    transfer.selectedProfile.value = inactive
    transfer.selectSource({ ...source, id: 'source-2' })
    expect(transfer.selectedProfile.value).toBeNull()
    expect(transfer.canStartTransfer.value).toBe(false)
  })

  it.each(['response', 'error'])(
    'ignores an old profile %s after selecting another source',
    async (result) => {
      const pending = deferred<{ data: Ref<EsimTransferProfile[]> }>()
      const nextProfile = { ...inactive, id: 'next-line' }
      api.getTransferProfiles
        .mockReturnValueOnce(pending.promise)
        .mockResolvedValueOnce({ data: ref([nextProfile]) })
      const transfer = useEsimTransfer(ref('target'))
      transfer.selectSource(source)
      const previous = transfer.loadProfiles()
      transfer.selectSource({ ...source, id: 'source-2' })
      await transfer.loadProfiles()

      if (result === 'response') pending.resolve({ data: ref([inactive]) })
      else pending.reject(new Error('Previous source unavailable'))
      await previous

      expect(transfer.selectedSource.value?.id).toBe('source-2')
      expect(transfer.profiles.value).toEqual([nextProfile])
      expect(transfer.state.value).toBe(TRANSFER_STATE.ready)
      expect(transfer.errorMessage.value).toBe('')
    },
  )

  it.each(['response', 'error'])(
    'ignores a profile %s after returning to source selection',
    async (result) => {
      const pending = deferred<{ data: Ref<EsimTransferProfile[]> }>()
      api.getTransferProfiles.mockReturnValueOnce(pending.promise)
      api.getTransferSources.mockResolvedValueOnce({ data: ref({ sources: [source] }) })
      const transfer = useEsimTransfer(ref('target'))
      transfer.selectSource(source)
      const previous = transfer.loadProfiles()
      await transfer.loadSources()

      if (result === 'response') pending.resolve({ data: ref([inactive]) })
      else pending.reject(new Error('Previous source unavailable'))
      await previous

      expect(transfer.sources.value).toEqual([source])
      expect(transfer.selectedSource.value).toBeNull()
      expect(transfer.selectedProfile.value).toBeNull()
      expect(transfer.profiles.value).toEqual([])
      expect(transfer.state.value).toBe(TRANSFER_STATE.ready)
      expect(transfer.errorMessage.value).toBe('')
    },
  )

  it('clears the reader IMEI and selected line when returning to source selection', async () => {
    api.getTransferProfiles.mockResolvedValueOnce({ data: ref([inactive]) })
    api.getTransferSources.mockResolvedValueOnce({ data: ref({ sources: [source] }) })
    const transfer = useEsimTransfer(ref('target'))
    transfer.selectSource({ type: 'ccid', id: 'reader', name: 'Reader', requiresSourceImei: true })
    await transfer.loadProfiles()
    transfer.selectedProfile.value = inactive
    transfer.sourceImei.value = '123456789012345'
    await transfer.loadSources()

    expect(transfer.sourceImei.value).toBe('')
    expect(transfer.selectedSource.value).toBeNull()
    expect(transfer.selectedProfile.value).toBeNull()
    expect(transfer.profiles.value).toEqual([])
    expect(transfer.canStartTransfer.value).toBe(false)
  })

  it.each(['cancel', 'remote'])('ignores an unfinished source refresh after %s', async (action) => {
    const pending = deferred<{ data: Ref<{ sources: EsimTransferSource[] }> }>()
    api.getTransferSources.mockReturnValueOnce(pending.promise)
    const transfer = useEsimTransfer(ref('target'))
    const previous = transfer.loadSources()
    if (action === 'cancel') transfer.cancelTransfer()
    else transfer.connectRemote('se', 'https://source.example')
    pending.resolve({ data: ref({ sources: [source] }) })
    await previous

    expect(transfer.sources.value).toEqual([])
    expect(transfer.state.value).toBe(
      action === 'cancel' ? TRANSFER_STATE.idle : TRANSFER_STATE.progress,
    )
    expect(transfer.remote.value).toBe(action === 'remote')
  })

  it('records the preview profile and completes the transfer progress', async () => {
    const onCompleted = vi.fn()
    const transfer = await prepareTransfer(onCompleted)

    transfer.startTransfer('default')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return
    ws.onopen?.()

    expect(JSON.parse(ws.sent[0] ?? '{}')).toMatchObject({
      type: TRANSFER_MESSAGE.start,
      sourceType: 'modem',
      sourceId: 'source-1',
      profileId: inactive.id,
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

  it('defaults required CCID source IMEI to the current modem id', async () => {
    const transfer = useEsimTransfer(ref('target-imei'))

    transfer.selectSource({
      type: 'ccid',
      id: 'reader-1',
      name: 'Reader 1',
      requiresSourceImei: true,
    })
    api.getTransferProfiles.mockResolvedValueOnce({ data: ref([inactive]) })
    await transfer.loadProfiles()
    transfer.selectedProfile.value = inactive

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
      profileId: inactive.id,
      sourceImei: 'target-imei',
    })
  })

  it('enters an error state for malformed websocket messages', async () => {
    const transfer = await prepareTransfer()

    transfer.startTransfer('default')
    const ws = FakeWebSocket.instances[0]
    expect(ws).toBeDefined()
    if (!ws) return
    ws.message('{')

    expect(transfer.state.value).toBe(TRANSFER_STATE.error)
    expect(transfer.errorMessage.value).toBe(TRANSFER_CLIENT_ERROR.invalidResponse)
  })

  it('enters websheet state when carrier setup is required', async () => {
    const transfer = await prepareTransfer()

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

    ws.message({ type: TRANSFER_MESSAGE.progress, stage: TRANSFER_STAGE.downloading })
    expect(transfer.state.value).toBe(TRANSFER_STATE.websheet)
    expect(transfer.stage.value).toBe(TRANSFER_STAGE.downloading)
    expect(transfer.carrierWebsheet.value?.id).toBe('sheet-1')

    transfer.completeWebsheet()
    expect(transfer.state.value).toBe(TRANSFER_STATE.progress)
    expect(transfer.carrierWebsheet.value).toBeNull()
  })

  it('enters an error state for unknown websocket message types', async () => {
    const transfer = await prepareTransfer()

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
