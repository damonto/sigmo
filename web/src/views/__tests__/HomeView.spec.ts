import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import HomeView from '@/views/HomeView.vue'
import type { Modem } from '@/types/modem'

const modemHarness = vi.hoisted(() => ({
  nextModems: [] as Modem[],
  fetchModems: vi.fn(),
}))

vi.mock('@/composables/useModems', async () => {
  const { computed, ref } = await vi.importActual<typeof import('vue')>('vue')

  const modems = ref<Modem[]>([])
  const isFetching = ref(false)

  return {
    useModems: () => ({
      modems,
      isLoading: computed(() => isFetching.value),
      fetchModems: async () => {
        isFetching.value = true
        try {
          await modemHarness.fetchModems()
          modems.value = modemHarness.nextModems
        } finally {
          isFetching.value = false
        }
      },
    }),
  }
})

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

const modem = (id: string): Modem => ({
  manufacturer: 'Quectel',
  id,
  firmwareRevision: '1.0.0',
  hardwareRevision: '1.0',
  name: `Modem ${id}`,
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
  registrationState: 'Registered',
  registeredOperator: {
    name: 'Carrier',
    code: '00101',
  },
  signalQuality: 72,
  supportsEsim: true,
})

const mountView = async () => {
  const wrapper = mount(HomeView, {
    global: {
      stubs: {
        HomeHeader: {
          props: ['subtitle', 'isLoading'],
          emits: ['refresh'],
          template: '<button type="button" @click="$emit(\'refresh\')">{{ subtitle }}</button>',
        },
        HomeModemList: {
          props: ['items', 'isLoading'],
          template: '<div data-testid="modem-list">{{ items.length }}</div>',
        },
      },
    },
  })

  await flushPromises()
  return wrapper
}

describe('HomeView', () => {
  beforeEach(() => {
    modemHarness.fetchModems.mockReset()
    modemHarness.fetchModems.mockResolvedValue(undefined)
    modemHarness.nextModems = []
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it.each([
    { name: 'no modems', modems: [], wantCount: '0' },
    { name: 'one modem', modems: [modem('modem-1')], wantCount: '1' },
    { name: 'multiple modems', modems: [modem('modem-1'), modem('modem-2')], wantCount: '2' },
  ])('loads and renders $name', async ({ modems, wantCount }) => {
    modemHarness.nextModems = modems

    const wrapper = await mountView()

    expect(modemHarness.fetchModems).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="modem-list"]').text()).toBe(wantCount)
  })
})
