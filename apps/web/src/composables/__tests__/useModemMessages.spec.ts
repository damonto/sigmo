import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemMessages } from '@/composables/useModemMessages'

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
