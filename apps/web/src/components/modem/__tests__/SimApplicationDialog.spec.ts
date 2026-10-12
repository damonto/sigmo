import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import SimApplicationDialog from '@/components/modem/SimApplicationDialog.vue'
import type { SimApplicationView } from '@/types/simApplication'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const stubs = {
  Button: {
    props: ['type'],
    template: '<button v-bind="$attrs" :type="type || \'button\'"><slot /></button>',
  },
  Dialog: {
    props: ['open'],
    emits: ['update:open'],
    template: '<div v-if="open"><slot /></div>',
  },
  DialogContent: { template: '<section><slot /></section>' },
  DialogDescription: { template: '<p><slot /></p>' },
  DialogFooter: { template: '<footer><slot /></footer>' },
  DialogHeader: { template: '<header><slot /></header>' },
  DialogTitle: { template: '<h2><slot /></h2>' },
  Input: {
    props: ['modelValue', 'type', 'minlength', 'maxlength'],
    emits: ['update:modelValue'],
    template:
      '<input :type="type || \'text\'" :value="modelValue" :minlength="minlength" :maxlength="maxlength" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
  ScrollArea: { template: '<div><slot /></div>' },
}

const mountDialog = (view: SimApplicationView) =>
  mount(SimApplicationDialog, {
    props: {
      open: true,
      view,
      'onUpdate:open': () => {},
    },
    global: {
      stubs,
    },
  })

describe('SimApplicationDialog', () => {
  it('renders menu items and emits the selected item', async () => {
    const wrapper = mountDialog({
      type: 'menu',
      menu: {
        kind: 'root',
        title: 'SIM',
        items: [
          { id: 1, label: 'Balance' },
          { id: 2, label: 'Recharge' },
        ],
      },
    })

    expect(wrapper.text()).toContain('Balance')
    expect(wrapper.text()).not.toContain('01')
    expect(wrapper.text()).not.toContain('02')
    await wrapper.findAll('button')[1].trigger('click')

    expect(wrapper.emitted('select-menu-item')?.[0]?.[0]).toMatchObject({
      id: 2,
      label: 'Recharge',
    })
  })

  it('submits input text', async () => {
    const wrapper = mountDialog({
      type: 'input',
      text: 'PIN',
      defaultText: '',
      minLength: 1,
      maxLength: 8,
      hideInput: true,
      helpAvailable: false,
    })

    await wrapper.find('input').setValue('1234')
    await wrapper.find('form').trigger('submit')

    expect(wrapper.emitted('submit-input')?.[0]?.[0]).toBe('1234')
  })

  it('allows long unspaced popup text to wrap', () => {
    const text = 'A'.repeat(120)
    const wrapper = mountDialog({
      type: 'display_text',
      text,
      highPriority: false,
      userClear: true,
      immediateResponse: false,
    })

    const message = wrapper.findAll('p').find((node) => node.text() === text)

    expect(message?.attributes('class')).toContain('break-words')
    expect(message?.attributes('class')).toContain('[overflow-wrap:anywhere]')
  })

  it('emits confirmation responses', async () => {
    const wrapper = mountDialog({
      type: 'confirm',
      command: 'send_ussd',
      text: '*123#',
    })

    await wrapper.findAll('button')[1].trigger('click')

    expect(wrapper.emitted('respond-confirm')?.[0]?.[0]).toBe(true)
  })
})
