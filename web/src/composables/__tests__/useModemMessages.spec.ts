import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useModemMessages } from '@/composables/useModemMessages'

const api = vi.hoisted(() => ({
  getMessages: vi.fn(),
  deleteMessagesByParticipant: vi.fn(),
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
  })

  it('reloads conversations from the API when the search query changes', async () => {
    const searchQuery = ref('')
    let messages!: ReturnType<typeof useModemMessages>

    mount({
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
    expect(api.getMessages).toHaveBeenNthCalledWith(1, 'modem-1', '')
    expect(api.getMessages).toHaveBeenNthCalledWith(2, 'modem-1', 'balance')
  })
})
