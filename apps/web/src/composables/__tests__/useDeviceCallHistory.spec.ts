import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, ref } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useDeviceCallHistory } from '@/composables/useDeviceCallHistory'
import { invalidateSession } from '@/lib/authSession'
import type { CallRecord } from '@/types/call'
import type { HistoryPage } from '@/types/communication'

const api = vi.hoisted(() => ({ calls: vi.fn(), deleteCall: vi.fn() }))
vi.mock('@/apis/communication', () => ({ communicationApi: api }))

const record = (callID: string): CallRecord => ({
  callID,
  number: '10086',
  direction: 'outgoing',
  state: 'ended',
  hold: 'none',
  route: 'volte',
  reason: '',
  startedAt: '',
  endedAt: '',
  answeredAt: '',
  updatedAt: '',
})
const mountHistory = () => {
  const modem = ref('a')
  let history: ReturnType<typeof useDeviceCallHistory> | undefined
  const wrapper = mount(
    defineComponent({
      setup() {
        history = useDeviceCallHistory(computed(() => modem.value))
        return () => null
      },
    }),
  )
  if (!history) throw new Error('Missing device history state')
  return { history, modem, wrapper }
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks()
  api.calls.mockResolvedValue({ items: [], nextCursor: '' })
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
})

it('discards an earlier device response and cursor after switching devices', async () => {
  let finish: ((value: HistoryPage<CallRecord>) => void) | undefined
  api.calls.mockImplementationOnce(
    () =>
      new Promise<HistoryPage<CallRecord>>((resolve) => {
        finish = resolve
      }),
  )
  const { history, modem } = mountHistory()
  modem.value = 'b'
  await flushPromises()
  finish?.({ items: [record('old')], nextCursor: 'old-cursor' })
  await flushPromises()
  expect(api.calls).toHaveBeenCalledWith({ modemId: 'b', q: '', cursor: '' })
  expect(history.recentCalls.value).toEqual([])
  expect(history.hasMore.value).toBe(false)
})

it('appends older calls once and preserves expanded history during automatic refresh', async () => {
  api.calls.mockResolvedValueOnce({ items: [record('recent')], nextCursor: 'older' })
  const { history } = mountHistory()
  await flushPromises()
  let finish: ((value: HistoryPage<CallRecord>) => void) | undefined
  api.calls.mockImplementationOnce(
    () =>
      new Promise<HistoryPage<CallRecord>>((resolve) => {
        finish = resolve
      }),
  )
  const loading = history.loadMoreCalls()
  await history.loadMoreCalls()
  expect(api.calls).toHaveBeenCalledTimes(2)
  expect(api.calls).toHaveBeenLastCalledWith({ modemId: 'a', q: '', cursor: 'older' })
  finish?.({ items: [record('older')], nextCursor: '' })
  await loading
  expect(history.recentCalls.value.map((call) => call.callID)).toEqual(['recent', 'older'])
  expect(history.hasMore.value).toBe(false)
  window.dispatchEvent(new Event('sigmo:communication-changed'))
  await flushPromises()
  expect(api.calls).toHaveBeenCalledTimes(2)
  expect(history.recentCalls.value.map((call) => call.callID)).toEqual(['recent', 'older'])
})

it.each(['device', 'search', 'session', 'missing device'])(
  'discards an older page after changing %s',
  async (change) => {
    api.calls.mockResolvedValueOnce({ items: [record('recent')], nextCursor: 'older' })
    const { history, modem } = mountHistory()
    await flushPromises()
    let finish: ((value: HistoryPage<CallRecord>) => void) | undefined
    api.calls.mockImplementationOnce(
      () =>
        new Promise<HistoryPage<CallRecord>>((resolve) => {
          finish = resolve
        }),
    )
    const loading = history.loadMoreCalls()
    if (change === 'device') modem.value = 'b'
    else if (change === 'search') history.setSearchQuery('10086')
    else if (change === 'session') invalidateSession()
    else modem.value = 'unknown'
    await flushPromises()
    finish?.({ items: [record('old')], nextCursor: 'old-cursor' })
    await loading
    expect(history.recentCalls.value).toEqual([])
    expect(history.hasMore.value).toBe(false)
    expect(history.isLoading.value).toBe(false)
  },
)

it('retains loaded calls and the cursor when loading more fails, then allows retry', async () => {
  api.calls
    .mockResolvedValueOnce({ items: [record('recent')], nextCursor: 'older' })
    .mockRejectedValueOnce(new Error('Offline'))
    .mockResolvedValueOnce({ items: [record('older')], nextCursor: '' })
  const { history } = mountHistory()
  await flushPromises()
  await history.loadMoreCalls()
  expect(history.recentCalls.value.map((call) => call.callID)).toEqual(['recent'])
  expect(history.hasMore.value).toBe(true)
  expect(history.errorMessage.value).toBe('Offline')
  await history.loadMoreCalls()
  expect(api.calls).toHaveBeenLastCalledWith({ modemId: 'a', q: '', cursor: 'older' })
  expect(history.recentCalls.value.map((call) => call.callID)).toEqual(['recent', 'older'])
  expect(history.errorMessage.value).toBe('')
})
