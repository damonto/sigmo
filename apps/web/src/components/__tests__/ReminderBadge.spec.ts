import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ReminderBadge from '@/components/ReminderBadge.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

afterEach(() => {
  document.body.innerHTML = ''
})

const mountBadge = () =>
  mount(ReminderBadge, {
    attachTo: document.body,
    props: {
      profileName: 'Travel',
      reminder: {
        nextAt: new Date(2026, 6, 18, 10, 30, 0, 0).toISOString(),
        repeatDays: 7,
        content: 'Renew the plan',
      },
    },
  })

describe('ReminderBadge', () => {
  it('opens details on desktop hover', async () => {
    const wrapper = mountBadge()

    await wrapper.get('button').trigger('pointerenter', { pointerType: 'mouse' })

    expect(document.body.textContent).toContain('Renew the plan')
  })

  it('opens details on click for touch devices', async () => {
    const wrapper = mountBadge()

    await wrapper.get('button').trigger('click')

    expect(document.body.textContent).toContain('Renew the plan')
  })

  it('opens details on keyboard focus', async () => {
    const wrapper = mountBadge()
    const button = wrapper.get('button')
    vi.spyOn(button.element, 'matches').mockReturnValue(true)

    await button.trigger('focus')

    expect(document.body.textContent).toContain('Renew the plan')
  })
})
