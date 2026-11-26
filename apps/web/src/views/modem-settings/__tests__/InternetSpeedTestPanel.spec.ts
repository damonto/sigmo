import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import InternetSpeedTestPanel from '@/views/modem-settings/InternetSpeedTestPanel.vue'

enableAutoUnmount(afterEach)

const mountPanel = () =>
  mount(InternetSpeedTestPanel, {
    attachTo: document.body,
    props: { result: null, canRun: true, running: false, failure: null, readFailed: false },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })],
    },
  })

describe('InternetSpeedTestPanel', () => {
  it('shows and submits the current size, locks it during a test, and resets on reopen', async () => {
    const panel = mountPanel()
    const trigger = panel.get<HTMLButtonElement>('#internet-speed-test-size')
    expect(trigger.text()).toBe('10 MB')
    expect(panel.text()).toContain('Uses about 10 MB of mobile data per test.')

    await trigger.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const options = Array.from(document.querySelectorAll('[role="option"]'))
    expect(options.map((option) => option.textContent?.trim())).toEqual([
      '10 MB',
      '25 MB',
      '50 MB',
      '100 MB',
    ])
    options[2]?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(trigger.text()).toBe('50 MB')
    expect(panel.text()).toContain('Uses about 50 MB of mobile data per test.')
    expect(panel.emitted('test')).toBeUndefined()

    await panel.get('button[data-slot="button"]').trigger('click')
    expect(panel.emitted('test')).toEqual([[50]])
    await panel.setProps({ running: true, canRun: false })
    expect(trigger.element.disabled).toBe(true)

    panel.unmount()
    expect(mountPanel().get('#internet-speed-test-size').text()).toBe('10 MB')
  })
})
