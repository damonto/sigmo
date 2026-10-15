import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref, type Ref } from 'vue'
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import type {
  Conversation,
  ConversationMessagesResponse,
  MessageResponse,
  MessageState,
} from '@/types/message'

import { useModemMessageThread } from '@/composables/useModemMessageThread'

const api = vi.hoisted(() => ({
  getMessagesByParticipant: vi.fn(),
  deleteMessagesByParticipant: vi.fn(),
  sendMessage: vi.fn(),
  getConversations: vi.fn(),
  getConversationMessages: vi.fn(),
  updateConversation: vi.fn(),
  deleteConversation: vi.fn(),
}))

const router = vi.hoisted(() => ({
  push: vi.fn(),
  replace: vi.fn(),
}))

vi.mock('@/apis/message', () => ({
  useMessageApi: () => api,
}))

vi.mock('vue-router', () => ({
  useRouter: () => router,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

describe('useModemMessageThread', () => {
  const wrappers: VueWrapper[] = []
  const fixtureMessage: MessageResponse = {
    id: 1,
    sender: '+12223334444',
    recipient: 'own',
    text: 'hello',
    timestamp: '2026-05-29T10:00:00Z',
    status: 'received',
    incoming: true,
    wifiCalling: false,
  }
  const fixtureConversation: Conversation = {
    id: 'conversation-a',
    profileId: 'sim-a',
    participant: '+12223334444',
    modemId: 'modem-1',
    lineLabel: 'Work',
    lineNumber: '',
    readThroughMessageId: 0,
    lastMessageId: 1,
    markedUnread: false,
    unreadCount: 1,
    latestMessage: fixtureMessage,
    canSend: true,
  }
  const page = (
    ids: number[],
    nextCursor = '',
    conversation = fixtureConversation,
    state: Partial<MessageState> = {},
  ) => ({
    data: ref<ConversationMessagesResponse>({
      backendId: 'backend',
      revision: 1,
      unreadCount: ids.length,
      ...state,
      conversation: { ...conversation },
      items: ids.map((id) => ({ ...fixtureMessage, id, conversationId: conversation.id })),
      nextCursor,
    }),
  })
  const existingThread = (conversationId = ref('conversation-a')) => {
    let thread: ReturnType<typeof useModemMessageThread> | undefined
    wrappers.push(
      mount({
        template: '<div />',
        setup() {
          thread = useModemMessageThread({
            modemId: computed(() => 'modem-1'),
            participant: computed(() => '+12223334444'),
            isNewConversation: computed(() => false),
            activeProfileId: computed(() => 'sim-a'),
            conversationId: computed(() => conversationId.value),
          })
        },
      }),
    )
    if (!thread) throw new Error('Thread did not mount')
    return thread
  }
  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  })
  beforeEach(() => {
    vi.resetAllMocks()
    api.getMessagesByParticipant.mockResolvedValue({ data: { value: [] } })
    api.sendMessage.mockResolvedValue({
      data: { value: { to: '+8613800138000', conversationId: 'conversation-a' } },
    })
    api.getConversations.mockResolvedValue({
      data: ref({
        backendId: 'backend',
        revision: 1,
        unreadCount: 1,
        items: [fixtureConversation],
      }),
    })
    api.getConversationMessages.mockResolvedValue(page([1]))
    api.updateConversation.mockResolvedValue({
      data: ref({ backendId: 'backend', revision: 2, unreadCount: 0 }),
    })
  })

  it('loads known conversation pages directly and preserves exhausted progress during refresh', async () => {
    api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'older-page'))
    const thread = existingThread()
    await flushPromises()
    expect(thread.items.value.map((message) => message.key)).toEqual(['3', '4'])
    expect(thread.cursor.value).toBe('older-page')

    api.getConversationMessages.mockResolvedValueOnce(page([1, 2]))
    await thread.fetchThreadMessages(true)

    expect(api.getConversationMessages).toHaveBeenLastCalledWith('conversation-a', 'older-page')
    expect(thread.items.value.map((message) => message.key)).toEqual(['1', '2', '3', '4'])
    expect(thread.cursor.value).toBe('')
    await thread.fetchThreadMessages(true)
    expect(api.getConversationMessages).toHaveBeenCalledTimes(2)

    api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'older-page'))
    await thread.fetchThreadMessages()
    expect(thread.cursor.value).toBe('')
    expect(thread.items.value.map((message) => message.key)).toEqual(['1', '2', '3', '4'])
    expect(api.getConversationMessages).toHaveBeenCalledTimes(3)
    expect(api.getConversations).not.toHaveBeenCalled()
  })

  it('locates an entry without an ID once before fetching its pages directly', async () => {
    api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'older-page'))
    const thread = existingThread(ref(''))
    await flushPromises()

    api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'older-page'))
    await thread.fetchThreadMessages()
    api.getConversationMessages.mockResolvedValueOnce(page([1, 2]))
    await thread.fetchThreadMessages(true)

    expect(api.getConversations).toHaveBeenCalledExactlyOnceWith({
      modemId: 'modem-1',
      profileId: 'sim-a',
    })
    expect(api.getConversationMessages.mock.calls).toEqual([
      ['conversation-a', ''],
      ['conversation-a', ''],
      ['conversation-a', 'older-page'],
    ])
    expect(thread.items.value.map((message) => message.key)).toEqual(['1', '2', '3', '4'])
  })

  it('caches the sent conversation ID when the initial lookup has no conversation', async () => {
    api.getConversations.mockResolvedValueOnce({ data: ref({ items: [] }) })
    const thread = existingThread(ref(''))
    await flushPromises()
    await thread.fetchThreadMessages()
    expect(api.getConversations).toHaveBeenCalledTimes(1)
    expect(api.getConversationMessages).not.toHaveBeenCalled()

    thread.messageDraft.value = 'hello'
    await thread.sendMessage()
    await thread.fetchThreadMessages()

    expect(api.getConversations).toHaveBeenCalledTimes(1)
    expect(api.getConversationMessages.mock.calls).toEqual([
      ['conversation-a', ''],
      ['conversation-a', ''],
    ])
  })

  it('bridges a refresh gap and uses the matching cursor from the new snapshot', async () => {
    api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'old-snapshot'))
    const thread = existingThread()
    await flushPromises()
    api.getConversationMessages
      .mockResolvedValueOnce(page([7, 8], 'new-middle'))
      .mockResolvedValueOnce(page([5, 6], 'new-overlap'))
      .mockResolvedValueOnce(page([3, 4], 'new-oldest'))

    await thread.fetchThreadMessages()

    expect(thread.items.value.map((message) => message.key)).toEqual(['3', '4', '5', '6', '7', '8'])
    expect(thread.cursor.value).toBe('new-oldest')
    api.getConversationMessages.mockResolvedValueOnce(page([1, 2]))
    await thread.fetchThreadMessages(true)
    expect(api.getConversationMessages).toHaveBeenLastCalledWith('conversation-a', 'new-oldest')
    expect(thread.items.value.map((message) => message.key)).toEqual([
      '1',
      '2',
      '3',
      '4',
      '5',
      '6',
      '7',
      '8',
    ])
  })

  it('reaches late imports with older timestamps and skips retained pages after a snapshot changes', async () => {
    api.getConversationMessages.mockResolvedValueOnce(page([5, 6], 'snapshot-6-before-5'))
    const thread = existingThread()
    await flushPromises()
    api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'snapshot-6-before-3'))
    await thread.fetchThreadMessages(true)

    // The new message was imported below the visible pages, leaving the head IDs unchanged.
    api.getConversationMessages.mockResolvedValueOnce(page([5, 6], 'snapshot-7-before-5'))
    await thread.fetchThreadMessages()
    expect(thread.cursor.value).toBe('snapshot-7-before-5')
    expect(thread.items.value.map((message) => message.key)).toEqual(['3', '4', '5', '6'])

    const oldest = page([1, 2, 7])
    const imported = oldest.data.value.items.find((message) => message.id === 7)
    if (!imported) throw new Error('Missing late import fixture')
    imported.timestamp = '2026-05-28T10:00:00Z'
    api.getConversationMessages
      .mockResolvedValueOnce(page([3, 4], 'snapshot-7-before-3'))
      .mockResolvedValueOnce(oldest)
    await thread.fetchThreadMessages(true)

    expect(api.getConversationMessages.mock.calls.slice(3)).toEqual([
      ['conversation-a', 'snapshot-7-before-5'],
      ['conversation-a', 'snapshot-7-before-3'],
    ])
    expect(thread.items.value.map((message) => message.key)).toEqual([
      '7',
      '1',
      '2',
      '3',
      '4',
      '5',
      '6',
    ])
    expect(thread.cursor.value).toBe('')
  })

  it.each(['repeated cursor', 'repeated IDs', 'cursor repeated with new IDs'])(
    'rejects nonadvancing history with %s',
    async (kind) => {
      api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'older-page'))
      const thread = existingThread()
      await flushPromises()
      if (kind === 'repeated cursor') {
        api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'older-page'))
      } else if (kind === 'cursor repeated with new IDs') {
        api.getConversationMessages.mockResolvedValueOnce(page([1, 2], 'older-page'))
      } else {
        api.getConversationMessages
          .mockResolvedValueOnce(page([3, 4], 'next-page'))
          .mockResolvedValueOnce(page([3, 4], 'another-page'))
      }

      await thread.fetchThreadMessages(true)

      expect(thread.items.value.map((message) => message.key)).toEqual(['3', '4'])
      expect(thread.cursor.value).toBe('older-page')
      expect(thread.error.value).toBe('communication.invalidHistoryResponse')
      expect(api.getConversationMessages).toHaveBeenCalledTimes(kind === 'repeated IDs' ? 3 : 2)
    },
  )

  it('keeps a failed older page retryable and ignores it after changing conversations', async () => {
    api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'older-page'))
    const conversationID = ref('conversation-a')
    const thread = existingThread(conversationID)
    await flushPromises()
    api.getConversationMessages.mockRejectedValueOnce(new Error('offline'))
    await thread.fetchThreadMessages(true)
    expect(thread.cursor.value).toBe('older-page')
    expect(thread.items.value.map((message) => message.key)).toEqual(['3', '4'])

    let finish: ((result: ReturnType<typeof page>) => void) | undefined
    api.getConversationMessages.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const pending = thread.fetchThreadMessages(true)
    const replacement = { ...fixtureConversation, id: 'conversation-b', profileId: 'sim-b' }
    api.getConversationMessages.mockResolvedValueOnce(page([10], '', replacement))
    conversationID.value = replacement.id
    await flushPromises()
    if (!finish) throw new Error('Older page was not requested')
    finish(page([1, 2]))
    await pending

    expect(thread.items.value.map((message) => message.key)).toEqual(['10'])
    expect(thread.cursor.value).toBe('')
    expect(thread.error.value).toBe('')
  })

  it.each([false, true])(
    'clears missing conversations and blocks sending (previously loaded: %s)',
    async (loaded) => {
      if (!loaded) api.getConversationMessages.mockRejectedValueOnce({ status: 404 })
      const thread = existingThread()
      await flushPromises()
      if (loaded) {
        expect(thread.items.value).toHaveLength(1)
        api.getConversationMessages.mockRejectedValueOnce({ status: 404 })
        await thread.fetchThreadMessages()
      }

      expect(thread.items.value).toEqual([])
      expect(thread.cursor.value).toBe('')
      expect(thread.error.value).toBe('modemDetail.messages.deleted')
      expect(thread.canSend.value).toBe(false)
      thread.messageDraft.value = 'do not recreate a deleted conversation'
      await thread.sendMessage()
      expect(api.sendMessage).not.toHaveBeenCalled()
      expect(api.getConversations).not.toHaveBeenCalled()
    },
  )

  it('retains history when a response is missing its page and leaves the cursor retryable', async () => {
    api.getConversationMessages.mockResolvedValueOnce(page([3, 4], 'older-page'))
    const thread = existingThread()
    await flushPromises()
    api.getConversationMessages.mockResolvedValueOnce({ data: ref(null) })

    await thread.fetchThreadMessages(true)

    expect(thread.items.value.map((message) => message.key)).toEqual(['3', '4'])
    expect(thread.cursor.value).toBe('older-page')
    expect(thread.error.value).toBe('communication.invalidHistoryResponse')
  })

  it('starts a send readback immediately and ignores a poll that began before the send', async () => {
    const thread = existingThread()
    await flushPromises()
    let finish: ((result: ReturnType<typeof page>) => void) | undefined
    api.getConversationMessages.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const pending = thread.fetchThreadMessages()
    expect(thread.isLoading.value).toBe(true)
    thread.messageDraft.value = 'sent while polling'
    api.getConversationMessages.mockResolvedValueOnce(page([1, 2]))

    await thread.sendMessage()

    expect(api.getConversationMessages).toHaveBeenCalledTimes(3)
    expect(thread.items.value.map((message) => message.key)).toEqual(['1', '2'])
    if (!finish) throw new Error('Poll was not requested')
    finish(page([1]))
    await pending
    expect(thread.items.value.map((message) => message.key)).toEqual(['1', '2'])
    expect(thread.isLoading.value).toBe(false)
  })

  it('acknowledges a newer manual unread flag even when its message was acknowledged before', async () => {
    const read = { ...fixtureConversation, readThroughMessageId: 1, unreadCount: 0 }
    api.getConversationMessages.mockResolvedValueOnce(page([1], '', read, { revision: 10 }))
    const thread = existingThread()
    await flushPromises()
    await thread.markDisplayed(1)
    expect(api.updateConversation).not.toHaveBeenCalled()

    api.getConversationMessages.mockResolvedValueOnce(
      page([1], '', { ...read, markedUnread: true, unreadCount: 1 }, { revision: 11 }),
    )
    await thread.fetchThreadMessages()
    api.updateConversation.mockResolvedValueOnce({
      data: ref({ backendId: 'backend', revision: 12, unreadCount: 0 }),
    })
    await thread.markDisplayed(1)
    await thread.markDisplayed(1)

    expect(api.updateConversation).toHaveBeenCalledExactlyOnceWith('conversation-a', {
      readThroughMessageId: 1,
      markedUnread: false,
    })
  })

  it('keeps newer read state when an older acknowledgement or page completes', async () => {
    const thread = existingThread()
    await flushPromises()
    let finish: ((result: { data: Ref<MessageState> }) => void) | undefined
    api.updateConversation.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const pending = thread.markDisplayed(1)
    const markedUnread = {
      ...fixtureConversation,
      readThroughMessageId: 1,
      markedUnread: true,
      lineLabel: 'Current line',
    }
    api.getConversationMessages.mockResolvedValueOnce(page([1], '', markedUnread, { revision: 3 }))
    await thread.fetchThreadMessages()
    if (!finish) throw new Error('Acknowledgement was not requested')
    finish({ data: ref({ backendId: 'backend', revision: 2, unreadCount: 0 }) })
    await pending

    api.updateConversation.mockResolvedValueOnce({
      data: ref({ backendId: 'backend', revision: 4, unreadCount: 0 }),
    })
    await thread.markDisplayed(1)
    expect(api.updateConversation).toHaveBeenCalledTimes(2)
    expect(thread.lineLabel.value).toBe('Current line')

    api.getConversationMessages.mockResolvedValueOnce(
      page([1], '', { ...markedUnread, lineLabel: 'Stale line' }, { revision: 3 }),
    )
    await thread.fetchThreadMessages()
    await thread.markDisplayed(1)
    expect(thread.lineLabel.value).toBe('Current line')
    expect(api.updateConversation).toHaveBeenCalledTimes(2)
  })

  it('resets retained history and read revisions when the backend identity changes', async () => {
    api.getConversationMessages.mockResolvedValueOnce(
      page([1], '', { ...fixtureConversation, readThroughMessageId: 10 }, { revision: 50 }),
    )
    const thread = existingThread()
    await flushPromises()
    api.getConversationMessages.mockResolvedValueOnce(
      page(
        [2],
        '',
        { ...fixtureConversation, lineLabel: 'Replacement backend' },
        { backendId: 'replacement' },
      ),
    )

    await thread.fetchThreadMessages()
    api.updateConversation.mockResolvedValueOnce({
      data: ref({ backendId: 'replacement', revision: 2, unreadCount: 0 }),
    })
    await thread.markDisplayed(2)

    expect(thread.items.value.map((message) => message.key)).toEqual(['2'])
    expect(thread.lineLabel.value).toBe('Replacement backend')
    expect(api.updateConversation).toHaveBeenCalledExactlyOnceWith('conversation-a', {
      readThroughMessageId: 2,
      markedUnread: false,
    })
  })

  it('routes a new conversation to the normalized recipient returned by the API', async () => {
    let thread!: ReturnType<typeof useModemMessageThread>
    wrappers.push(
      mount({
        template: '<div />',
        setup() {
          thread = useModemMessageThread({
            modemId: computed(() => 'modem-1'),
            participant: computed(() => ''),
            isNewConversation: computed(() => true),
            activeProfileId: computed(() => 'sim-a'),
          })
        },
      }),
    )
    thread.newRecipient.value = '13800138000'
    thread.messageDraft.value = 'hello'

    await thread.sendMessage()

    expect(api.sendMessage).toHaveBeenCalledWith('modem-1', '13800138000', 'hello', 'sim-a')
    expect(router.replace).toHaveBeenCalledWith({
      name: 'modem-message-thread',
      params: { id: 'modem-1', participant: '+8613800138000' },
      query: { conversation: 'conversation-a', profile: 'sim-a' },
    })
    expect(thread.messageDraft.value).toBe('')
  })

  it('uses the participant display number from fetched thread messages', async () => {
    api.getConversations.mockResolvedValueOnce({
      data: ref({ items: [{ ...fixtureConversation, lineLabel: '', lineNumber: '+12025550123' }] }),
    })
    api.getConversationMessages.mockResolvedValueOnce(
      page([1], '', { ...fixtureConversation, lineLabel: '', lineNumber: '+12025550123' }),
    )
    const active = ref('sim-a')
    let thread!: ReturnType<typeof useModemMessageThread>
    wrappers.push(
      mount({
        template: '<div />',
        setup() {
          thread = useModemMessageThread({
            modemId: computed(() => 'modem-1'),
            participant: computed(() => '+12223334444'),
            isNewConversation: computed(() => false),
            defaultCountry: computed(() => 'US'),
            activeProfileId: computed(() => active.value),
          })
        },
      }),
    )

    await flushPromises()

    expect(thread.participantLabel.value).toBe('+1 (222) 333-4444')
    expect(thread.lineLabel.value).toBe('+1 (202) 555-0123')
    active.value = 'sim-b'
    expect(thread.participantLabel.value).toBe('+1 (222) 333-4444')
    expect(thread.lineLabel.value).toBe('+1 (202) 555-0123')
  })

  it('retains uncertain sends without resending on refresh and blocks an inactive SIM', async () => {
    const active = ref('sim-a')
    let thread!: ReturnType<typeof useModemMessageThread>
    wrappers.push(
      mount({
        template: '<div />',
        setup() {
          thread = useModemMessageThread({
            modemId: computed(() => 'modem-1'),
            participant: computed(() => '+12223334444'),
            isNewConversation: computed(() => false),
            activeProfileId: computed(() => active.value),
            conversationId: computed(() => 'conversation-a'),
          })
        },
      }),
    )
    await flushPromises()
    thread.messageDraft.value = 'keep this draft'
    api.sendMessage.mockRejectedValueOnce(new TypeError('network timeout'))
    await thread.sendMessage()
    await thread.fetchThreadMessages()
    expect(thread.messageDraft.value).toBe('keep this draft')
    expect(thread.sendError.value).toBe('modemDetail.messages.resultUnknown')
    expect(api.sendMessage).toHaveBeenCalledTimes(1)
    active.value = 'sim-b'
    await flushPromises()
    expect(thread.canSend.value).toBe(false)
    await thread.sendMessage()
    expect(api.sendMessage).toHaveBeenCalledTimes(1)
    expect(thread.canUseActive.value).toBe(true)
    thread.useActiveProfile()
    expect(thread.canSend.value).toBe(true)
    await thread.sendMessage()
    expect(api.sendMessage).toHaveBeenLastCalledWith(
      'modem-1',
      '+12223334444',
      'keep this draft',
      'sim-b',
    )
    expect(router.replace).toHaveBeenCalledWith(
      expect.objectContaining({ query: { conversation: 'conversation-a', profile: 'sim-b' } }),
    )
  })

  it('acknowledges displayed IDs only, without using the modem SMS status', async () => {
    let thread!: ReturnType<typeof useModemMessageThread>
    wrappers.push(
      mount({
        template: '<div />',
        setup() {
          thread = useModemMessageThread({
            modemId: computed(() => 'modem-1'),
            participant: computed(() => '+12223334444'),
            isNewConversation: computed(() => false),
            activeProfileId: computed(() => 'sim-a'),
            conversationId: computed(() => 'conversation-a'),
          })
        },
      }),
    )
    await flushPromises()
    await thread.markDisplayed(99)
    expect(api.updateConversation).not.toHaveBeenCalled()
    await thread.markDisplayed(1)
    expect(api.updateConversation).toHaveBeenCalledWith('conversation-a', {
      readThroughMessageId: 1,
      markedUnread: false,
    })
  })
})
