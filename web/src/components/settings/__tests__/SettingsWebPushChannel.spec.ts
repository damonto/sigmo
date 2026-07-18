import type { Ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import SettingsWebPushChannel from '@/components/settings/SettingsWebPushChannel.vue'
import { useWebPush } from '@/composables/useWebPush'
import en from '@/i18n/locales/en'
import type { WebPushSubscriptionResponse } from '@/types/webPush'

vi.mock('@/composables/useWebPush', async () => {
  const { ref } = await import('vue')
  const subscriptions = ref<WebPushSubscriptionResponse[]>([])
  const currentSubscription = ref<WebPushSubscriptionResponse | null>(null)
  const webPush = {
    subscriptions,
    currentSubscription,
    enabled: ref(true),
    supportReason: ref('supported'),
    permission: ref('granted'),
    isLoading: ref(false),
    isUpdating: ref(false),
    errorMessage: ref(''),
    load: vi.fn().mockResolvedValue(undefined),
    setEnabled: vi.fn().mockResolvedValue(undefined),
    enableCurrentDevice: vi.fn().mockResolvedValue(true),
    deleteSubscription: vi.fn().mockResolvedValue(undefined),
    renameSubscription: vi.fn().mockResolvedValue(undefined),
  }

  return {
    useWebPush: () => webPush,
  }
})

vi.mock('vue-sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

type WebPushHarness = ReturnType<typeof useWebPush> & {
  subscriptions: Ref<WebPushSubscriptionResponse[]>
  currentSubscription: Ref<WebPushSubscriptionResponse | null>
}

const webPush = useWebPush() as WebPushHarness

const subscription = (
  id: string,
  platform: string,
  userAgent = '',
): WebPushSubscriptionResponse => ({
  id,
  endpoint: `https://push.example/${id}`,
  label: `Device ${id}`,
  userAgent,
  platform,
  createdAt: '2026-07-17T08:00:00Z',
  updatedAt: '2026-07-18T08:00:00Z',
})

const mountChannel = () =>
  mount(SettingsWebPushChannel, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en },
        }),
      ],
      stubs: {
        Switch: {
          props: ['disabled', 'modelValue'],
          template: '<button type="button" role="switch" :disabled="disabled" />',
        },
      },
    },
  })

describe('SettingsWebPushChannel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    webPush.subscriptions.value = [
      subscription('mac', 'macOS'),
      subscription('ios', 'iOS'),
      subscription('windows', 'Win32'),
      subscription('tablet', '', 'Mozilla/5.0 (iPad)'),
      subscription('unknown', ''),
    ]
    webPush.currentSubscription.value = webPush.subscriptions.value[0] ?? null
  })

  it('renders platform icons and current-device metadata without status timestamps', () => {
    const wrapper = mountChannel()
    const iconClasses = wrapper
      .findAll('[data-testid="web-push-platform-icon"]')
      .map((icon) => icon.classes())
    const metadata = wrapper.findAll('[data-testid="web-push-device-metadata"]')

    expect(iconClasses[0]).toContain('lucide-laptop')
    expect(iconClasses[1]).toContain('lucide-smartphone')
    expect(iconClasses[2]).toContain('lucide-monitor')
    expect(iconClasses[3]).toContain('lucide-tablet')
    expect(iconClasses[4]).toContain('lucide-earth')
    expect(metadata[0]?.text()).toContain('macOS')
    expect(metadata[0]?.text()).toContain('Current device')
    expect(metadata[1]?.text()).toBe('iOS')
    expect(wrapper.text()).not.toContain('Updated at')
    expect(wrapper.text()).not.toContain('Authorized')
  })

  it('renames a device through the inline editor', async () => {
    const wrapper = mountChannel()
    const current = webPush.subscriptions.value[0]

    await wrapper.get('button[aria-label="Rename device"]').trigger('click')
    await wrapper.get('input[aria-label="Device name"]').setValue('MacBook Pro')
    await wrapper.get('button[aria-label="Save device name"]').trigger('click')
    await flushPromises()

    expect(webPush.renameSubscription).toHaveBeenCalledWith(current, 'MacBook Pro')
    expect(wrapper.find('input[aria-label="Device name"]').exists()).toBe(false)
  })
})
