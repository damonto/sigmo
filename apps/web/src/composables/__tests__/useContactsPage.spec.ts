import { reactive, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { useContactsPage } from '@/composables/useContactsPage'
import { invalidateSession } from '@/lib/authSession'
import type { Contact } from '@/types/communication'

enableAutoUnmount(afterEach)
const api = vi.hoisted(() => ({ load: vi.fn(), deleteContact: vi.fn(), replace: vi.fn() }))
const route = reactive({ params: { id: '', contactId: 'alice' }, query: { number: '' } })
const contacts = ref<Contact[]>([])
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ replace: api.replace }),
}))
vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(true) }))
vi.mock('@/apis/communication', () => ({ communicationApi: api }))
vi.mock('@/composables/useCapabilities', () => ({
  useCapabilities: () => ({ hasFeature: () => true, fetchCapabilities: async () => {} }),
}))
vi.mock('@/composables/useContacts', () => ({
  useContacts: () => ({ contacts, load: api.load, match: () => undefined }),
}))
const mountPage = () => {
  let page: ReturnType<typeof useContactsPage> | undefined
  const wrapper = mount({
    template: '<div />',
    setup() {
      page = useContactsPage()
    },
  })
  if (!page) throw new Error('missing contact state')
  return { page, wrapper }
}
beforeEach(() => {
  vi.resetAllMocks()
  route.params.contactId = 'alice'
  route.params.id = ''
  route.query.number = ''
  contacts.value = [
    {
      id: 'alice',
      name: 'Alice',
      numbers: [],
      notes: '',
      avatarColor: '',
      hasAvatar: false,
      updatedAt: '',
    },
  ]
  api.load.mockResolvedValue(undefined)
})

describe('contact page lifecycle', () => {
  it('clears a loading error after retry succeeds', async () => {
    api.load.mockRejectedValueOnce(new Error('Offline'))
    const { page } = mountPage()
    await flushPromises()
    expect(page.error.value).toBe('Offline')
    await page.refresh()
    expect(page.error.value).toBe('')
    expect(page.loading.value).toBe(false)
  })

  it.each(['navigation', 'modem', 'unmount', 'session'])(
    'does not redirect after deletion finishes following %s',
    async (change) => {
      const { page, wrapper } = mountPage()
      await flushPromises()
      let finish: (() => void) | undefined
      api.deleteContact.mockImplementation(
        () =>
          new Promise<void>((resolve) => {
            finish = resolve
          }),
      )
      const deletion = page.remove()
      if (change === 'navigation') route.params.contactId = 'another'
      else if (change === 'modem') route.params.id = 'another'
      else if (change === 'session') invalidateSession()
      else wrapper.unmount()
      await flushPromises()
      finish?.()
      await deletion
      expect(api.deleteContact).toHaveBeenCalledWith('alice')
      expect(api.replace).not.toHaveBeenCalled()
    },
  )

  it.each(['', 'modem-a'])('keeps list, save and delete navigation in context: %s', async (id) => {
    route.params.id = id
    const { page } = mountPage()
    await flushPromises()
    const contact = contacts.value[0]
    if (!contact) throw new Error('missing contact')
    const detail = id
      ? { name: 'modem-contact', params: { id, contactId: contact.id } }
      : { name: 'contact', params: { contactId: contact.id } }
    expect(page.contactRoute(contact.id)).toEqual(detail)
    route.query.number = '10086'
    await page.closeDetails()
    expect(api.replace).toHaveBeenLastCalledWith({
      ...(id ? { name: 'modem-contacts', params: { id } } : { name: 'contacts' }),
      query: { number: '10086' },
    })
    await page.saved(contact)
    expect(api.replace).toHaveBeenLastCalledWith(detail)
    await page.remove()
    expect(api.replace).toHaveBeenLastCalledWith(
      id ? { name: 'modem-contacts', params: { id } } : { name: 'contacts' },
    )
  })

  it('prefills an unknown number without losing the modem when saving it', async () => {
    route.params.id = 'modem-a'
    route.query.number = '10086'
    const { page } = mountPage()
    await flushPromises()
    expect(page.editor.value?.numbers[0]?.number).toBe('10086')
    expect(contacts.value[0]?.numbers).toEqual([])
    route.params.id = 'modem-b'
    expect(page.editor.value).toBeUndefined()
  })

  it('rejects stale loading errors after switching to another contact', async () => {
    let fail: ((reason: Error) => void) | undefined
    api.load.mockImplementationOnce(
      () =>
        new Promise<void>((_, reject) => {
          fail = reject
        }),
    )
    const { page } = mountPage()
    await flushPromises()
    route.params.contactId = 'another'
    await flushPromises()
    fail?.(new Error('Old request'))
    await flushPromises()
    expect(page.error.value).toBe('')
    expect(page.loading.value).toBe(false)
  })
})
