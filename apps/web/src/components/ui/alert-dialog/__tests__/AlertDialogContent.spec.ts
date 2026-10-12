import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('reka-ui', async () => ({
  ...(await vi.importActual<typeof import('reka-ui')>('reka-ui')),
  AlertDialogContent: defineComponent({
    props: {
      disableOutsidePointerEvents: {
        type: Boolean,
        // Reka preserves undefined so its modal child can apply the default.
        default: undefined,
      },
    },
    inheritAttrs: false,
    setup(props, { attrs, slots }) {
      return () =>
        h(
          'section',
          {
            'data-reka': 'content',
            'data-disable-outside-pointer-events': String(props.disableOutsidePointerEvents),
            ...attrs,
          },
          slots.default?.(),
        )
    },
  }),
  AlertDialogOverlay: defineComponent({
    setup(_, { slots }) {
      return () => h('div', { 'data-reka': 'overlay' }, slots.default?.())
    },
  }),
  AlertDialogPortal: defineComponent({
    setup(_, { slots }) {
      return () => h('div', { 'data-reka': 'portal' }, slots.default?.())
    },
  }),
}))

import AlertDialogContent from '../AlertDialogContent.vue'

describe('AlertDialogContent', () => {
  it('preserves the Reka default for outside pointer events', () => {
    const wrapper = mount(AlertDialogContent, {
      slots: {
        default: 'Body',
      },
    })

    expect(
      wrapper.find('[data-reka="content"]').attributes('data-disable-outside-pointer-events'),
    ).toBe('undefined')
  })

  it('still forwards an explicit outside pointer events override', () => {
    const wrapper = mount(AlertDialogContent, {
      props: {
        disableOutsidePointerEvents: false,
      },
      slots: {
        default: 'Body',
      },
    })

    expect(
      wrapper.find('[data-reka="content"]').attributes('data-disable-outside-pointer-events'),
    ).toBe('false')
  })
})
