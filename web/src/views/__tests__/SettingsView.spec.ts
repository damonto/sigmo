import { defineComponent, nextTick, ref, type Ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import SettingsKeyValueField from '@/components/settings/SettingsKeyValueField.vue'
import SettingsLayout from '@/layouts/SettingsLayout.vue'
import type { SettingsResponse, SettingsValues } from '@/types/settings'
import SettingsAuthView from '@/views/SettingsAuthView.vue'
import SettingsNotificationsView from '@/views/SettingsNotificationsView.vue'
import SettingsProxyView from '@/views/SettingsProxyView.vue'
import SettingsView from '@/views/SettingsView.vue'
import SettingsWebPushView from '@/views/SettingsWebPushView.vue'

const api = vi.hoisted(() => ({
  getSettings: vi.fn(),
  testAuth: vi.fn(),
  updateAuth: vi.fn(),
  updateProxy: vi.fn(),
  updateNotificationChannel: vi.fn(),
}))

const toast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
}))

vi.mock('@/apis/settings', () => ({
  useSettingsApi: () => api,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
    te: (key: string) => !key.includes('missing'),
  }),
}))

vi.mock('vue-sonner', () => ({
  toast,
}))

const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T
type SettingsApiResult = { data: Ref<SettingsResponse | undefined> }

const values = (): SettingsValues => ({
  auth: {
    authProviders: [],
    otpRequired: false,
    tokenValidityDays: 30,
  },
  proxy: {
    listenAddress: '127.0.0.1',
    httpPort: 8080,
    socks5Port: 1080,
    password: '',
  },
  channels: {
    telegram: {
      enabled: true,
      botToken: 'secret',
      recipients: ['10001'],
      headers: {
        Authorization: 'Bearer token',
      },
    },
  },
})

const response = (): SettingsResponse => ({
  schema: {
    auth: [
      {
        key: 'otpRequired',
        label: 'OTP required',
        control: 'switch',
      },
      {
        key: 'authProviders',
        label: 'Auth providers',
        control: 'channelList',
      },
      {
        key: 'tokenValidityDays',
        label: 'Token validity',
        control: 'number',
        min: 1,
        max: 180,
      },
    ],
    proxy: [
      {
        key: 'httpPort',
        label: 'HTTP port',
        control: 'number',
      },
    ],
    channels: [
      {
        key: 'telegram',
        label: 'Telegram',
        description: 'Telegram notifications',
        fields: [
          {
            key: 'botToken',
            label: 'Bot token',
            control: 'password',
          },
          {
            key: 'recipients',
            label: 'Recipients',
            control: 'list',
          },
          {
            key: 'headers',
            label: 'Headers',
            control: 'keyValue',
          },
        ],
      },
      {
        key: 'email',
        label: 'Email',
        description: 'Email notifications',
        fields: [
          {
            key: 'smtpHost',
            label: 'SMTP host',
            control: 'text',
          },
          {
            key: 'smtpPort',
            label: 'SMTP port',
            control: 'number',
          },
          {
            key: 'smtpUsername',
            label: 'SMTP username',
            control: 'text',
          },
          {
            key: 'smtpPassword',
            label: 'SMTP password',
            control: 'password',
          },
          {
            key: 'from',
            label: 'From',
            control: 'text',
          },
          {
            key: 'recipients',
            label: 'Recipients',
            control: 'list',
          },
          {
            key: 'tlsPolicy',
            label: 'TLS policy',
            control: 'select',
            options: [
              { label: 'Required', value: 'required' },
              { label: 'Opportunistic', value: 'opportunistic' },
              { label: 'None', value: 'none' },
            ],
          },
          {
            key: 'ssl',
            label: 'SSL',
            control: 'switch',
          },
        ],
      },
    ],
  },
  values: values(),
})

const stubs = {
  Checkbox: {
    props: ['disabled', 'id', 'modelValue'],
    emits: ['update:model-value'],
    template:
      '<button :id="id" type="button" role="checkbox" :aria-checked="modelValue" :disabled="disabled" @click="$emit(\'update:model-value\', !modelValue)" />',
  },
  Input: {
    props: ['disabled', 'id', 'modelValue', 'type'],
    emits: ['update:model-value'],
    template:
      '<input :id="id" :type="type || \'text\'" :value="modelValue" :disabled="disabled" @input="$emit(\'update:model-value\', $event.target.value)" />',
  },
  Label: {
    props: ['for'],
    template: '<label :for="$props.for"><slot /></label>',
  },
  Spinner: {
    template: '<span />',
  },
  Switch: {
    props: ['disabled', 'id', 'modelValue'],
    emits: ['update:model-value'],
    template:
      '<button :id="id" type="button" role="switch" :aria-checked="modelValue" :disabled="disabled" @click="$emit(\'update:model-value\', !modelValue)" />',
  },
  TagsInput: {
    props: ['delimiter', 'disabled', 'id', 'modelValue'],
    emits: ['update:model-value'],
    template:
      '<div :id="id" role="listbox"><button type="button" class="tags-input-add" @click="$emit(\'update:model-value\', [...(modelValue || []), \'10002\'])" /><slot /></div>',
  },
  TagsInputInput: {
    props: ['placeholder'],
    template: '<input :placeholder="placeholder" />',
  },
  TagsInputItem: {
    props: ['value'],
    template: '<span role="option"><slot />{{ value }}</span>',
  },
  TagsInputItemDelete: {
    template: '<button type="button" />',
  },
  TagsInputItemText: {
    template: '<span />',
  },
}

const Root = defineComponent({
  components: { RouterView },
  template: '<RouterView />',
})

const createSettingsRouter = () =>
  createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/',
        name: 'home',
        component: { template: '<div>Home</div>' },
      },
      {
        path: '/settings',
        component: SettingsLayout,
        children: [
          { path: '', name: 'settings', component: SettingsView },
          { path: 'auth', name: 'settings-auth', component: SettingsAuthView },
          { path: 'proxy', name: 'settings-proxy', component: SettingsProxyView },
          {
            path: 'web-push',
            name: 'settings-web-push',
            component: SettingsWebPushView,
          },
          {
            path: 'notifications',
            name: 'settings-notifications',
            component: SettingsNotificationsView,
          },
        ],
      },
    ],
  })

const mountSettings = async (path: string, settings = response()) => {
  api.getSettings.mockResolvedValue({ data: ref<SettingsResponse>(clone(settings)) })
  api.testAuth.mockResolvedValue({ data: ref<void>() })
  api.updateAuth.mockImplementation(async (payload: SettingsValues['auth']) => {
    const updated = clone(settings)
    updated.values.auth = clone(payload)
    return { data: ref<SettingsResponse>(updated) }
  })
  api.updateProxy.mockImplementation(async (payload: SettingsValues['proxy']) => {
    const updated = clone(settings)
    updated.values.proxy = clone(payload)
    return { data: ref<SettingsResponse>(updated) }
  })
  api.updateNotificationChannel.mockImplementation(
    async (channel: string, payload: SettingsValues['channels'][string]) => {
      const updated = clone(settings)
      updated.values.channels[channel] = clone(payload)
      if (payload.enabled !== true) {
        updated.values.auth.authProviders = updated.values.auth.authProviders.filter(
          (provider) => provider !== channel,
        )
      }
      return { data: ref<SettingsResponse>(updated) }
    },
  )

  const router = createSettingsRouter()
  await router.push(path)
  await router.isReady()

  const wrapper = mount(Root, {
    global: {
      plugins: [router],
      stubs,
    },
  })
  await flushPromises()
  return { wrapper, router }
}

const save = async (wrapper: ReturnType<typeof mount>) => {
  const saveButton = wrapper
    .findAll('button')
    .find((button) => button.text().includes('settings.save'))
  expect(saveButton).toBeDefined()
  await saveButton?.trigger('click')
  await flushPromises()
}

const navigate = async (router: Router, path: string) => {
  await router.push(path)
  await flushPromises()
}

describe('Settings routes', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('renders four category cards with direct module routes', async () => {
    const { wrapper } = await mountSettings('/settings')
    const hrefs = wrapper
      .findAll('main a')
      .map((link) => link.attributes('href'))
      .filter((href): href is string => href?.startsWith('/settings/') === true)

    expect(hrefs).toEqual([
      '/settings/auth',
      '/settings/proxy',
      '/settings/web-push',
      '/settings/notifications',
    ])
    expect(wrapper.text()).toContain('settings.authTitle')
    expect(wrapper.text()).toContain('settings.notificationTitle')
  })

  it('renders one top-level Back link and marks the active desktop module', async () => {
    const { wrapper } = await mountSettings('/settings/notifications')

    expect(wrapper.get('aside a[aria-current="page"]').attributes('href')).toBe(
      '/settings/notifications',
    )
    expect(wrapper.get('a[href="/settings"]').text()).toContain('settings.back')
    expect(wrapper.find('main a[href="/settings"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="settings-nav-description-auth"]').classes()).toContain(
      'whitespace-normal',
    )
  })

  it('keeps unsaved edits while navigating and saves each module independently', async () => {
    const { wrapper, router } = await mountSettings('/settings/auth')

    await wrapper.get('#settings-auth-otpRequired').trigger('click')
    await navigate(router, '/settings/proxy')
    await wrapper.get('#settings-proxy-httpPort').setValue('9090')
    await save(wrapper)

    expect(api.updateProxy).toHaveBeenCalledWith(expect.objectContaining({ httpPort: 9090 }))
    expect(api.updateAuth).not.toHaveBeenCalled()

    await navigate(router, '/settings/auth')
    expect(wrapper.get('#settings-auth-otpRequired').attributes('aria-checked')).toBe('true')
    await save(wrapper)

    expect(api.updateAuth).toHaveBeenCalledWith(expect.objectContaining({ otpRequired: true }))
  })

  it('does not include unsaved notification drafts in the auth save', async () => {
    const { wrapper, router } = await mountSettings('/settings/notifications')

    await wrapper.get('button[aria-controls="settings-channel-email-details"]').trigger('click')
    await wrapper.get('#settings-channel-email-smtpHost').setValue('smtp.draft.example')
    await navigate(router, '/settings/auth')
    await save(wrapper)

    expect(api.updateAuth).toHaveBeenCalledTimes(1)
    expect(api.updateNotificationChannel).not.toHaveBeenCalled()

    await navigate(router, '/settings/notifications')
    expect(
      (wrapper.get('#settings-channel-email-smtpHost').element as HTMLInputElement).value,
    ).toBe('smtp.draft.example')
  })

  it('renders schema fields and saves selected auth providers', async () => {
    const { wrapper } = await mountSettings('/settings/auth')

    expect(wrapper.get('#settings-auth-otpRequired').attributes('role')).toBe('switch')
    const tokenValidity = wrapper.get('#settings-auth-tokenValidityDays')
    expect((tokenValidity.element as HTMLInputElement).value).toBe('30')
    expect(tokenValidity.attributes('min')).toBe('1')
    expect(tokenValidity.attributes('max')).toBe('180')
    await tokenValidity.setValue('90')
    const authProvider = wrapper.get('[role="checkbox"]')
    expect(authProvider.attributes('aria-checked')).toBe('false')
    await authProvider.trigger('click')
    await save(wrapper)

    expect(api.updateAuth.mock.calls[0]?.[0].authProviders).toEqual(['telegram'])
    expect(api.updateAuth.mock.calls[0]?.[0].tokenValidityDays).toBe(90)
  })

  it('renders the responsive auth test action above save', async () => {
    const { wrapper, router } = await mountSettings('/settings/auth')

    expect(wrapper.find('main header button').exists()).toBe(false)
    const authActions = wrapper.get('.fixed.inset-x-0.bottom-0')
    expect(authActions.classes()).toContain('lg:static')
    expect(authActions.findAll('button').map((button) => button.text())).toEqual([
      'settings.test',
      'settings.save',
    ])

    await navigate(router, '/settings/proxy')

    expect(wrapper.find('main header button').exists()).toBe(false)
    expect(wrapper.find('main [data-slot="card"] + button').text()).toContain('settings.save')
    expect(wrapper.find('.fixed.inset-x-0.bottom-0 button').text()).toContain('settings.save')
  })

  it('tests selected authentication providers without saving', async () => {
    const { wrapper } = await mountSettings('/settings/auth')

    const testButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('settings.test'))
    expect(testButton).toBeDefined()
    expect(testButton?.attributes('disabled')).toBeDefined()

    await wrapper.get('[role="checkbox"]').trigger('click')
    expect(testButton?.attributes('disabled')).toBeUndefined()
    await testButton?.trigger('click')
    await flushPromises()

    expect(api.testAuth).toHaveBeenCalledWith({ authProviders: ['telegram'] })
    expect(api.updateAuth).not.toHaveBeenCalled()
    expect(toast.success).toHaveBeenCalledWith('settings.authTestSuccess')
  })

  it('blocks repeated actions while testing authentication providers', async () => {
    let resolveTest: (() => void) | undefined
    api.testAuth.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveTest = () => resolve({ data: ref<void>() })
        }),
    )
    const { wrapper } = await mountSettings('/settings/auth')
    await wrapper.get('[role="checkbox"]').trigger('click')

    const buttons = () => wrapper.findAll('button')
    const testButton = buttons().find((button) => button.text().includes('settings.test'))
    const saveButton = buttons().find((button) => button.text().includes('settings.save'))
    await testButton?.trigger('click')
    await nextTick()

    expect(testButton?.attributes('disabled')).toBeDefined()
    expect(saveButton?.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="checkbox"]').attributes('disabled')).toBeDefined()
    await testButton?.trigger('click')
    expect(api.testAuth).toHaveBeenCalledTimes(1)

    resolveTest?.()
    await flushPromises()
    expect(testButton?.attributes('disabled')).toBeUndefined()
  })

  it('renders localized schema text and unsupported controls explicitly', async () => {
    const settings = response()
    settings.schema.auth[0] = {
      ...settings.schema.auth[0]!,
      label: 'settings.schema.auth.otpRequired.label',
      description: 'settings.schema.auth.otpRequired.description',
    }
    settings.schema.auth.push({
      key: 'mystery',
      label: 'Mystery',
      control: 'unsupported' as never,
    })

    const { wrapper } = await mountSettings('/settings/auth', settings)

    expect(wrapper.text()).toContain('settings.schema.auth.otpRequired.label')
    expect(wrapper.text()).toContain('settings.schema.auth.otpRequired.description')
    expect(wrapper.text()).toContain('Unsupported control: unsupported')
    expect(wrapper.find('input#settings-auth-mystery').exists()).toBe(false)
  })
})

describe('Settings notifications', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders each channel as a card and expands the first enabled channel', async () => {
    const { wrapper } = await mountSettings('/settings/notifications')

    expect(wrapper.findAll('main [data-slot="card"]')).toHaveLength(2)
    expect(wrapper.find('#settings-channel-telegram-recipients[role="listbox"]').exists()).toBe(
      true,
    )
    expect(wrapper.find('#settings-channel-email-smtpHost').exists()).toBe(false)
    expect(wrapper.find('#settings-channel-telegram-save').exists()).toBe(true)
    expect(wrapper.find('.fixed.inset-x-0.bottom-0').exists()).toBe(false)
  })

  it('expands a disabled channel without enabling it', async () => {
    const { wrapper } = await mountSettings('/settings/notifications')

    expect(wrapper.get('#settings-channel-email-enabled').attributes('aria-checked')).toBe('false')
    await wrapper.get('button[aria-controls="settings-channel-email-details"]').trigger('click')

    expect(wrapper.find('#settings-channel-email-smtpHost').exists()).toBe(true)
    expect(wrapper.get('#settings-channel-email-enabled').attributes('aria-checked')).toBe('false')
  })

  it('saves one disabled channel, preserves its fields, and removes it from auth providers', async () => {
    const settings = response()
    settings.values.auth.authProviders = ['telegram']
    const { wrapper } = await mountSettings('/settings/notifications', settings)

    await wrapper.get('#settings-channel-telegram-enabled').trigger('click')
    expect(wrapper.find('#settings-channel-telegram-recipients').exists()).toBe(true)
    await wrapper.get('#settings-channel-telegram-save').trigger('click')
    await flushPromises()

    expect(api.updateAuth).not.toHaveBeenCalled()
    expect(api.updateProxy).not.toHaveBeenCalled()
    expect(api.updateNotificationChannel).toHaveBeenCalledTimes(1)
    expect(api.updateNotificationChannel.mock.calls[0]?.[0]).toBe('telegram')
    expect(api.updateNotificationChannel.mock.calls[0]?.[1]).toMatchObject({
      enabled: false,
      botToken: 'secret',
      recipients: ['10001'],
      headers: { Authorization: 'Bearer token' },
    })
  })

  it('saves updated tag lists and key/value fields', async () => {
    const { wrapper } = await mountSettings('/settings/notifications')

    await wrapper.get('#settings-channel-telegram-recipients .tags-input-add').trigger('click')

    const keyValueField = wrapper.findComponent(SettingsKeyValueField)
    const addButton = keyValueField
      .findAll('button')
      .find((button) => button.text().includes('settings.addHeader'))
    await addButton?.trigger('click')

    let inputs = keyValueField.findAll('input')
    await inputs[2]?.setValue('X-Sigmo')
    inputs = keyValueField.findAll('input')
    await inputs[3]?.setValue('enabled')
    await keyValueField.findAll('button')[1]?.trigger('click')
    await wrapper.get('#settings-channel-telegram-save').trigger('click')
    await flushPromises()

    const payload = api.updateNotificationChannel.mock
      .calls[0]?.[1] as SettingsValues['channels'][string]
    expect(payload.recipients).toEqual(['10001', '10002'])
    expect(payload.headers).toEqual({ 'X-Sigmo': 'enabled' })
  })

  it('uses two columns and expands an isolated field to the full row', async () => {
    const settings = response()
    settings.schema.channels.push({
      key: 'lark',
      label: 'Lark',
      fields: [{ key: 'endpoint', label: 'Webhook', control: 'text' }],
    })
    const { wrapper } = await mountSettings('/settings/notifications', settings)

    await wrapper.get('button[aria-controls="settings-channel-email-details"]').trigger('click')

    expect(wrapper.get('[data-channel="email"] [data-field="smtpHost"]').classes()).not.toContain(
      'sm:col-span-2',
    )
    expect(wrapper.get('[data-channel="email"] [data-field="from"]').classes()).toContain(
      'sm:col-span-2',
    )

    await wrapper.get('button[aria-controls="settings-channel-lark-details"]').trigger('click')
    expect(wrapper.get('[data-channel="lark"] [data-field="endpoint"]').classes()).toContain(
      'sm:col-span-2',
    )
  })

  it('keeps another channel draft when one card is saved', async () => {
    const { wrapper } = await mountSettings('/settings/notifications')

    await wrapper.get('button[aria-controls="settings-channel-email-details"]').trigger('click')
    await wrapper.get('#settings-channel-email-smtpHost').setValue('smtp.draft.example')
    await wrapper.get('#settings-channel-telegram-save').trigger('click')
    await flushPromises()

    expect(
      (wrapper.get('#settings-channel-email-smtpHost').element as HTMLInputElement).value,
    ).toBe('smtp.draft.example')
    expect(api.updateNotificationChannel.mock.calls[0]?.[0]).toBe('telegram')
  })

  it('serializes settings saves while a notification request is pending', async () => {
    const { wrapper, router } = await mountSettings('/settings/notifications')
    let resolveNotification: ((value: SettingsApiResult) => void) | undefined
    api.updateNotificationChannel.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveNotification = resolve
        }),
    )

    await wrapper.get('#settings-channel-telegram-save').trigger('click')
    await navigate(router, '/settings/auth')

    const authSave = wrapper
      .findAll<HTMLButtonElement>('main button')
      .find((button) => button.text().includes('settings.save'))
    expect(authSave).toBeDefined()
    expect(authSave?.element.disabled).toBe(true)
    await authSave?.trigger('click')
    expect(api.updateAuth).not.toHaveBeenCalled()

    resolveNotification?.({ data: ref<SettingsResponse>(response()) })
    await flushPromises()
    expect(authSave?.element.disabled).toBe(false)
  })

  it('renders multi-option channel fields with shadcn select', async () => {
    const settings = response()
    settings.values.channels = {
      email: {
        enabled: true,
        tlsPolicy: 'opportunistic',
      },
    }

    const { wrapper } = await mountSettings('/settings/notifications', settings)
    const trigger = wrapper.get('#settings-channel-email-tlsPolicy[data-slot="select-trigger"]')

    expect(trigger.attributes('role')).toBe('combobox')
    expect(wrapper.find('select').exists()).toBe(false)
  })
})
