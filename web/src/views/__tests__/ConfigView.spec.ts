import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ConfigKeyValueField from '@/components/config/ConfigKeyValueField.vue'
import ConfigView from '@/views/ConfigView.vue'
import type { ConfigResponse, ConfigValues } from '@/types/config'

const api = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
}))

vi.mock('@/apis/config', () => ({
  useConfigApi: () => api,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
    te: (key: string) => !key.includes('missing'),
  }),
}))

vi.mock('vue-sonner', () => ({
  toast: {
    success: vi.fn(),
    warning: vi.fn(),
  },
}))

const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T

const sectionRect = (top: number): DOMRect => ({
  bottom: top + 100,
  height: 100,
  left: 0,
  right: 100,
  top,
  width: 100,
  x: 0,
  y: top,
  toJSON: () => ({}),
})

const stubDesktopViewport = () => {
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => ({
      matches: true,
      media: '(min-width: 768px)',
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(() => true),
    })),
  )
}

const values = (): ConfigValues => ({
  app: {
    environment: 'production',
    listenAddress: '0.0.0.0:9527',
    authProviders: [],
    otpRequired: false,
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

const response = (): ConfigResponse => ({
  path: '/tmp/sigmo/config.toml',
  schema: {
    app: [
      {
        key: 'environment',
        label: 'Environment',
        control: 'select',
        options: [
          { label: 'Production', value: 'production' },
          { label: 'Development', value: 'development' },
        ],
      },
      {
        key: 'listenAddress',
        label: 'Listen address',
        control: 'text',
      },
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
        fields: [
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
        ],
      },
    ],
  },
  values: values(),
})

const stubs = {
  Button: {
    props: ['disabled', 'type'],
    template: '<button :type="type || \'button\'" :disabled="disabled"><slot /></button>',
  },
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
  RouterLink: {
    props: ['to'],
    template: '<a><slot /></a>',
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

const mountView = async (config = response(), attachTo?: HTMLElement) => {
  api.getConfig.mockResolvedValue({ data: { value: clone(config) } })
  api.updateConfig.mockImplementation(async (payload: ConfigValues) => ({
    data: {
      value: {
        ...config,
        values: clone(payload),
      },
    },
  }))

  const wrapper = mount(ConfigView, {
    attachTo,
    global: {
      stubs,
    },
  })
  await flushPromises()
  return wrapper
}

describe('ConfigView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('renders fields from schema and auth providers as checkboxes', async () => {
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('/tmp/sigmo/config.toml')
    const environment = wrapper.find('#config-app-environment[data-slot="select-trigger"]')
    expect(environment.exists()).toBe(true)
    expect(environment.attributes('role')).toBe('combobox')
    expect(wrapper.find('input#config-app-listenAddress').exists()).toBe(true)
    expect(wrapper.find('button#config-app-otpRequired[role="switch"]').exists()).toBe(true)
    expect(wrapper.find('#config-channel-telegram-recipients[role="listbox"]').exists()).toBe(true)
    expect(wrapper.find('#config-channel-email-tlsPolicy[data-slot="select-trigger"]').exists()).toBe(
      false,
    )
    expect(wrapper.find('select').exists()).toBe(false)

    const authProviders = wrapper.findAll('[role="checkbox"]')
    expect(authProviders).toHaveLength(1)
    const authProvider = authProviders[0]
    expect(authProvider?.attributes('aria-checked')).toBe('false')
    expect(wrapper.text()).toContain('Telegram')
  })

  it('renders localized schema text when i18n keys are present', async () => {
    const config = response()
    const environmentField = config.schema.app[0]
    const telegramChannel = config.schema.channels[0]
    if (!environmentField || !telegramChannel) {
      throw new Error('test fixture missing schema entries')
    }
    config.schema.app[0] = {
      ...environmentField,
      label: 'config.schema.app.environment.label',
      description: 'config.schema.app.environment.description',
      options: environmentField.options?.map((option) => ({
        ...option,
        label: `config.schema.app.environment.options.${option.value}`,
      })),
    }
    config.schema.channels[0] = {
      ...telegramChannel,
      label: 'config.schema.channels.telegram.label',
      description: 'config.schema.channels.telegram.description',
      fields: telegramChannel.fields.map((field) =>
        field.key === 'headers'
          ? {
              ...field,
              label: 'config.schema.channels.http.headers.label',
              description: 'config.schema.channels.http.headers.description',
            }
          : field,
      ),
    }

    const wrapper = await mountView(config)

    expect(wrapper.text()).toContain('config.schema.app.environment.label')
    expect(wrapper.text()).toContain('config.schema.app.environment.description')
    expect(wrapper.text()).toContain('config.schema.channels.telegram.label')
    expect(wrapper.text()).toContain('config.schema.channels.telegram.description')
    expect(wrapper.text()).toContain('config.schema.channels.http.headers.label')
    expect(wrapper.text()).toContain('config.schema.channels.http.headers.description')
  })

  it('renders multi-option select fields with shadcn select', async () => {
    const config = response()
    config.values.channels = {
      email: {
        enabled: true,
        tlsPolicy: 'opportunistic',
      },
    }

    const wrapper = await mountView(config)

    const trigger = wrapper.find('#config-channel-email-tlsPolicy[data-slot="select-trigger"]')
    expect(trigger.exists()).toBe(true)
    expect(trigger.attributes('role')).toBe('combobox')
    expect(wrapper.find('select').exists()).toBe(false)
  })

  it('selects the desktop nav item for the visible section while scrolling', async () => {
    stubDesktopViewport()
    const root = document.createElement('div')
    document.body.append(root)
    const wrapper = await mountView(response(), root)

    vi.spyOn(
      wrapper.find('#config-section-app').element,
      'getBoundingClientRect',
    ).mockReturnValue(sectionRect(-360))
    vi.spyOn(
      wrapper.find('#config-section-proxy').element,
      'getBoundingClientRect',
    ).mockReturnValue(sectionRect(80))
    vi.spyOn(
      wrapper.find('#config-section-channels').element,
      'getBoundingClientRect',
    ).mockReturnValue(sectionRect(720))

    window.dispatchEvent(new Event('scroll'))
    await wrapper.vm.$nextTick()

    const navButtons = wrapper.findAll('aside button')
    expect(navButtons[1]?.classes()).toContain('bg-muted')
    expect(navButtons[0]?.classes()).not.toContain('bg-muted')

    wrapper.unmount()
    root.remove()
  })

  it('selects the last desktop nav item at the page bottom', async () => {
    stubDesktopViewport()
    vi.stubGlobal('innerHeight', 600)
    vi.stubGlobal('scrollY', 1400)
    vi.spyOn(document.documentElement, 'scrollHeight', 'get').mockReturnValue(2000)
    vi.spyOn(document.body, 'scrollHeight', 'get').mockReturnValue(2000)

    const root = document.createElement('div')
    document.body.append(root)
    const wrapper = await mountView(response(), root)

    const navButtons = wrapper.findAll('aside button')
    expect(navButtons[2]?.classes()).toContain('bg-muted')
    expect(navButtons[1]?.classes()).not.toContain('bg-muted')

    wrapper.unmount()
    root.remove()
  })

  it('does not register scroll listeners after unmounting during config load', async () => {
    stubDesktopViewport()
    const addEventListener = vi.spyOn(window, 'addEventListener')
    let resolveConfig: (value: { data: { value: ConfigResponse } }) => void = () => {}
    api.getConfig.mockReturnValue(
      new Promise((resolve) => {
        resolveConfig = resolve
      }),
    )

    const wrapper = mount(ConfigView, {
      global: {
        stubs,
      },
    })
    await wrapper.vm.$nextTick()
    expect(api.getConfig).toHaveBeenCalledTimes(1)

    wrapper.unmount()
    resolveConfig({ data: { value: response() } })
    await flushPromises()

    expect(addEventListener).not.toHaveBeenCalledWith(
      'scroll',
      expect.any(Function),
      expect.anything(),
    )
  })

  it('saves selected auth providers as a channel name array', async () => {
    const wrapper = await mountView()

    await wrapper.find('[role="checkbox"]').trigger('click')
    const saveButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('config.save'))
    expect(saveButton).toBeDefined()
    await saveButton?.trigger('click')
    await flushPromises()

    expect(api.updateConfig).toHaveBeenCalledTimes(1)
    const payload = api.updateConfig.mock.calls[0]?.[0] as ConfigValues
    expect(payload.app.authProviders).toEqual(['telegram'])
  })

  it('saves updated tag lists as arrays', async () => {
    const wrapper = await mountView()

    await wrapper.find('#config-channel-telegram-recipients .tags-input-add').trigger('click')
    const saveButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('config.save'))
    expect(saveButton).toBeDefined()
    await saveButton?.trigger('click')
    await flushPromises()

    expect(api.updateConfig).toHaveBeenCalledTimes(1)
    const payload = api.updateConfig.mock.calls[0]?.[0] as ConfigValues
    expect(payload.channels.telegram?.recipients).toEqual(['10001', '10002'])
  })

  it('saves added, renamed, and removed key/value channel fields', async () => {
    const wrapper = await mountView()
    const keyValueField = wrapper.findComponent(ConfigKeyValueField)
    expect(keyValueField.exists()).toBe(true)

    const addHeaderButton = keyValueField
      .findAll('button')
      .find((button) => button.text().includes('config.addHeader'))
    expect(addHeaderButton).toBeDefined()
    await addHeaderButton?.trigger('click')

    let inputs = keyValueField.findAll('input')
    expect(inputs).toHaveLength(4)
    await inputs[2]?.setValue('X-Sigmo')

    inputs = keyValueField.findAll('input')
    await inputs[3]?.setValue('enabled')

    const removeOriginalButton = keyValueField.findAll('button')[1]
    expect(removeOriginalButton).toBeDefined()
    await removeOriginalButton?.trigger('click')

    const saveButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('config.save'))
    expect(saveButton).toBeDefined()
    await saveButton?.trigger('click')
    await flushPromises()

    expect(api.updateConfig).toHaveBeenCalledTimes(1)
    const payload = api.updateConfig.mock.calls[0]?.[0] as ConfigValues
    expect(payload.channels.telegram?.headers).toEqual({
      'X-Sigmo': 'enabled',
    })
  })

  it('keeps channel settings when a channel is disabled', async () => {
    const wrapper = await mountView()

    await wrapper.find('#config-channel-telegram-enabled').trigger('click')
    const saveButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('config.save'))
    expect(saveButton).toBeDefined()
    await saveButton?.trigger('click')
    await flushPromises()

    expect(api.updateConfig).toHaveBeenCalledTimes(1)
    const payload = api.updateConfig.mock.calls[0]?.[0] as ConfigValues
    expect(payload.channels.telegram).toMatchObject({
      enabled: false,
      botToken: 'secret',
      recipients: ['10001'],
      headers: {
        Authorization: 'Bearer token',
      },
    })
  })

  it('does not render unsupported controls as editable inputs', async () => {
    const config = response()
    config.schema.app.push({
      key: 'mystery',
      label: 'Mystery',
      control: 'unsupported' as never,
    })

    const wrapper = await mountView(config)

    expect(wrapper.text()).toContain('Unsupported control: unsupported')
    expect(wrapper.find('input#config-app-mystery').exists()).toBe(false)
  })

  it('shows unsupported auth controls instead of dropping them', async () => {
    const config = response()
    config.schema.app = config.schema.app.map((field) =>
      field.key === 'authProviders' ? { ...field, control: 'unsupported' as never } : field,
    )

    const wrapper = await mountView(config)

    expect(wrapper.text()).toContain('Unsupported control: unsupported')
  })
})
