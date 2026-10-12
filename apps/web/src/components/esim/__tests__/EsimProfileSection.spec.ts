import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import EsimProfileSection from '@/components/esim/EsimProfileSection.vue'
import type { EsimProfile } from '@/types/esim'

enableAutoUnmount(afterEach)

const updateEsimNickname = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('@/apis/esim', () => ({
  useEsimApi: () => ({
    enableEsim: vi.fn(),
    updateEsimNickname,
    deleteEsim: vi.fn(),
  }),
}))

vi.mock('@/apis/reminder', () => ({
  useReminderApi: () => ({
    saveEsimReminder: vi.fn(),
    deleteEsimReminder: vi.fn(),
  }),
}))

const profiles: EsimProfile[] = [
  {
    id: 'active',
    seId: 'default',
    seLabel: 'eUICC',
    name: 'Active',
    iccid: 'iccid-active',
    isdPAID: 'A000000559',
    enabled: true,
    serviceProviderName: 'Carrier Active',
    profileName: 'Active Line',
    profileNickname: 'Active',
    profileStateName: 'enabled',
    profileClass: 'operational',
    profileOwner: { mcc: '208', mnc: '09', gid1: '6332' },
    regionCode: 'US',
  },
  {
    id: 'inactive',
    seId: 'default',
    seLabel: 'eUICC',
    name: 'Inactive',
    iccid: 'iccid-inactive',
    enabled: false,
    serviceProviderName: 'Carrier Inactive',
    profileName: 'Inactive Line',
    profileStateName: 'disabled',
    profileClass: 'operational',
    profileOwner: { mcc: '310', mnc: '260' },
    regionCode: 'US',
  },
]

const stubs = {
  AlertDialog: { template: '<div><slot /></div>' },
  AlertDialogCancel: { template: '<button type="button"><slot /></button>' },
  AlertDialogContent: { template: '<div><slot /></div>' },
  AlertDialogDescription: { template: '<p><slot /></p>' },
  AlertDialogFooter: { template: '<div><slot /></div>' },
  AlertDialogHeader: { template: '<div><slot /></div>' },
  AlertDialogTitle: { template: '<p><slot /></p>' },
  Badge: { template: '<span><slot /></span>' },
  Button: {
    props: ['disabled', 'type'],
    template:
      '<button v-bind="$attrs" :type="type || \'button\'" :disabled="disabled"><slot /></button>',
  },
  Dialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
  DialogContent: { template: '<div><slot /></div>' },
  DialogDescription: { template: '<p><slot /></p>' },
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
  EsimProfileDetailsDialog: {
    props: ['open', 'profile'],
    template:
      '<section v-if="open" data-testid="profile-details"><span>{{ profile?.serviceProviderName }}</span><span>{{ profile?.profileName }}</span><span>{{ profile?.profileOwner?.mcc }}</span></section>',
  },
  Skeleton: { template: '<span />' },
  Spinner: { template: '<span v-bind="$attrs" />' },
  Switch: {
    props: ['modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<span role="switch" :aria-checked="modelValue ? \'true\' : \'false\'" :aria-disabled="disabled ? \'true\' : undefined" @click="$emit(\'update:modelValue\', !modelValue)"><slot /></span>',
  },
}

const mountSection = (props: Record<string, unknown> = {}, realDialog = false) =>
  mount(EsimProfileSection, {
    attachTo: realDialog ? document.body : undefined,
    props: {
      profiles: profiles.map((profile) => ({ ...profile })),
      modemId: 'modem-1',
      wifiCallingAvailable: true,
      'onUpdate:profiles': () => {},
      ...props,
    },
    global: {
      stubs: {
        ...stubs,
        ...(realDialog
          ? {
              Dialog: false,
              DialogContent: false,
              DialogHeader: false,
              DialogTitle: false,
              DialogDescription: false,
              DialogFooter: false,
              DialogPortal: { template: '<div><slot /></div>' },
            }
          : {}),
      },
    },
  })

const buttonWithText = (wrapper: ReturnType<typeof mountSection>, text: string, index = 0) => {
  const button = wrapper.findAll('button').filter((item) => item.text().includes(text))[index]
  if (!button) {
    throw new Error(`button containing ${text} not found`)
  }
  return button
}

describe('EsimProfileSection', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    updateEsimNickname.mockReset()
    updateEsimNickname.mockResolvedValue({ data: { value: undefined } })
  })

  it.each([
    { name: '   ', error: 'modemDetail.validation.required' },
    { name: 'a'.repeat(65), error: 'modemDetail.validation.maxBytes' },
    { name: '中'.repeat(22), error: 'modemDetail.validation.maxBytes' },
  ])('rejects an invalid nickname: $name', async ({ name, error }) => {
    const wrapper = mountSection()
    await buttonWithText(wrapper, 'actions.rename').trigger('click')
    await flushPromises()

    await wrapper.get('input[name="name"]').setValue(name)
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.text()).toContain(error)
    })
    expect(updateEsimNickname).not.toHaveBeenCalled()
  })

  it('submits a trimmed nickname at the UTF-8 byte limit', async () => {
    const wrapper = mountSection()
    const nickname = '中'.repeat(21) + 'a'
    await buttonWithText(wrapper, 'actions.rename').trigger('click')
    await flushPromises()

    expect(wrapper.get<HTMLInputElement>('input[name="name"]').element.value).toBe('Active')
    await wrapper.get('input[name="name"]').setValue(' ' + nickname + ' ')
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(updateEsimNickname).toHaveBeenCalledExactlyOnceWith(
        'modem-1',
        'default',
        'iccid-active',
        nickname,
      )
    })
  })

  it('keeps the rename target and dialog open while the request is pending', async () => {
    let completeRequest: (() => void) | undefined
    const request = new Promise<{ data: { value: undefined } }>((resolve) => {
      completeRequest = () => resolve({ data: { value: undefined } })
    })
    updateEsimNickname.mockReturnValueOnce(request)
    const items = profiles.map((profile) => ({ ...profile }))
    const wrapper = mountSection({ profiles: items }, true)
    await buttonWithText(wrapper, 'actions.rename').trigger('click')
    await flushPromises()

    await wrapper.get('input[name="name"]').setValue('New name')
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => {
      expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    })
    expect(wrapper.get('input[name="name"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-slot="dialog-close"]').exists()).toBe(false)
    expect(
      wrapper.get('[role="dialog"] button[type="button"]').attributes('disabled'),
    ).toBeDefined()

    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(wrapper.find('input[name="name"]').exists()).toBe(true)

    wrapper
      .get('[data-slot="dialog-overlay"]')
      .element.dispatchEvent(
        new PointerEvent('pointerdown', { bubbles: true, button: 0, pointerType: 'mouse' }),
      )
    await flushPromises()
    expect(wrapper.find('input[name="name"]').exists()).toBe(true)

    await buttonWithText(wrapper, 'actions.rename', 1).trigger('click')
    expect(wrapper.get<HTMLInputElement>('input[name="name"]').element.value).toBe('New name')

    completeRequest?.()
    await flushPromises()
    expect(wrapper.find('input[name="name"]').exists()).toBe(false)
    expect(items.map((profile) => profile.name)).toEqual(['New name', 'Inactive'])
    expect(updateEsimNickname).toHaveBeenCalledExactlyOnceWith(
      'modem-1',
      'default',
      'iccid-active',
      'New name',
    )

    await buttonWithText(wrapper, 'actions.rename', 1).trigger('click')
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('input[name="name"]').element.value).toBe('Inactive')
    await wrapper.get('[data-slot="dialog-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('input[name="name"]').exists()).toBe(false)
  })

  it('allows retrying a rename after the request fails', async () => {
    const error = new Error('network unavailable')
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    updateEsimNickname.mockRejectedValueOnce(error)
    const wrapper = mountSection({}, true)
    await buttonWithText(wrapper, 'actions.rename').trigger('click')
    await flushPromises()
    await wrapper.get('input[name="name"]').setValue('New name')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(log).toHaveBeenCalledWith('[EsimProfileSection] Failed to update nickname:', error)
    expect(wrapper.get('input[name="name"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-slot="dialog-close"]').exists()).toBe(true)

    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateEsimNickname).toHaveBeenCalledTimes(2)
    expect(wrapper.find('input[name="name"]').exists()).toBe(false)
  })

  it.each(['escape', 'outside', 'close'])(
    'dismisses and resets an idle rename via %s',
    async (action) => {
      const wrapper = mountSection({}, true)
      await buttonWithText(wrapper, 'actions.rename').trigger('click')
      await flushPromises()
      await wrapper.get('input[name="name"]').setValue('')
      await wrapper.get('input[name="name"]').trigger('blur')
      await vi.waitFor(() => {
        expect(wrapper.text()).toContain('modemDetail.validation.required')
      })

      if (action === 'escape') {
        await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
      } else if (action === 'outside') {
        wrapper
          .get('[data-slot="dialog-overlay"]')
          .element.dispatchEvent(
            new PointerEvent('pointerdown', { bubbles: true, button: 0, pointerType: 'mouse' }),
          )
      } else {
        await wrapper.get('[data-slot="dialog-close"]').trigger('click')
      }
      await vi.waitFor(() => {
        expect(wrapper.find('input[name="name"]').exists()).toBe(false)
      })

      await buttonWithText(wrapper, 'actions.rename').trigger('click')
      await flushPromises()
      expect(wrapper.get<HTMLInputElement>('input[name="name"]').element.value).toBe('Active')
      expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    },
  )

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

  it('separates active profile quick action items', () => {
    const wrapper = mountSection()
    const menus = wrapper.findAllComponents({ name: 'DropdownMenu' })

    expect(menus[0].findAll('hr')).toHaveLength(6)
    expect(menus[1].findAll('hr')).toHaveLength(3)
  })

  it('shows Reminder for active and inactive profiles', () => {
    const wrapper = mountSection()

    expect(
      wrapper.findAll('button').filter((button) => button.text().includes('reminder.title')),
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

  it('opens the profile details dialog from the action menu', async () => {
    const wrapper = mountSection()

    const detailsActions = wrapper
      .findAll('button')
      .filter((button) => button.text().includes('actions.viewDetails'))
    expect(detailsActions).toHaveLength(2)

    await detailsActions[0].trigger('click')

    expect(wrapper.find('[data-testid="profile-details"]').text()).toContain('Carrier Active')
    expect(wrapper.find('[data-testid="profile-details"]').text()).toContain('Active Line')
    expect(wrapper.find('[data-testid="profile-details"]').text()).toContain('208')
  })

  it('shows SIM Application only for the enabled matching profile', async () => {
    const wrapper = mountSection({
      simApplicationAvailable: true,
      simApplicationProfileIccid: 'iccid-active',
    })

    const actions = wrapper
      .findAll('button')
      .filter((button) => button.text().includes('simApplication.title'))
    expect(actions).toHaveLength(1)

    await actions[0].trigger('click')

    expect(wrapper.emitted('open-sim-application')?.[0]?.[0]).toMatchObject({
      id: 'active',
    })
  })
})
