import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModemDesktopTopNav from '@/components/modem/ModemDesktopTopNav.vue'
import type { Modem } from '@/types/modem'

const router = vi.hoisted(() => ({
  push: vi.fn(),
}))

const route = vi.hoisted(() => ({
  name: 'modem-detail' as string | null,
}))

const modemHarness = vi.hoisted(() => ({
  modems: [] as Modem[],
  fetchModems: vi.fn(),
}))

vi.mock('vue-router', () => ({
  RouterLink: {
    props: ['to'],
    template: '<a><slot /></a>',
  },
  useRoute: () => route,
  useRouter: () => router,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/composables/useModems', async () => {
  const { computed } = await vi.importActual<typeof import('vue')>('vue')

  return {
    useModems: () => ({
      modems: computed(() => modemHarness.modems),
      isLoading: computed(() => false),
      fetchModems: modemHarness.fetchModems,
    }),
  }
})

const modem = (id: string, name: string): Modem => ({
  manufacturer: 'Quectel',
  id,
  firmwareRevision: '1.0.0',
  hardwareRevision: '1.0',
  name,
  number: '',
  state: 'registered',
  unlockRequired: 'none',
  unlockSupported: false,
  sim: {
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
  supportsEsim: true,
})

const stubs = {
  Button: {
    template: '<button type="button"><slot /></button>',
  },
  DropdownMenu: {
    template: '<div><slot /></div>',
  },
  DropdownMenuContent: {
    template: '<div><slot /></div>',
  },
  DropdownMenuItem: {
    props: ['disabled'],
    template: '<button type="button" :disabled="disabled"><slot /></button>',
  },
  DropdownMenuTrigger: {
    template: '<div><slot /></div>',
  },
  ModemSignalStatus: {
    template: '<span />',
  },
  RegionFlag: {
    template: '<span />',
  },
}

const mountNav = () =>
  mount(ModemDesktopTopNav, {
    props: {
      items: [],
      modemId: '869710031623444',
    },
    global: {
      stubs,
    },
  })

describe('ModemDesktopTopNav', () => {
  beforeEach(() => {
    route.name = 'modem-detail'
    router.push.mockReset()
    modemHarness.fetchModems.mockReset()
    modemHarness.fetchModems.mockResolvedValue(undefined)
    modemHarness.modems = [modem('869710031623444', 'RM520N')]
  })

  it('shows the modem name and IMEI in the desktop header', () => {
    const wrapper = mountNav()

    expect(wrapper.text()).toContain('RM520N')
    expect(wrapper.text()).toContain('869710031623444')
  })

  it('switches modems from the title menu on the current modem route', async () => {
    route.name = 'modem-settings-internet'
    modemHarness.modems = [modem('869710031623444', 'RM520N'), modem('869710031623555', 'Office')]
    const wrapper = mountNav()

    const nextButton = wrapper.findAll('button').find((button) => button.text().includes('Office'))
    expect(nextButton).toBeDefined()
    await nextButton?.trigger('click')

    expect(router.push).toHaveBeenCalledWith({
      name: 'modem-settings-internet',
      params: { id: '869710031623555' },
    })
  })
})
