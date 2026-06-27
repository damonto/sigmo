import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import EsimProfileSection from '@/components/esim/EsimProfileSection.vue'
import type { EsimProfile } from '@/types/esim'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/apis/esim', () => ({
  useEsimApi: () => ({
    enableEsim: vi.fn(),
    updateEsimNickname: vi.fn(),
    deleteEsim: vi.fn(),
  }),
}))

const profiles: EsimProfile[] = [
  {
    id: 'active',
    name: 'Active',
    iccid: 'iccid-active',
    enabled: true,
    regionCode: 'US',
  },
  {
    id: 'inactive',
    name: 'Inactive',
    iccid: 'iccid-inactive',
    enabled: false,
    regionCode: 'US',
  },
]

const stubs = {
  AlertDialog: { template: '<div><slot /></div>' },
  AlertDialogCancel: { template: '<button type="button"><slot /></button>' },
  AlertDialogContent: { template: '<div><slot /></div>' },
  AlertDialogFooter: { template: '<div><slot /></div>' },
  AlertDialogHeader: { template: '<div><slot /></div>' },
  AlertDialogTitle: { template: '<p><slot /></p>' },
  Badge: { template: '<span><slot /></span>' },
  Button: {
    props: ['disabled', 'type'],
    template: '<button :type="type || \'button\'" :disabled="disabled"><slot /></button>',
  },
  Dialog: { template: '<div><slot /></div>' },
  DialogContent: { template: '<div><slot /></div>' },
  DialogFooter: { template: '<div><slot /></div>' },
  DialogHeader: { template: '<div><slot /></div>' },
  DialogTitle: { template: '<h2><slot /></h2>' },
  DropdownMenu: {
    name: 'DropdownMenu',
    emits: ['update:open'],
    template: '<div><slot /></div>',
  },
  DropdownMenuContent: { template: '<div><slot /></div>' },
  DropdownMenuItem: {
    props: ['disabled'],
    template: '<button type="button" :disabled="disabled"><slot /></button>',
  },
  DropdownMenuSeparator: { template: '<hr />' },
  DropdownMenuTrigger: { template: '<div><slot /></div>' },
  EsimProfileAvatar: { template: '<span />' },
  FormControl: { template: '<div><slot /></div>' },
  FormField: { template: '<div><slot :component-field="{}" /></div>' },
  FormItem: { template: '<div><slot /></div>' },
  FormLabel: { template: '<label><slot /></label>' },
  FormMessage: { template: '<span />' },
  Input: { template: '<input />' },
  Skeleton: { template: '<span />' },
  Spinner: { template: '<span v-bind="$attrs" />' },
  Switch: {
    props: ['modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<span role="switch" :aria-checked="modelValue ? \'true\' : \'false\'" :aria-disabled="disabled ? \'true\' : undefined" @click="$emit(\'update:modelValue\', !modelValue)"><slot /></span>',
  },
}

const mountSection = (props: Record<string, unknown> = {}) =>
  mount(EsimProfileSection, {
    props: {
      profiles: profiles.map((profile) => ({ ...profile })),
      modemId: 'modem-1',
      wifiCallingAvailable: true,
      'onUpdate:profiles': () => {},
      ...props,
    },
    global: {
      stubs,
    },
  })

const buttonWithText = (wrapper: ReturnType<typeof mountSection>, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`button containing ${text} not found`)
  }
  return button
}

describe('EsimProfileSection', () => {
  it('shows quick actions only for the active profile', () => {
    const wrapper = mountSection()

    expect(
      wrapper.findAll('button').filter((button) => button.text().includes('networkTitle')),
    ).toHaveLength(1)
    expect(
      wrapper.findAll('button').filter((button) => button.text().includes('wifiCallingTitle')),
    ).toHaveLength(1)
    expect(
      wrapper.findAll('button').filter((button) => button.text().includes('msisdnTitle')),
    ).toHaveLength(1)
    expect(
      wrapper.findAll('button').filter((button) => button.text().includes('actions.rename')),
    ).toHaveLength(2)
  })

  it('emits network connect and disconnect toggles', async () => {
    const wrapper = mountSection({ internetConnected: false })

    await buttonWithText(wrapper, 'networkTitle').trigger('click')
    expect(wrapper.emitted('toggle-network')?.[0]?.[1]).toBe(true)

    await wrapper.setProps({ internetConnected: true })
    await buttonWithText(wrapper, 'networkTitle').trigger('click')
    expect(wrapper.emitted('toggle-network')?.[1]?.[1]).toBe(false)
  })

  it('emits Wi-Fi Calling connect and disconnect toggles', async () => {
    const wrapper = mountSection({ wifiCallingEnabled: true, wifiCallingConnected: false })

    await buttonWithText(wrapper, 'wifiCallingTitle').trigger('click')
    expect(wrapper.emitted('toggle-wifi-calling')?.[0]?.[1]).toBe(true)

    await wrapper.setProps({ wifiCallingEnabled: true, wifiCallingConnected: true })
    await buttonWithText(wrapper, 'wifiCallingTitle').trigger('click')
    expect(wrapper.emitted('toggle-wifi-calling')?.[1]?.[1]).toBe(false)
  })

  it('shows a loading state while Wi-Fi Calling is connecting or disconnecting', () => {
    const wrapper = mountSection({ wifiCallingBusy: true })
    const action = buttonWithText(wrapper, 'wifiCallingTitle')

    expect(action.attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="wifi-calling-quick-action-loading"]').exists()).toBe(true)
  })

  it('emits profile action menu open changes', () => {
    const wrapper = mountSection()
    const menus = wrapper.findAllComponents({ name: 'DropdownMenu' })

    menus[0].vm.$emit('update:open', true)

    expect(wrapper.emitted('profile-actions-open-change')?.[0]?.[0]).toMatchObject({
      id: 'active',
    })
    expect(wrapper.emitted('profile-actions-open-change')?.[0]?.[1]).toBe(true)
  })

  it('emits the phone number edit action for the active profile', async () => {
    const wrapper = mountSection()

    await buttonWithText(wrapper, 'msisdnTitle').trigger('click')

    expect(wrapper.emitted('edit-phone-number')?.[0]?.[0]).toMatchObject({
      id: 'active',
    })
  })
})
