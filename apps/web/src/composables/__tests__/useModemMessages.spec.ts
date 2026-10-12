import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemMessages } from '@/composables/useModemMessages'
import type { Conversation } from '@/types/message'

const api = vi.hoisted(() => ({
  getMessages: vi.fn(),
  deleteMessagesByParticipant: vi.fn(),
  getConversations: vi.fn(),
  deleteConversation: vi.fn(),
}))

vi.mock('@/apis/message', () => ({
  useMessageApi: () => api,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

describe('useModemMessages', () => {
  it('formats participant and line numbers while preserving names and historical regions', async () => {
    let messages!: ReturnType<typeof useModemMessages>
    const wrapper = mount({
      template: '<div />',
      setup() {
        messages = useModemMessages(
          computed(() => 'modem-1'),
          computed(() => 'US'),
        )
      },
    })
    await flushPromises()
    const conversation: Conversation = {
      id: 'conversation',
      profileId: 'sim-1',
      modemId: 'modem-1',
      participant: '+12025550123',
      lineLabel: '',
      lineNumber: '+12025550124',
      readThroughMessageId: 0,
      lastMessageId: 1,
      markedUnread: false,
      unreadCount: 0,
      canSend: true,
      latestMessage: {
        id: 1,
        sender: '+12025550123',
        recipient: '+12025550124',
        text: 'hello',
        timestamp: '2026-05-29T10:00:00Z',
        status: 'received',
        incoming: true,
        wifiCalling: false,
      },
    }
    messages.conversations.value = [
      conversation,
      { ...conversation, id: 'named', participant: 'BANK123', lineLabel: 'Work' },
      { ...conversation, id: 'history', modemId: '', canSend: false },
      {
        ...conversation,
        id: 'national-history',
        canSend: false,
        participant: '2025550123',
        lineNumber: '2025550124',
      },
    ]
    expect(
      messages.items.value.map(({ participantLabel, lineLabel }) => ({
        participantLabel,
        lineLabel,
      })),
    ).toEqual([
      { participantLabel: '+1 (202) 555-0123', lineLabel: '+1 (202) 555-0124' },
      { participantLabel: 'BANK123', lineLabel: 'Work' },
      { participantLabel: '+1 (202) 555-0123', lineLabel: '+1 (202) 555-0124' },
      { participantLabel: '2025550123', lineLabel: '2025550124' },
    ])
    expect(messages.items.value[0]?.participantValue).toBe('+12025550123')
    wrapper.unmount()
  })

  beforeEach(() => {
    vi.clearAllMocks()
    api.getMessages.mockResolvedValue({ data: ref([]) })
    api.deleteMessagesByParticipant.mockResolvedValue({ data: ref(undefined) })
    api.getConversations.mockResolvedValue({
      data: ref({ backendId: 'backend', revision: 1, unreadCount: 0, items: [] }),
    })
  })

  it('reloads conversations from the API when the search query changes', async () => {
    const searchQuery = ref('')
    let messages!: ReturnType<typeof useModemMessages>

    const wrapper = mount({
      template: '<div />',
      setup() {
        messages = useModemMessages(
          computed(() => 'modem-1'),
          computed(() => 'US'),
          computed(() => searchQuery.value.trim()),
        )
        return {}
      },
    })
    await flushPromises()

    searchQuery.value = 'balance'
    await flushPromises()

    expect(messages.items.value).toEqual([])
    expect(api.getConversations).toHaveBeenNthCalledWith(1, {
      modemId: 'modem-1',
      q: '',
      unassigned: false,
    })
    expect(api.getConversations).toHaveBeenNthCalledWith(2, {
      modemId: 'modem-1',
      q: 'balance',
      unassigned: false,
    })
    wrapper.unmount()
  })

  it('requests historical lines independently', async () => {
    let messages!: ReturnType<typeof useModemMessages>
    const wrapper = mount({
      template: '<div />',
      setup() {
        messages = useModemMessages(computed(() => 'modem-1'))
      },
    })
    await flushPromises()
    messages.historical.value = true
    await flushPromises()
    expect(api.getConversations).toHaveBeenLastCalledWith({
      modemId: undefined,
      q: '',
      unassigned: true,
    })
    wrapper.unmount()
  })
})
