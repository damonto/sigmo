import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import ModemNotificationsHeader from '@/components/modem/notifications/ModemNotificationsHeader.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const stubs = {
  BackButton: {
    props: ['to', 'label'],
    template: '<button type="button">{{ label }}</button>',
  },
  Badge: {
    template: '<span><slot /></span>',
  },
  ModemStickyTopBar: {
    template: '<div><slot name="right" /></div>',
  },
}

describe('ModemNotificationsHeader', () => {
  it('keeps the mobile back control hidden on desktop', () => {
    const wrapper = mount(ModemNotificationsHeader, {
      props: {
        count: 3,
        isLoading: false,
        modemId: 'modem-1',
      },
      global: {
        stubs,
      },
    })

    const mobileBackControl = wrapper
      .findAll('.lg\\:hidden')
      .find((element) => element.text().includes('modemDetail.back'))

    expect(mobileBackControl?.exists()).toBe(true)
  })
})
