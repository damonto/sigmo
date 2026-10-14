import { defineComponent, reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { useCommunicationHistory } from '@/composables/useCommunicationHistory'
import { invalidateSession } from '@/lib/authSession'
import type { MessageThread, ThreadMessage } from '@/types/communication'

enableAutoUnmount(afterEach)
const api = vi.hoisted(() => ({
  lines: vi.fn(),
  threads: vi.fn(),
  thread: vi.fn(),
  messages: vi.fn(),
  read: vi.fn(),
  deleteThread: vi.fn(),
}))
const route = reactive({ params: { threadId: 'one' } })
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('@/apis/communication', () => ({ communicationApi: api }))
vi.mock('@/composables/useCapabilities', () => ({
  useCapabilities: () => ({ hasFeature: () => true, fetchCapabilities: async () => {} }),
}))
vi.mock('@/composables/useContacts', () => ({
  useContacts: () => ({ load: async () => {}, match: () => undefined }),
}))
const message: ThreadMessage = {
  id: 1,
  conversationId: 'sim-conversation',
  profileId: 'sim',
  historyProfileId: 'sim',
  modemId: 'modem',
  sender: '10086',
  recipient: '10010',
  text: 'Hello',
  timestamp: '2026-10-13T12:00:00Z',
  incoming: true,
  read: false,
  status: 'received',
  lineLabel: '',
  lineNumber: '',
}
const thread: MessageThread = {
  id: 'one',
  participant: '10086',
  latestMessage: message,
  unreadCount: 1,
  markedUnread: false,
}
const mountHistory = () => {
  let list: ReturnType<typeof useCommunicationHistory> | undefined
  let detail: ReturnType<typeof useCommunicationHistory> | undefined
  const wrapper = mount(
    defineComponent({
      setup() {
        list = useCommunicationHistory('messages')
        detail = useCommunicationHistory('thread')
        return () => null
      },
    }),
  )
  if (!list || !detail) throw new Error('Missing history state')
  return { list, detail, wrapper }
}
beforeEach(() => {
  vi.clearAllMocks()
  route.params.threadId = 'one'
  api.lines.mockResolvedValue({ items: [] })
  api.threads.mockResolvedValue({ items: [thread], nextCursor: '' })
  api.thread.mockResolvedValue(thread)
  api.messages.mockResolvedValue({ items: [message], nextCursor: '' })
  api.read.mockResolvedValue({})
  api.deleteThread.mockResolvedValue(undefined)
})

describe('communication split view', () => {
  it('loads history while optional line metadata is still pending', async () => {
    let finish: ((value: { items: [] }) => void) | undefined
    api.lines.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const { detail } = mountHistory()
    await flushPromises()
    expect(detail.messages.value).toEqual([message])
    expect(detail.loading.value).toBe(false)
    finish?.({ items: [] })
    await flushPromises()
  })

  it('keeps history usable when optional metadata fails', async () => {
    api.lines.mockRejectedValue(new Error('Directory offline'))
    const { list, detail } = mountHistory()
    await flushPromises()
    expect(list.threads.value).toEqual([thread])
    expect(detail.messages.value).toEqual([message])
    expect(detail.error.value).toBe('')
  })

  it('clears the participant and modem when the login session changes', async () => {
    const { detail } = mountHistory()
    await flushPromises()
    expect(detail.participant.value).toBe('10086')
    invalidateSession()
    expect(detail.participant.value).toBe('')
    expect(detail.originalModemID.value).toBe('')
    expect(detail.messages.value).toEqual([])
  })

  it('coalesces read receipts and discards late errors after logout', async () => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    let fail: ((reason: Error) => void) | undefined
    api.read.mockImplementation(
      () =>
        new Promise((_, reject) => {
          fail = reject
        }),
    )
    const { detail } = mountHistory()
    await flushPromises()
    const first = detail.acknowledge(1)
    const second = detail.acknowledge(1)
    expect(api.read).toHaveBeenCalledTimes(1)
    invalidateSession()
    fail?.(new Error('Old session'))
    await Promise.all([first, second])
    expect(detail.error.value).toBe('')
  })

  it('loads the list and opened thread independently without reading unseen messages', async () => {
    const { list, detail } = mountHistory()
    expect(list.loading.value).toBe(true)
    expect(detail.loading.value).toBe(true)
    await flushPromises()
    expect(list.threads.value).toEqual([thread])
    expect(detail.messages.value).toEqual([message])
    expect(list.threadID.value).toBe('')
    expect(api.read).not.toHaveBeenCalled()
    expect(api.threads).toHaveBeenCalledWith({ q: '', cursor: '' })
    expect(api.messages).toHaveBeenCalledWith('one', '')
    let release: (() => void) | undefined
    api.threads.mockImplementation(async () => {
      await new Promise<void>((resolve) => {
        release = resolve
      })
      return { items: [thread], nextCursor: '' }
    })
    const refresh = list.load()
    await flushPromises()
    expect(list.loading.value).toBe(true)
    expect(list.threads.value).toEqual([thread])
    release?.()
    await refresh
  })

  it('filters unread messages across loaded pages without discarding the all view', async () => {
    const read = { ...thread, id: 'read', unreadCount: 0 }
    const marked = { ...thread, id: 'marked', unreadCount: 0, markedUnread: true }
    api.threads.mockResolvedValueOnce({ items: [thread, read], nextCursor: 'older' })
    const { list } = mountHistory()
    await flushPromises()
    list.unreadOnly.value = true
    expect(list.visibleThreads.value.map((item) => item.id)).toEqual(['one'])
    expect(list.cursor.value).toBe('older')
    api.threads.mockResolvedValueOnce({ items: [marked], nextCursor: '' })
    await list.load(true)
    expect(list.visibleThreads.value.map((item) => item.id)).toEqual(['one', 'marked'])
    list.unreadOnly.value = false
    expect(list.visibleThreads.value.map((item) => item.id)).toEqual(['one', 'read', 'marked'])
    expect(api.read).not.toHaveBeenCalled()
  })

  it('ignores a late thread response after navigation', async () => {
    let release: ((value: MessageThread) => void) | undefined
    api.thread.mockImplementationOnce(
      () =>
        new Promise<MessageThread>((resolve) => {
          release = resolve
        }),
    )
    const { detail } = mountHistory()
    await flushPromises()
    route.params.threadId = 'two'
    api.thread.mockResolvedValue({ ...thread, id: 'two', participant: '10010' })
    await flushPromises()
    release?.(thread)
    await flushPromises()
    expect(detail.participant.value).toBe('10010')
    expect(api.messages).not.toHaveBeenCalledWith('one', '')
  })

  it('deletes an opened thread without requesting the removed resource again', async () => {
    const { detail } = mountHistory()
    await flushPromises()
    api.thread.mockClear()
    expect(await detail.deleteCurrent('one')).toBe(true)
    expect(api.deleteThread).toHaveBeenCalledWith('one')
    expect(api.thread).not.toHaveBeenCalled()
  })

  it('allows a successful deletion to close the thread after a background refresh', async () => {
    let finish: (() => void) | undefined
    api.deleteThread.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve
        }),
    )
    const { detail } = mountHistory()
    await flushPromises()
    const deletion = detail.deleteCurrent('one')
    await detail.load()
    finish?.()
    expect(await deletion).toBe(true)
  })
})
