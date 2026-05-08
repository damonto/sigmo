import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModemDetailHeader from '@/components/modem/ModemDetailHeader.vue'
import ModemSignalStatus from '@/components/modem/ModemSignalStatus.vue'
import SimSlotSwitcher from '@/components/modem/SimSlotSwitcher.vue'
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

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')

  return {
    ...actual,
    useRoute: () => route,
    useRouter: () => router,
  }
})

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

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const modem = (registrationState = 'Roaming'): Modem => ({
  manufacturer: 'Quectel',
  id: 'modem-1',
  firmwareRevision: '1.0.0',
  hardwareRevision: '1.0',
  name: 'Modem 1',
  number: '',
  sim: {
    active: true,
    operatorName: 'Carrier',
    operatorIdentifier: '00101',
    regionCode: 'us',
    identifier: 'sim-1',
  },
  slots: [],
  accessTechnology: 'LTE',
  registrationState,
  registeredOperator: {
    name: 'Carrier',
    code: '00101',
  },
  signalQuality: 67,
  supportsEsim: true,
})

const headerStubs = {
  Button: {
    props: ['type'],
    template: '<button :type="type || \'button\'" v-bind="$attrs"><slot /></button>',
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
  ModemStickyTopBar: {
    props: ['title', 'backLabel', 'backTo', 'show'],
    template: '<div data-testid="sticky-top-bar"><slot name="right" /></div>',
  },
  Skeleton: {
    template: '<span />',
  },
}

beforeEach(() => {
  router.push.mockClear()
  route.name = 'modem-detail'
  modemHarness.modems = []
  modemHarness.fetchModems.mockReset()
  modemHarness.fetchModems.mockResolvedValue(undefined)
})

describe('ModemSignalStatus', () => {
  it('shows the roaming label with the signal percentage', () => {
    const wrapper = mount(ModemSignalStatus, {
      props: {
        signalQuality: 67,
        registrationState: 'Roaming',
      },
    })

    expect(wrapper.text()).toContain('R')
    expect(wrapper.text()).toContain('67%')
  })

  it('shows only the signal percentage for ordinary registration states', () => {
    const wrapper = mount(ModemSignalStatus, {
      props: {
        signalQuality: 72,
        registrationState: 'Registered',
      },
    })

    expect(wrapper.text().trim()).toBe('72%')
  })
})

describe('ModemDetailHeader', () => {
  it('keeps detail actions free of signal status', () => {
    const wrapper = mount(ModemDetailHeader, {
      props: {
        modem: modem(),
        isLoading: false,
        showDetailsAction: true,
      },
      global: {
        stubs: headerStubs,
      },
    })

    const statuses = wrapper.findAll('[data-testid="modem-signal-status"]')
    expect(statuses).toHaveLength(0)
    expect(wrapper.findAll('button[aria-label="modemDetail.tabs.detail"]')).toHaveLength(2)
  })

  it('switches to another modem from the title menu', async () => {
    const current = modem()
    const next = {
      ...modem('Registered'),
      id: 'modem-2',
      name: 'Modem 2',
      signalQuality: 72,
    }
    modemHarness.modems = [current, next]

    const wrapper = mount(ModemDetailHeader, {
      props: {
        modem: current,
        isLoading: false,
        showDetailsAction: true,
      },
      global: {
        stubs: headerStubs,
      },
    })

    expect(wrapper.find('button[aria-label="modemDetail.switchModem"]').exists()).toBe(true)
    const nextButton = wrapper.findAll('button').find((button) => button.text().includes('Modem 2'))
    expect(nextButton).toBeDefined()
    await nextButton?.trigger('click')

    expect(router.push).toHaveBeenCalledWith({
      name: 'modem-detail',
      params: { id: 'modem-2' },
    })
  })
})

describe('SimSlotSwitcher', () => {
  it('shows signal status on the SIM row', () => {
    const wrapper = mount(SimSlotSwitcher, {
      props: {
        modelValue: 'slot-1',
        slots: [
          {
            active: false,
            operatorName: 'Carrier',
            operatorIdentifier: '00101',
            regionCode: 'us',
            identifier: 'slot-0',
          },
          {
            active: true,
            operatorName: 'Carrier',
            operatorIdentifier: '00101',
            regionCode: 'us',
            identifier: 'slot-1',
          },
        ],
        signalQuality: 67,
        registrationState: 'Roaming',
      },
      global: {
        stubs: {
          AlertDialog: {
            template: '<div><slot /></div>',
          },
          AlertDialogCancel: {
            template: '<button type="button"><slot /></button>',
          },
          AlertDialogContent: {
            template: '<div><slot /></div>',
          },
          AlertDialogFooter: {
            template: '<div><slot /></div>',
          },
          AlertDialogHeader: {
            template: '<div><slot /></div>',
          },
          AlertDialogTitle: {
            template: '<p><slot /></p>',
          },
          Button: {
            template: '<button type="button"><slot /></button>',
          },
          Label: {
            props: ['for'],
            template: '<label :for="$props.for"><slot /></label>',
          },
          RadioGroup: {
            props: ['modelValue'],
            template: '<div role="radiogroup"><slot /></div>',
          },
          RadioGroupItem: {
            props: ['id', 'value'],
            template: '<button :id="id" type="button" role="radio" />',
          },
          Spinner: {
            template: '<span />',
          },
        },
      },
    })

    const status = wrapper.find('[data-testid="modem-signal-status"]')
    expect(status.exists()).toBe(true)
    expect(status.text()).toContain('R')
    expect(status.text()).toContain('67%')
  })
})
