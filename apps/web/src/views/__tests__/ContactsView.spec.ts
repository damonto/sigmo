import { computed, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, useRoute, useRouter } from 'vue-router'
import ContactsView from '@/views/ContactsView.vue'
import CommunicationComposer from '@/components/communication/CommunicationComposer.vue'
import ContactEditor from '@/components/contacts/ContactEditor.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import type { Contact } from '@/types/communication'

const selected = ref<Contact>()
const loading = ref(false)
const desktop = ref(true)
enableAutoUnmount(afterEach)
vi.mock('@/composables/useContactsPage', () => ({
  useContactsPage: () => {
    const route = useRoute()
    const router = useRouter()
    return {
      desktop,
      closeDetails: () =>
        router.replace(
          route.params.id
            ? { name: 'modem-contacts', params: { id: route.params.id } }
            : { name: 'contacts' },
        ),
      modemId: computed(() => route.params.id ?? ''),
      listRoute: ref({ name: 'contacts' }),
      contactRoute: (contactId: string) => ({ name: 'contact', params: { contactId } }),
      selected,
      visible: ref([]),
      supported: ref(true),
      loading,
      error: ref(''),
      search: ref(''),
      editor: ref(),
      deleting: ref(false),
      create: vi.fn(),
      saved: vi.fn(),
      remove: vi.fn(),
    }
  },
}))

const mountPage = async (locale = 'en', path = '/contacts') => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/contacts', name: 'contacts', component: ContactsView },
      { path: '/contacts/:contactId', name: 'contact', component: ContactsView },
      { path: '/modems/:id/contacts', name: 'modem-contacts', component: ContactsView },
      { path: '/modems/:id/contacts/:contactId', name: 'modem-contact', component: ContactsView },
    ],
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(ContactsView, {
    attachTo: document.body,
    global: {
      plugins: [router, createI18n({ legacy: false, locale, messages: { en, zh } })],
      stubs: { ContactAvatar: true, ContactEditor: true, CommunicationComposer: true },
    },
  })
  return wrapper
}

beforeEach(() => {
  desktop.value = true
  loading.value = false
  selected.value = {
    id: 'contact',
    name: 'Alice',
    notes: '',
    numbers: [],
    hasAvatar: false,
    avatarColor: '',
    updatedAt: '',
  }
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
})
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('contact communication shortcuts', () => {
  it('shows list skeletons until contacts have loaded', async () => {
    selected.value = undefined
    loading.value = true
    const wrapper = await mountPage()
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    expect(wrapper.find('[data-slot="skeleton"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain(en.communication.emptyContacts)
    loading.value = false
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.text()).toContain(en.communication.emptyContacts)
    wrapper.unmount()
  })
  it('disables both actions for a name-only contact', async () => {
    const wrapper = await mountPage()
    const actions = wrapper
      .findAll('button')
      .filter((button) => ['Message', 'Call'].includes(button.text()))
    expect(actions).toHaveLength(2)
    for (const button of actions) expect(button.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('requires a recipient choice for multi-number contacts before opening the composer', async () => {
    if (!selected.value) throw new Error('missing contact fixture')
    selected.value.numbers = [
      { number: '+14155552671', key: '+14155552671', label: 'mobile', region: 'US' },
      { number: '+442079460018', key: '+442079460018', label: 'work', region: 'GB' },
    ]
    const wrapper = await mountPage()
    const message = wrapper.findAll('button').find((button) => button.text() === 'Message')
    if (!message) throw new Error('missing message action')
    await message.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.findComponent(CommunicationComposer).exists()).toBe(false)
    const options = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]'))
    const work = options.find((option) => option.textContent?.includes('+442079460018'))
    expect(work).toBeDefined()
    work?.click()
    await flushPromises()
    const composer = wrapper.findComponent(CommunicationComposer)
    expect(composer.props('participant')).toBe('+442079460018')
    expect(composer.props('mode')).toBe('message')
    wrapper.unmount()
  })

  it.each(['Message', 'Call'])(
    'uses the saved local number for %s instead of its matching key',
    async (label) => {
      if (!selected.value) throw new Error('missing contact fixture')
      selected.value.numbers = [
        { number: '4155552671', key: '+14155552671', label: 'mobile', region: 'US' },
      ]
      const wrapper = await mountPage()
      const button = wrapper.findAll('button').find((entry) => entry.text() === label)
      if (!button) throw new Error('missing communication action')
      await button.trigger('click')
      expect(wrapper.getComponent(CommunicationComposer).props('participant')).toBe('4155552671')
      wrapper.unmount()
    },
  )

  it('localizes the contacts heading in Chinese', async () => {
    selected.value = undefined
    const wrapper = await mountPage('zh')
    expect(wrapper.get('h1').text()).toBe('联系人')
    wrapper.unmount()
  })

  it.each(['Message', 'Call'])('keeps %s on the modem used to open Contacts', async (label) => {
    if (!selected.value) throw new Error('missing contact fixture')
    selected.value.numbers = [{ number: '10086', key: '10086', label: '', region: '' }]
    const wrapper = await mountPage('en', '/modems/current/contacts')
    const button = wrapper.findAll('button').find((entry) => entry.text() === label)
    if (!button) throw new Error('missing communication action')
    await button.trigger('click')
    expect(wrapper.getComponent(CommunicationComposer).props('fixedModemId')).toBe('current')
    wrapper.unmount()
  })
})

describe('mobile contact details', () => {
  const openDialog = () => document.querySelector<HTMLElement>('[role="dialog"][data-state="open"]')
  const dialogButton = (name: string) => {
    const button = Array.from(openDialog()?.querySelectorAll('button') ?? []).find(
      (button) => button.textContent?.trim() === name || button.getAttribute('aria-label') === name,
    )
    if (!button) throw new Error(`missing ${name} button`)
    return button
  }
  it.each(['/contacts/contact', '/modems/current/contacts/contact'])(
    'opens a direct contact link in a dialog and closes to its list: %s',
    async (path) => {
      desktop.value = false
      const wrapper = await mountPage('en', path)
      await flushPromises()
      expect(openDialog()?.textContent).toContain('Alice')
      expect(wrapper.get('h1').text()).toBe('Contacts')
      expect(document.querySelectorAll('#contact-name')).toHaveLength(1)
      dialogButton('Close').click()
      await flushPromises()
      expect(openDialog()).toBeNull()
      expect(wrapper.vm.$router.currentRoute.value.path).toBe(path.replace(/\/contact$/, ''))
    },
  )

  it('returns to the contact after canceling edit or the message composer', async () => {
    desktop.value = false
    if (!selected.value) throw new Error('missing contact fixture')
    selected.value.numbers = [{ number: '12345', key: '12345', label: 'work', region: '' }]
    const wrapper = await mountPage('en', '/modems/current/contacts/contact')
    await flushPromises()
    dialogButton('Edit contact').click()
    await flushPromises()
    expect(openDialog()).toBeNull()
    wrapper.getComponent(ContactEditor).vm.$emit('close')
    await flushPromises()
    expect(openDialog()?.textContent).toContain('Alice')
    dialogButton('Message').click()
    await flushPromises()
    expect(openDialog()).toBeNull()
    const composer = wrapper.getComponent(CommunicationComposer)
    expect(composer.props('fixedModemId')).toBe('current')
    composer.vm.$emit('close')
    await flushPromises()
    expect(openDialog()?.textContent).toContain('Alice')
  })

  it('opens delete confirmation directly and returns to the contact on cancel', async () => {
    desktop.value = false
    await mountPage('en', '/contacts/contact')
    await flushPromises()
    dialogButton('Delete').click()
    await flushPromises()
    expect(openDialog()?.textContent).toContain(en.communication.deleteContactDescription)
    dialogButton('Close').click()
    await flushPromises()
    expect(openDialog()?.textContent).toContain('Alice')
  })

  it('switches between the dialog and desktop panel without duplicating the contact', async () => {
    desktop.value = false
    const wrapper = await mountPage('en', '/contacts/contact')
    await flushPromises()
    expect(openDialog()).not.toBeNull()
    desktop.value = true
    await flushPromises()
    expect(openDialog()).toBeNull()
    expect(wrapper.get('#contact-name').text()).toBe('Alice')
    expect(document.querySelectorAll('#contact-name')).toHaveLength(1)
    desktop.value = false
    await flushPromises()
    expect(openDialog()?.textContent).toContain('Alice')
  })
})
