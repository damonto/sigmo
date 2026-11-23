import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { RouterView } from 'vue-router'

import router from '@/router'
import type { Modem } from '@/types/modem'

enableAutoUnmount(afterEach)

const harness = vi.hoisted(() => ({
  desktop: true,
  modems: [] as Modem[],
  fetchModems: vi.fn(),
}))

vi.mock('@vueuse/core', async (importOriginal) => {
  const { ref } = await import('vue')
  return {
    ...(await importOriginal<typeof import('@vueuse/core')>()),
    useMediaQuery: () => ref(harness.desktop),
  }
})
vi.mock('@/composables/useModems', async () => {
  const { ref } = await import('vue')
  return {
    useModems: () => ({
      modems: ref(harness.modems),
      isLoading: ref(false),
      fetchModems: harness.fetchModems,
    }),
  }
})
vi.mock('@/composables/useModemResource', async () => {
  const { ref } = await import('vue')
  return { useModemResource: () => ({ modem: ref(null), isLoading: ref(false) }) }
})
vi.mock('@/composables/useSettingsContext', () => ({
  provideSettingsContext: () => ({ load: vi.fn() }),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ check: async () => true }) }))
vi.mock('@/stores/license', () => ({ useLicenseStore: () => ({ check: async () => true }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/views/HomeView.vue', () => ({ default: { template: '<main>Home</main>' } }))
vi.mock('@/views/ModemDetailView.vue', () => ({
  default: { template: '<section>Details</section>' },
}))
vi.mock('@/views/ModemMessagesView.vue', () => ({
  default: { template: '<section>Messages</section>' },
}))
vi.mock('@/views/ModemMessageThreadView.vue', () => ({
  default: { template: '<section>Thread</section>' },
}))
vi.mock('@/views/ContactsView.vue', () => ({
  default: { template: '<section>Contacts</section>' },
}))

const modem = (id: string, name: string): Modem => ({
  manufacturer: 'Quectel',
  id,
  primaryPort: '/dev/cdc-wdm0',
  firmwareRevision: '1.0.0',
  hardwareRevision: '1.0',
  name,
  number: '',
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
    slot: 1,
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '00101',
    regionCode: 'us',
    identifier: 'sim-1',
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState: 'Registered',
  registeredOperator: {
    name: 'Carrier',
    code: '00101',
  },
  signalQuality: 67,
  airplaneMode: false,
  simKind: 'euicc',
})

const mountRoute = async (path: string) => {
  await router.push(path)
  return mount(RouterView, {
    global: {
      plugins: [router],
      stubs: {
        CommunicationMessagesPanel: { template: '<section data-testid="threads" />' },
        CommunicationMessageThread: true,
        SettingsView: true,
        SettingsProxyView: true,
      },
    },
  })
}

beforeEach(() => {
  harness.desktop = true
  harness.modems = [modem('first', 'RM520N'), modem('second', 'Office')]
  harness.fetchModems.mockClear()
})

describe('device workspace', { timeout: 20_000 }, () => {
  it('opens the first device from home and keeps the sidebar mounted', async () => {
    const wrapper = await mountRoute('/')
    const list = wrapper.get('aside').element

    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/modems/first'), {
      timeout: 10_000,
    })
    expect(wrapper.get('h1').text()).toBe('RM520N')
    expect(wrapper.get('aside').element).toBe(list)
    const device = wrapper.get('aside nav a[aria-current="true"]')
    expect(
      device.findAll('[title="Carrier"], [title="RM520N"]').map((label) => label.text()),
    ).toEqual(['RM520N', 'Carrier'])
    expect(harness.fetchModems).toHaveBeenCalledTimes(1)
  })

  it('shows each device connection state and keeps the selected device addressable', async () => {
    harness.modems = [
      { ...modem('offline', 'Spare'), state: 'disabled', airplaneMode: true },
      modem('first', 'RM520N'),
      {
        ...modem('wifi', 'Office'),
        state: 'disabled',
        airplaneMode: true,
        wifiCallingConnected: true,
      },
    ]
    const wrapper = await mountRoute('/modems/offline')
    const sidebar = wrapper.get('aside')
    expect(sidebar.get('a[href="/modems/first"]').text()).toContain(
      'modemDetail.connection.connected',
    )
    expect(sidebar.get('a[href="/modems/wifi"]').text()).toContain(
      'modemDetail.connection.connected',
    )
    expect(sidebar.get('a[aria-current="true"]').text()).toContain('Spare')
    expect(sidebar.get('a[aria-current="true"]').text()).toContain(
      'modemDetail.connection.disconnected',
    )
    expect(wrapper.get('header').text()).toContain('modemDetail.connection.disconnected')
  })

  it('swaps the sidebar menu and returns to the device page that opened settings', async () => {
    const devicePath = '/modems/second/messages?profile=sim-1'
    const wrapper = await mountRoute(devicePath)
    const globalNav = 'header nav[aria-label="communication.navigation"]'
    expect(wrapper.find(globalNav).exists()).toBe(false)
    expect(wrapper.get('aside > div').text()).toBe('home.title')

    await wrapper.get('aside a[aria-label="home.globalSettings"]').trigger('click', { button: 0 })
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('settings'), {
      timeout: 10_000,
    })
    expect(wrapper.find(globalNav).exists()).toBe(false)
    expect(wrapper.get('aside > div').text()).toBe('home.title')
    expect(wrapper.find('aside a[aria-label="home.globalSettings"]').exists()).toBe(false)

    await wrapper.get('aside nav a[href="/settings/proxy"]').trigger('click', { button: 0 })
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('settings-proxy'), {
      timeout: 10_000,
    })
    expect(wrapper.find(globalNav).exists()).toBe(false)

    const back = wrapper.get('aside > div a[aria-label="settings.backToDevices"]')
    expect(back.attributes('href')).toBe(devicePath)
    await back.trigger('click', { button: 0 })
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe(devicePath), {
      timeout: 10_000,
    })
    expect(wrapper.find(globalNav).exists()).toBe(false)
  })

  it.each([
    { path: '/modems/first/messages/alice?profile=old', target: '/modems/second/messages' },
    { path: '/modems/first/contacts/alice', target: '/modems/second/contacts' },
  ])('switches devices within the current section from $path', async ({ path, target }) => {
    const wrapper = await mountRoute(path)
    const sidebar = wrapper.get('aside')
    const content = wrapper.get('[class~="@container/modem"]').element
    content.scrollTop = 80
    expect(router.currentRoute.value.fullPath).toBe(path)
    expect(sidebar.get('a[aria-current="true"]').text()).toContain('RM520N')
    expect(sidebar.get('a[aria-current="true"]').attributes('href')).toBe(path)

    await sidebar.get(`a[href="${target}"]`).trigger('click', { button: 0 })

    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe(target), {
      timeout: 10_000,
    })
    expect(wrapper.get('h1').text()).toBe('Office')
    expect(sidebar.get('a[aria-current="true"]').text()).toContain('Office')
    expect(content.scrollTop).toBe(0)
  })

  it('keeps home available on mobile', async () => {
    harness.desktop = false
    const wrapper = await mountRoute('/')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/')
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.get('main').text()).toBe('Home')
  })

  it('keeps the desktop device list available with no devices', async () => {
    harness.modems = []
    const wrapper = await mountRoute('/')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/')
    expect(wrapper.get('aside nav').findAll('a')).toHaveLength(0)
    expect(wrapper.find('header nav[aria-label="communication.navigation"]').exists()).toBe(false)
  })
})

it(
  'keeps the same desktop messages card before and after opening a thread',
  { timeout: 20_000 },
  async () => {
    const wrapper = await mountRoute('/messages')
    const listCard = wrapper.get('[data-testid="threads"]').element.parentElement
    const cardClasses = ['lg:rounded-xl', 'lg:border', 'lg:bg-card/40', 'lg:p-4']
    expect(Array.from(listCard?.classList ?? [])).toEqual(expect.arrayContaining(cardClasses))

    await router.push('/messages/thread-1')
    await flushPromises()

    expect(wrapper.get('[data-testid="threads"]').element.parentElement).toBe(listCard)
    expect(Array.from(listCard?.classList ?? [])).toEqual(expect.arrayContaining(cardClasses))
  },
)
