import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('reka-ui', () => ({
  DialogClose: defineComponent({
    setup(_, { slots }) {
      return () => h('button', { 'data-reka': 'close' }, slots.default?.())
    },
  }),
  DialogContent: defineComponent({
    inheritAttrs: false,
    setup(_, { attrs, slots }) {
      return () => h('section', { 'data-reka': 'content', ...attrs }, slots.default?.())
    },
  }),
  DialogPortal: defineComponent({
    setup(_, { slots }) {
      return () => h('div', { 'data-reka': 'portal' }, slots.default?.())
    },
  }),
  DialogOverlay: defineComponent({
    setup(_, { slots }) {
      return () => h('div', { 'data-reka': 'overlay' }, slots.default?.())
    },
  }),
  useForwardPropsEmits: (props: object) => props,
}))

import DialogContent from '../DialogContent.vue'

describe('DialogContent', () => {
  it('lets explicit aria-describedby attrs win over forwarded empty props', () => {
    const wrapper = mount(DialogContent, {
      attrs: {
        'aria-describedby': 'phone-dialpad-description',
      },
      props: {
        showCloseButton: false,
      },
      slots: {
        default: 'Body',
      },
    })

    const content = wrapper.find('[data-reka="content"]')

    expect(content.attributes('aria-describedby')).toBe('phone-dialpad-description')
  })
})
