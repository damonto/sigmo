import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { UpdateSnapshot } from '@/types/update'
import SettingsUpdatesView from '@/views/SettingsUpdatesView.vue'

const api = vi.hoisted(() => ({
  settings: vi.fn(),
  saveSettings: vi.fn(),
  check: vi.fn(),
  install: vi.fn(),
  installation: vi.fn(),
}))

vi.mock('@/apis/update', () => ({
  useUpdateApi: () => api,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: { value: 'en-US' },
    t: (key: string) => key,
  }),
}))

vi.mock('vue-sonner', () => ({
  toast: { success: vi.fn() },
}))

const snapshot = (overrides: Partial<UpdateSnapshot> = {}): UpdateSnapshot => ({
  settings: { automatic: false, channel: 'stable' },
  current: {
    version: 'v1.0.0',
    commit: '1111111111111111111111111111111111111111',
    channel: 'stable',
    edition: 'community',
    target: 'linux-amd64',
    distribution: 'standalone',
  },
  latest: {
    schemaVersion: 1,
    edition: 'community',
    channel: 'stable',
    version: 'v1.1.0',
    commit: '2222222222222222222222222222222222222222',
    publishedAt: '2026-08-09T12:00:00Z',
    notes: 'fix update\n<strong>shown as text</strong>',
  },
  state: 'idle',
  checkedAt: '2026-08-09T12:10:00Z',
  updateAvailable: true,
  selfUpdateSupported: true,
  ...overrides,
})

const stubs = {
  Alert: { template: '<section data-testid="alert"><slot /></section>' },
  AlertDescription: { template: '<div><slot /></div>' },
  AlertTitle: { template: '<h2><slot /></h2>' },
  Badge: { template: '<span><slot /></span>' },
  Button: {
    props: ['disabled'],
    template: '<button :disabled="disabled"><slot /></button>',
  },
  Card: { template: '<section><slot /></section>' },
  CardContent: { template: '<div><slot /></div>' },
  CardDescription: { template: '<p><slot /></p>' },
  CardHeader: { template: '<header><slot /></header>' },
  CardTitle: { template: '<h2><slot /></h2>' },
  Download: { template: '<span />' },
  Label: { template: '<label><slot /></label>' },
  RefreshCw: { template: '<span />' },
  Select: {
    props: ['disabled', 'modelValue'],
    template:
      '<div data-testid="channel-select" :data-disabled="String(Boolean(disabled))" :data-value="modelValue"><slot /></div>',
  },
  SelectContent: { template: '<div><slot /></div>' },
  SelectItem: { template: '<span><slot /></span>' },
  SelectTrigger: { template: '<button><slot /></button>' },
  SelectValue: { template: '<span />' },
  SettingsHeader: { props: ['title'], template: '<h1>{{ title }}</h1>' },
  ShieldCheck: { template: '<span />' },
  Spinner: { template: '<span />' },
  Switch: {
    props: ['disabled', 'modelValue'],
    template:
      '<button role="switch" :disabled="disabled" :aria-checked="String(Boolean(modelValue))" />',
  },
}

const render = async (value: UpdateSnapshot) => {
  api.settings.mockResolvedValue({ data: ref(value) })
  const wrapper = mount(SettingsUpdatesView, { global: { stubs } })
  await flushPromises()
  return wrapper
}

describe('SettingsUpdatesView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders Community as Stable-only and keeps release notes as text', async () => {
    const wrapper = await render(snapshot())

    expect(wrapper.get('[data-testid="channel-select"]').attributes('data-disabled')).toBe('true')
    expect(wrapper.text()).toContain('v1.0.0')
    expect(wrapper.text()).toContain('v1.1.0')
    expect(wrapper.text()).toContain('<strong>shown as text</strong>')
    expect(wrapper.find('strong').exists()).toBe(false)
  })

  it('renders Pro channels and authorization identity', async () => {
    const value = snapshot({
      settings: { automatic: true, channel: 'dev' },
      current: {
        version: 'dev-11111111',
        commit: '1111111111111111111111111111111111111111',
        channel: 'dev',
        edition: 'pro',
        target: 'linux-amd64-musl',
        distribution: 'standalone',
      },
      latest: {
        schemaVersion: 1,
        edition: 'pro',
        channel: 'dev',
        version: 'dev-22222222',
        commit: '2222222222222222222222222222222222222222',
        publishedAt: '2026-08-09T12:00:00Z',
        notes: 'fix(network): reconnect',
      },
      license: {
        status: 'active',
        telegramId: 10001,
        displayName: 'Alice Example',
        username: 'alice',
        expiresAt: '2027-08-09T12:00:00Z',
        offlineUntil: '2026-08-12T12:00:00Z',
      },
    })
    const wrapper = await render(value)

    expect(wrapper.get('[data-testid="channel-select"]').attributes('data-disabled')).toBe('false')
    expect(wrapper.text()).toContain('Dev')
    expect(wrapper.text()).toContain('Alice Example')
    expect(wrapper.text()).toContain('@alice')
    expect(wrapper.text()).toContain('Telegram ID: 10001')
    expect(wrapper.text()).toContain('settings.updates.licenseExpires')
    expect(wrapper.text()).toContain('settings.updates.licenseOfflineUntil')
  })

  it('disables self-installation and shows the Docker command for containers', async () => {
    const value = snapshot({
      current: {
        version: 'v1.0.0',
        commit: '1111111111111111111111111111111111111111',
        channel: 'stable',
        edition: 'community',
        target: 'linux-amd64',
        distribution: 'container',
      },
      selfUpdateSupported: false,
      unsupportedReason: 'container',
    })
    const wrapper = await render(value)

    expect(wrapper.text()).toContain('docker compose pull && docker compose up -d')
    expect(wrapper.get('[data-testid="automatic-updates"]').attributes('disabled')).toBeDefined()
    const install = wrapper
      .findAll('button')
      .find((button) => button.text().includes('settings.updates.installNow'))
    expect(install?.attributes('disabled')).toBeDefined()
  })
})
