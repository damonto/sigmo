import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('@/components/ui/dialog', () => ({
  DialogContent: defineComponent({
    name: 'DialogContent',
    emits: ['openAutoFocus'],
    setup(_, { attrs, emit, slots }) {
      return () =>
        h(
          'section',
          {
            'data-testid': 'dialog-content',
            ...attrs,
            onClick: () => emit('openAutoFocus', new Event('openAutoFocus')),
          },
          slots.default?.(),
        )
    },
  }),
}))

import EsimPersistentDialogContent from '@/components/esim/EsimPersistentDialogContent.vue'

describe('EsimPersistentDialogContent', () => {
  it('forwards open auto focus events', async () => {
    const onOpenAutoFocus = vi.fn()
    const wrapper = mount(EsimPersistentDialogContent, {
      attrs: {
        onOpenAutoFocus,
      },
      slots: {
        default: 'Body',
      },
    })

    await wrapper.find('[data-testid="dialog-content"]').trigger('click')

    expect(onOpenAutoFocus).toHaveBeenCalledOnce()
  })
})
