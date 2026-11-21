import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import WiFiCallingProxyPanel from '@/views/modem-settings/WiFiCallingProxyPanel.vue'

const api = vi.hoisted(() => ({
  listWiFiProxies: vi.fn(),
  checkWiFiProxy: vi.fn(),
  saveWiFiProxy: vi.fn(),
}))

vi.mock('@/apis/modem', () => ({ useModemApi: () => api }))

const mountPanel = (locale: 'en' | 'zh' = 'en') =>
  mount(WiFiCallingProxyPanel, {
    props: { modemId: 'modem-1', usingProxy: false, disabled: false },
    global: {
      plugins: [createI18n({ legacy: false, locale, messages: { en, zh } })],
      stubs: {
        Select: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template:
            '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><slot /></select>',
        },
        SelectTrigger: true,
        SelectValue: true,
        SelectContent: { template: '<slot />' },
        SelectItem: { props: ['value'], template: '<option :value="value"><slot /></option>' },
      },
    },
  })

describe('WiFiCallingProxyPanel', () => {
  it('tests while off, switches proxies explicitly, and turns off even with a draft selection', async () => {
    api.listWiFiProxies.mockResolvedValue({
      data: {
        value: [
          { id: 'p-1', name: 'Office', host: 'proxy.test', port: 1080, hasPassword: true },
          { id: 'p-2', name: 'Travel', host: 'travel.test', port: 1080, hasPassword: false },
        ],
      },
    })
    api.checkWiFiProxy.mockResolvedValue({
      data: {
        value: {
          tcp: { ip: '203.0.113.12', serverAddress: '192.0.2.4:1080' },
          udp: { relayAddress: '192.0.2.4:40843', errorCode: 'udp_no_response' },
          dns: { addresses: ['192.0.2.75'] },
        },
      },
    })
    const wrapper = mountPanel()
    await flushPromises()
    const toggle = wrapper.get('#wifi-proxy-enabled')
    expect(toggle.attributes('disabled')).toBeDefined()
    await wrapper.get('select').setValue('p-1')
    const button = (label: string) => wrapper.findAll('button').find((btn) => btn.text() === label)!

    await button('Test Connection').trigger('click')
    await flushPromises()
    expect(api.checkWiFiProxy).toHaveBeenCalledWith('modem-1', 'p-1', expect.any(AbortSignal))
    expect(wrapper.text()).toContain('203.0.113.12')
    expect(wrapper.text()).toContain('192.0.2.4:40843')
    expect(wrapper.text()).toContain('No UDP reply received.')
    expect(wrapper.emitted('update-proxy')).toBeUndefined()
    expect(toggle.attributes('aria-checked')).toBe('false')

    await toggle.trigger('click')
    expect(wrapper.emitted('update-proxy')).toEqual([['p-1']])
    await wrapper.setProps({ savedProxyId: 'p-1', usingProxy: true })
    await wrapper.get('select').setValue('p-2')
    expect(wrapper.emitted('update-proxy')).toHaveLength(1)
    await button('Switch Proxy').trigger('click')
    expect(wrapper.emitted('update-proxy')).toEqual([['p-1'], ['p-2']])

    await toggle.trigger('click')
    expect(wrapper.emitted('update-proxy')).toEqual([['p-1'], ['p-2'], [null]])
    await wrapper.setProps({ usingProxy: false })
    expect(toggle.attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })

  it('shows localized placeholders and saves a blank port as 1080 without enabling the proxy', async () => {
    api.listWiFiProxies.mockResolvedValue({ data: { value: [] } })
    api.saveWiFiProxy.mockResolvedValue({
      data: {
        value: {
          id: 'new-proxy',
          name: '办公室',
          host: 'proxy.test',
          port: 1080,
          username: 'user',
          hasPassword: true,
        },
      },
    })
    const wrapper = mountPanel('zh')
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === '添加代理')
      ?.trigger('click')

    const name = wrapper.get('input#wifi-proxy-name')
    const host = wrapper.get('input#wifi-proxy-host')
    const port = wrapper.get('input#wifi-proxy-port')
    expect(name.attributes('placeholder')).toBe('办公室代理')
    expect(host.attributes('placeholder')).toBe('proxy.example.com')
    expect(port.attributes('placeholder')).toBe('1080')
    await name.setValue('办公室')
    await host.setValue('proxy.test')
    await wrapper.get('#wifi-proxy-auth').trigger('click')
    expect(wrapper.get('input#wifi-proxy-username').attributes('placeholder')).toBe(
      '输入代理用户名',
    )
    expect(wrapper.get('input#wifi-proxy-password').attributes('placeholder')).toBe('输入代理密码')
    await wrapper.get('input#wifi-proxy-username').setValue('user')
    await wrapper.get('input#wifi-proxy-password').setValue('secret')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(api.saveWiFiProxy).toHaveBeenCalledWith('', expect.objectContaining({ port: 1080 }))
    expect(wrapper.get('#wifi-proxy-enabled').attributes('aria-checked')).toBe('false')
    expect(wrapper.emitted('update-proxy')).toBeUndefined()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === '编辑代理')
      ?.trigger('click')
    expect(wrapper.get('input#wifi-proxy-password').attributes('placeholder')).toBe(
      '留空保留已保存的密码',
    )
    wrapper.unmount()
  })
})
