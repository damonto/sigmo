import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import MCPCreateKeyDialog from '@/components/settings/mcp/MCPCreateKeyDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const SlotStub = defineComponent({ template: '<div><slot /></div>' })
const ButtonStub = defineComponent({
  inheritAttrs: false,
  props: { disabled: Boolean },
  emits: ['click'],
  template:
    '<button v-bind="$attrs" type="button" :disabled="disabled" @click="$emit(\'click\', $event)"><slot /></button>',
})
const InputStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: [String, Number] },
  emits: ['update:model-value'],
  template:
    '<input v-bind="$attrs" :value="modelValue" @input="$emit(\'update:model-value\', $event.target.value)" />',
})
const ToggleStub = defineComponent({
  props: { modelValue: Boolean },
  emits: ['update:model-value'],
  template: '<button type="button" @click="$emit(\'update:model-value\', !modelValue)" />',
})
const DialogStub = defineComponent({
  props: { open: Boolean },
  emits: ['update:open'],
  template: '<section v-if="open"><slot /></section>',
})

const mountDialog = () =>
  mount(MCPCreateKeyDialog, {
    props: {
      open: true,
      isCreating: false,
      modems: [
        { id: 'imei-a', name: 'Modem A' },
        { id: 'imei-b', name: 'Modem B' },
      ],
      permissionGroups: [
        { module: 'modem', permissions: ['modem.read'] },
        { module: 'sms', permissions: ['sms.read', 'sms.send'] },
      ],
    },
    global: {
      stubs: {
        Button: ButtonStub,
        Checkbox: ToggleStub,
        Dialog: DialogStub,
        DialogContent: SlotStub,
        DialogDescription: SlotStub,
        DialogFooter: SlotStub,
        DialogHeader: SlotStub,
        DialogTitle: SlotStub,
        Input: InputStub,
        Label: SlotStub,
        Spinner: SlotStub,
        Switch: ToggleStub,
      },
    },
  })

describe('MCPCreateKeyDialog', () => {
  it('owns scoped modem and permission selection state', async () => {
    const wrapper = mountDialog()

    await wrapper.get('#mcp-key-name').setValue('SMS agent')
    await wrapper.get('[data-testid="mcp-all-modems"]').trigger('click')

    const modemLabel = wrapper.findAll('label').find((label) => label.text().includes('Modem A'))
    expect(modemLabel).toBeDefined()
    await modemLabel!.get('button').trigger('click')
    await wrapper.get('[data-testid="toggle-all-mcp-permissions"]').trigger('click')
    await wrapper.get('[data-testid="create-mcp-key"]').trigger('click')

    expect(wrapper.emitted('create')).toEqual([
      [
        {
          name: 'SMS agent',
          validityDays: 30,
          allModems: false,
          modemIds: ['imei-a'],
          permissions: ['modem.read', 'sms.read', 'sms.send'],
        },
      ],
    ])
  })

  it('resets transient form state after closing', async () => {
    const wrapper = mountDialog()

    await wrapper.get('#mcp-key-name').setValue('Temporary agent')
    await wrapper.get('[data-testid="toggle-all-mcp-permissions"]').trigger('click')
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })

    expect(wrapper.get<HTMLInputElement>('#mcp-key-name').element.value).toBe('')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="create-mcp-key"]').element.disabled).toBe(
      true,
    )
    expect(wrapper.get('[data-testid="toggle-all-mcp-permissions"]').text()).toBe(
      'settings.mcp.selectAllPermissions',
    )
  })
})
