import { flushPromises } from '@vue/test-utils'
import { effectScope, nextTick, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useSimPin } from '@/composables/useSimPin'
import type { SimPinStatus } from '@/types/simPin'

const api = vi.hoisted(() => ({ status: vi.fn(), submit: vi.fn() }))
vi.mock('@/apis/simPin', () => ({ useSimPinApi: () => api }))
const snapshot = (changes: Partial<SimPinStatus> = {}): SimPinStatus => ({
  supported: true,
  enabled: true,
  lock: 'none',
  pinRetries: 3,
  pukRetries: 10,
  simToken: 'SIM-A',
  ...changes,
})
function mount() {
  const scope = effectScope(),
    id = ref('modem-1'),
    identity = ref('SIM-A')
  const state = scope.run(() => useSimPin(id, identity))
  if (!state) throw new Error('missing scope')
  return { state, identity, id, stop: () => scope.stop() }
}
beforeEach(() => {
  vi.resetAllMocks()
  api.status.mockResolvedValue(snapshot())
  api.submit.mockResolvedValue(undefined)
})

describe('SIM PIN state', () => {
  it('validates ASCII and confirmation, preserves leading zeros and never optimistically toggles', async () => {
    const { state, stop } = mount()
    await flushPromises()
    state.choose('disable')
    state.pin.value = '１２３４'
    expect(state.canSubmit.value).toBe(false)
    state.pin.value = '0001'
    expect(state.canSubmit.value).toBe(true)
    expect(state.status.value?.enabled).toBe(true)
    await state.submit()
    expect(api.submit).toHaveBeenCalledWith('modem-1', {
      kind: 'protection',
      enabled: false,
      pin: '0001',
      simToken: 'SIM-A',
    })
    expect(state.pin.value).toBe('')
    state.choose('change')
    state.pin.value = '0001'
    state.newPin.value = '0002'
    state.confirmation.value = '0003'
    expect(state.canSubmit.value).toBe(false)
    state.confirmation.value = '0002'
    expect(state.canSubmit.value).toBe(true)
    stop()
  })
  it('changes from PIN to PUK after failure and stops at permanent block', async () => {
    api.status.mockResolvedValue(snapshot({ lock: 'pin' }))
    const { state, stop } = mount()
    await flushPromises()
    api.submit.mockRejectedValue({ error_code: 'sim_puk_required' })
    api.status.mockResolvedValue(snapshot({ lock: 'puk', pinRetries: 0 }))
    state.pin.value = '0000'
    await state.submit()
    expect(state.action.value).toBe('puk')
    expect(state.pin.value).toBe('')
    expect(state.status.value?.pinRetries).toBe(0)
    state.pin.value = '00000000'
    state.newPin.value = '0001'
    state.confirmation.value = '0001'
    api.status.mockResolvedValue(snapshot({ lock: 'permanently_blocked', pukRetries: 0 }))
    await state.submit()
    expect(state.action.value).toBe(null)
    expect(state.canSubmit.value).toBe(false)
    expect(api.submit).toHaveBeenCalledTimes(2)
    stop()
  })
  it('blocks duplicates and does not replay after transport errors', async () => {
    let reject: ((error: Error) => void) | undefined
    api.submit.mockImplementation(
      () =>
        new Promise<void>((_, fail) => {
          reject = fail
        }),
    )
    const { state, stop } = mount()
    await flushPromises()
    state.choose('change')
    state.pin.value = '1234'
    state.newPin.value = state.confirmation.value = '0000'
    const first = state.submit()
    await state.submit()
    expect(api.submit).toHaveBeenCalledTimes(1)
    state.close()
    expect(state.busy.value).toBe(true)
    reject?.(new Error('connection lost'))
    await first
    expect(state.errorCode.value).toBe('sim_pin_result_unknown')
    expect(api.submit).toHaveBeenCalledTimes(1)
    stop()
  })
  it('ignores stale reads and clears credentials when the SIM changes or the page closes', async () => {
    let resolve: ((status: SimPinStatus) => void) | undefined
    api.status.mockImplementationOnce(
      () =>
        new Promise<SimPinStatus>((done) => {
          resolve = done
        }),
    )
    const { state, identity, stop } = mount()
    state.pin.value = '1234'
    identity.value = 'SIM-B'
    await nextTick()
    await flushPromises()
    expect(state.pin.value).toBe('')
    resolve?.(snapshot({ simToken: 'stale' }))
    await flushPromises()
    expect(state.status.value?.simToken).toBe('SIM-A')
    state.pin.value = '1234'
    state.close()
    expect(state.pin.value).toBe('')
    stop()
  })
  it('retains acknowledgement when refresh fails and hides management for old backends', async () => {
    const { state, stop } = mount()
    await flushPromises()
    state.choose('disable')
    state.pin.value = '1234'
    api.status.mockRejectedValue({ status: 503 })
    expect(await state.submit()).toBe(true)
    expect(state.acknowledged.value).toBe(true)
    expect(state.status.value).toBeNull()
    expect(state.errorCode.value).toBe('sim_pin_unavailable')
    api.status.mockResolvedValue(snapshot({ enabled: false }))
    await state.refresh()
    expect(state.errorCode.value).toBe('')
    expect(state.status.value?.enabled).toBe(false)
    expect(api.submit).toHaveBeenCalledTimes(1)
    api.status.mockRejectedValue({ status: 404 })
    await state.refresh()
    expect(state.legacy.value).toBe(true)
    expect(state.available.value).toBe(false)
    stop()
  })

  it('immediately clears credentials and loading when the device becomes unavailable', async () => {
    let finishRead: ((status: SimPinStatus) => void) | undefined
    api.status.mockImplementationOnce(
      () =>
        new Promise<SimPinStatus>((resolve) => {
          finishRead = resolve
        }),
    )
    const { state, id, stop } = mount()
    state.pin.value = '1234'
    expect(state.loading.value).toBe(true)

    id.value = 'unknown'
    expect(state.pin.value).toBe('')
    expect(state.loading.value).toBe(false)
    expect(state.status.value).toBeNull()
    finishRead?.(snapshot())
    await flushPromises()
    expect(state.status.value).toBeNull()
    expect(api.status).toHaveBeenCalledTimes(1)
    stop()
  })

  it('ignores a write completion after the user switches SIMs', async () => {
    let finishWrite: (() => void) | undefined
    api.submit.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          finishWrite = resolve
        }),
    )
    const { state, identity, stop } = mount()
    await flushPromises()
    state.choose('disable')
    state.pin.value = '1234'
    const pending = state.submit()
    state.choose('change')
    expect(state.action.value).toBe('disable')

    api.status.mockResolvedValue(snapshot({ simToken: 'SIM-B', enabled: false }))
    identity.value = 'SIM-B'
    expect(state.pin.value).toBe('')
    await flushPromises()
    finishWrite?.()
    expect(await pending).toBe(false)
    expect(state.status.value?.simToken).toBe('SIM-B')
    expect(state.acknowledged.value).toBe(false)
    expect(state.errorCode.value).toBe('')
    expect(api.status).toHaveBeenCalledTimes(2)
    expect(api.submit).toHaveBeenCalledTimes(1)
    stop()
  })

  it('acknowledges a partial unlock and prevents another credential attempt', async () => {
    api.status.mockResolvedValue(snapshot({ lock: 'pin' }))
    api.submit.mockRejectedValue({ error_code: 'enable_modem_after_unlock_failed' })
    const { state, stop } = mount()
    await flushPromises()
    state.pin.value = '1234'
    expect(await state.submit()).toBe(false)
    expect(state.acknowledged.value).toBe(true)
    expect(state.errorCode.value).toBe('enable_modem_after_unlock_failed')
    state.pin.value = '1234'
    expect(state.canSubmit.value).toBe(false)
    await state.submit()
    expect(api.submit).toHaveBeenCalledTimes(1)
    stop()
  })

  it('does not offer credential actions for an unsupported SIM', async () => {
    api.status.mockResolvedValue(snapshot({ supported: false, lock: 'pin' }))
    const { state, stop } = mount()
    await flushPromises()
    expect(state.action.value).toBeNull()
    expect(state.available.value).toBe(false)
    expect(state.canSubmit.value).toBe(false)
    stop()
  })
})
