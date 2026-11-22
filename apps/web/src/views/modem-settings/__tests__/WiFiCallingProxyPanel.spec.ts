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

const pasteInto = (input: HTMLInputElement, text: string) => {
  const event = new Event('paste', { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'clipboardData', { value: { getData: () => text } })
  input.dispatchEvent(event)
  return event
}

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

  it('fills the editor from a copied SOCKS5 URL and only saves it on submit', async () => {
    api.listWiFiProxies.mockResolvedValue({ data: { value: [] } })
    api.saveWiFiProxy.mockClear().mockResolvedValue({
      data: { value: { id: 'new-proxy', name: '2001:db8::1', hasPassword: true } },
    })
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Add Proxy')
      ?.trigger('click')
    const field = (name: string) =>
      wrapper.get<HTMLInputElement>(`input#wifi-proxy-${name}`).element

    const event = pasteInto(field('host'), ' SOCKS5H://u%40ser:p:%2F%E6%B5%8B@[2001:db8::1]:1081\n')
    await wrapper.vm.$nextTick()

    expect(event.defaultPrevented).toBe(true)
    expect(field('name').value).toBe('2001:db8::1')
    expect(field('host').value).toBe('2001:db8::1')
    expect(field('port').value).toBe('1081')
    expect(wrapper.get('#wifi-proxy-auth').attributes('aria-checked')).toBe('true')
    expect(field('username').value).toBe('u@ser')
    expect(field('password').value).toBe('p:/测')
    expect(field('password').type).toBe('password')
    expect(api.saveWiFiProxy).not.toHaveBeenCalled()
    expect(wrapper.emitted('update-proxy')).toBeUndefined()

    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.saveWiFiProxy).toHaveBeenCalledWith('', {
      name: '2001:db8::1',
      host: '2001:db8::1',
      port: 1081,
      username: 'u@ser',
      password: 'p:/测',
    })
    expect(wrapper.emitted('update-proxy')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps the draft on invalid paste and preserves normal hostname paste', async () => {
    api.listWiFiProxies.mockResolvedValue({ data: { value: [] } })
    const wrapper = mountPanel('zh')
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === '添加代理')
      ?.trigger('click')
    const name = wrapper.get<HTMLInputElement>('input#wifi-proxy-name')
    const host = wrapper.get<HTMLInputElement>('input#wifi-proxy-host')
    const port = wrapper.get<HTMLInputElement>('input#wifi-proxy-port')
    await name.setValue('自定义代理')
    pasteInto(host.element, 'socks5://user:pass@proxy.test:1234')
    await wrapper.vm.$nextTick()

    for (const input of [
      'http://other:secret@proxy.test:1080',
      'socks5://user@proxy.test',
      'socks5://user:%GG@proxy.test',
      'socks5://[2001:::1]:1080',
      'socks5://proxy.test/path',
      'socks5://proxy.test:',
      'socks5://proxy.test:999999999999999999999',
      'socks5:/user:secret@proxy.test',
      'proxy.test:1080:8080',
      'proxy.test:65536',
      'socks5://user:line%0Aend@proxy.test',
      `socks5://user:${encodeURIComponent('密'.repeat(86))}@proxy.test`,
    ]) {
      expect(pasteInto(host.element, input).defaultPrevented).toBe(true)
      await wrapper.vm.$nextTick()
      expect(wrapper.get('#wifi-proxy-paste-error').text()).toContain('请粘贴有效的 SOCKS5 链接')
      expect(host.element.value).toBe('proxy.test')
      expect(port.element.value).toBe('1234')
      expect(wrapper.get<HTMLInputElement>('input#wifi-proxy-password').element.value).toBe('pass')
    }

    pasteInto(host.element, '[2001:db8::2]:9000')
    await wrapper.vm.$nextTick()
    expect(host.element.value).toBe('2001:db8::2')
    expect(port.element.value).toBe('9000')
    expect(wrapper.get('#wifi-proxy-auth').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('input#wifi-proxy-password').exists()).toBe(false)

    pasteInto(host.element, 'socks5://192.0.2.1')
    await wrapper.vm.$nextTick()
    expect(host.element.value).toBe('192.0.2.1')
    expect(port.element.value).toBe('1080')
    expect(name.element.value).toBe('自定义代理')
    expect(wrapper.find('#wifi-proxy-paste-error').exists()).toBe(false)
    for (const plainHost of ['plain.test', '2001:db8::2', 'fe80::1%eth0']) {
      expect(pasteInto(host.element, plainHost).defaultPrevented).toBe(false)
    }
    wrapper.unmount()
  })
})
