import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAppUpdate } from '@/composables/useAppUpdate'
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

    target: 'linux-amd64',
    distribution: 'standalone',
  },
  latest: {
    schemaVersion: 1,

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

enableAutoUnmount(afterEach)

describe('SettingsUpdatesView', () => {
  beforeEach(() => {
    useAppUpdate().dispose()
    vi.resetAllMocks()
  })

  afterEach(() => {
    useAppUpdate().dispose()
    vi.useRealTimers()
  })

  it('renders both update channels and keeps release notes as text', async () => {
    const wrapper = await render(snapshot())

    expect(wrapper.get('[data-testid="channel-select"]').attributes('data-disabled')).toBe('false')
    expect(wrapper.text()).toContain('v1.0.0')
    expect(wrapper.text()).toContain('v1.1.0')
    expect(wrapper.text()).toContain('<strong>shown as text</strong>')
    expect(wrapper.find('strong').exists()).toBe(false)
  })

  it('renders the development channel and authorization identity', async () => {
    const value = snapshot({
      settings: { automatic: true, channel: 'dev' },
      current: {
        version: 'dev-11111111',
        commit: '1111111111111111111111111111111111111111',
        channel: 'dev',

        target: 'linux-amd64-musl',
        distribution: 'standalone',
      },
      latest: {
        schemaVersion: 1,

        channel: 'dev',
        version: 'dev-22222222',
        commit: '2222222222222222222222222222222222222222',
        publishedAt: '2026-08-09T12:00:00Z',
        notes: 'fix(network): reconnect',
      },
      license: {
        status: 'active',
        deviceId: '0123456789abcdef0123456789abcdef',
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
    expect(wrapper.text()).toContain('settings.updates.deviceId: 0123456789abcdef0123456789abcdef')
    expect(wrapper.text()).toContain('settings.updates.licenseExpires')
    expect(wrapper.text()).toContain('settings.updates.licenseOfflineUntil')
  })

  it('disables self-installation and shows the Docker command for containers', async () => {
    const value = snapshot({
      current: {
        version: 'v1.0.0',
        commit: '1111111111111111111111111111111111111111',
        channel: 'stable',

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

  it('shows a clear message after a successful check finds no update', async () => {
    const wrapper = await render(
      snapshot({
        latest: {
          schemaVersion: 1,

          channel: 'stable',
          version: 'v1.0.0',
          commit: '1111111111111111111111111111111111111111',
          publishedAt: '2026-08-09T12:00:00Z',
          notes: '',
        },
        updateAvailable: false,
      }),
    )

    expect(wrapper.get('[data-testid="up-to-date-message"]').text()).toContain(
      'settings.updates.upToDate',
    )
  })

  it('keeps monitoring across navigation and reuses progress when the page reopens', async () => {
    vi.useFakeTimers()
    api.install.mockResolvedValue({
      data: ref(snapshot({ state: 'downloading' })),
    })
    api.installation.mockRejectedValue(new TypeError('Failed to fetch'))
    const wrapper = await render(snapshot())
    const install = wrapper
      .findAll('button')
      .find((button) => button.text().includes('settings.updates.installNow'))

    if (!install) throw new Error('install button not found')
    await install.trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    expect(api.installation).toHaveBeenCalledTimes(1)
    expect(useAppUpdate().updating.value).toBe(true)
    expect(wrapper.text()).toContain('settings.updates.reconnecting')
    expect(wrapper.get('[data-testid="channel-select"]').attributes('data-disabled')).toBe('true')
    wrapper.unmount()

    expect(useAppUpdate().updating.value).toBe(true)
    const reopened = await render(snapshot())
    expect(api.settings).toHaveBeenCalledTimes(1)
    expect(reopened.text()).toContain('settings.updates.reconnecting')

    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    expect(api.installation).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(1999)
    expect(api.installation).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(api.installation).toHaveBeenCalledTimes(3)
  })

  it('resumes background requests when installation fails', async () => {
    vi.useFakeTimers()
    api.install.mockResolvedValue({ data: ref(snapshot({ state: 'downloading' })) })
    api.installation.mockResolvedValue(snapshot({ state: 'failed', error: 'download failed' }))
    const wrapper = await render(snapshot())
    const install = wrapper
      .findAll('button')
      .find((button) => button.text().includes('settings.updates.installNow'))
    if (!install) throw new Error('install button not found')

    await install.trigger('click')
    await flushPromises()
    expect(useAppUpdate().updating.value).toBe(true)

    await vi.advanceTimersByTimeAsync(1000)
    expect(useAppUpdate().updating.value).toBe(false)
    expect(wrapper.text()).toContain('download failed')
    expect(install.attributes('disabled')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(10_000)
    expect(api.installation).toHaveBeenCalledTimes(1)
  })

  it('observes an installation already in progress when the page loads', async () => {
    vi.useFakeTimers()
    api.installation.mockResolvedValue(snapshot({ state: 'verifying' }))
    await render(snapshot({ state: 'downloading' }))

    expect(useAppUpdate().updating.value).toBe(true)
    await vi.advanceTimersByTimeAsync(500)
    expect(api.installation).toHaveBeenCalledTimes(1)
  })

  it('does not pause background requests when installation is rejected', async () => {
    api.install.mockRejectedValue(new Error('installation rejected'))
    const wrapper = await render(snapshot())
    const install = wrapper
      .findAll('button')
      .find((button) => button.text().includes('settings.updates.installNow'))
    if (!install) throw new Error('install button not found')

    await install.trigger('click')
    await flushPromises()

    expect(useAppUpdate().updating.value).toBe(false)
    expect(api.installation).not.toHaveBeenCalled()
  })

  it('shows a recovery timeout and allows the user to check the server again', async () => {
    vi.useFakeTimers()
    api.installation.mockRejectedValue(new TypeError('Failed to fetch'))
    const wrapper = await render(snapshot({ state: 'restarting' }))

    await vi.advanceTimersByTimeAsync(180_000)
    expect(useAppUpdate().updating.value).toBe(false)
    expect(wrapper.text()).toContain('settings.updates.recoveryTimedOut')

    api.settings.mockResolvedValue({
      data: ref(snapshot({ state: 'failed', error: 'update failed' })),
    })
    const retry = wrapper
      .findAll('button')
      .find((button) => button.text() === 'settings.updates.retry')
    if (!retry) throw new Error('retry button not found')
    await retry.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('settings.updates.recoveryTimedOut')
    expect(wrapper.text()).toContain('update failed')
  })
})
