import { flushPromises, mount } from '@vue/test-utils'
import { computed } from 'vue'
import { describe, expect, it, beforeEach, vi } from 'vitest'

import { useModemMessageThread } from '@/composables/useModemMessageThread'

const api = vi.hoisted(() => ({
  getMessagesByParticipant: vi.fn(),
  deleteMessagesByParticipant: vi.fn(),
  sendMessage: vi.fn(),
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
  beforeEach(() => {
    vi.clearAllMocks()
    api.getMessagesByParticipant.mockResolvedValue({ data: { value: [] } })
    api.sendMessage.mockResolvedValue({ data: { value: { to: '+8613800138000' } } })
  })

  it('routes a new conversation to the normalized recipient returned by the API', async () => {
    let thread!: ReturnType<typeof useModemMessageThread>
    mount({
      template: '<div />',
      setup() {
        thread = useModemMessageThread({
          modemId: computed(() => 'modem-1'),
          participant: computed(() => ''),
          isNewConversation: computed(() => true),
        })
      },
    })
    thread.newRecipient.value = '13800138000'
    thread.messageDraft.value = 'hello'

    await thread.sendMessage()

    expect(api.sendMessage).toHaveBeenCalledWith('modem-1', '13800138000', 'hello')
    expect(router.replace).toHaveBeenCalledWith({
      name: 'modem-message-thread',
      params: { id: 'modem-1', participant: '+8613800138000' },
    })
    expect(api.getMessagesByParticipant).toHaveBeenCalledWith('modem-1', '+8613800138000')
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
    mount({
      template: '<div />',
      setup() {
        thread = useModemMessageThread({
          modemId: computed(() => 'modem-1'),
          participant: computed(() => '+12223334444'),
          isNewConversation: computed(() => false),
          defaultCountry: computed(() => 'US'),
        })
      },
    })

    await flushPromises()

    expect(thread.participantLabel.value).toBe('(222) 333-4444')
  })
})
