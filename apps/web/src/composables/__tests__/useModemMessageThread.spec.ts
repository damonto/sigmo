import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import type { Conversation, MessageResponse } from '@/types/message'

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
  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  })
  beforeEach(() => {
    vi.clearAllMocks()
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
    api.getConversationMessages.mockResolvedValue({ data: ref([fixtureMessage]) })
    api.updateConversation.mockResolvedValue({
      data: ref({ backendId: 'backend', revision: 2, unreadCount: 0 }),
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
    api.getMessagesByParticipant.mockResolvedValueOnce({
      data: {
        value: [
          {
            id: 1,
            sender: '+12223334444',
            recipient: '+8613344445555',
            text: 'hello',
            timestamp: '2026-05-29T10:00:00Z',
            status: 'received',
            incoming: true,
            wifiCalling: false,
          },
        ],
      },
    })
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
            activeProfileId: computed(() => 'sim-a'),
          })
        },
      }),
    )

    await flushPromises()

    expect(thread.participantLabel.value).toBe('(222) 333-4444')
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
