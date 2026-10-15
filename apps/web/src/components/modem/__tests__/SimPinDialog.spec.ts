import { flushPromises, mount } from '@vue/test-utils'
import { effectScope, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import SimPinDialog from '@/components/modem/SimPinDialog.vue'
import { useSimPin } from '@/composables/useSimPin'

const api = vi.hoisted(() => ({ status: vi.fn(), submit: vi.fn() }))
vi.mock('@/apis/simPin', () => ({ useSimPinApi: () => api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, te: () => true }) }))

const stubs = {
  Dialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
  DialogContent: { template: '<section><slot /></section>' },
  DialogHeader: { template: '<header><slot /></header>' },
  DialogTitle: { template: '<h2><slot /></h2>' },
  DialogDescription: { template: '<p><slot /></p>' },
  DialogFooter: { template: '<footer><slot /></footer>' },
  Switch: {
    props: ['modelValue', 'disabled'],
    emits: ['update:modelValue'],
    template:
      '<button role="switch" :aria-checked="modelValue" :disabled="disabled" @click="$emit(\'update:modelValue\', !modelValue)" />',
  },
}

async function make() {
  vi.resetAllMocks()
  api.status.mockResolvedValue({
    supported: true,
    enabled: false,
    lock: 'none',
    pinRetries: 3,
    pukRetries: 10,
    simToken: 'SIM-A',
  })
  api.submit.mockResolvedValue(undefined)
  const scope = effectScope()
  const controller = scope.run(() => useSimPin(ref('modem-1'), ref('SIM-A')))
  if (!controller) throw new Error('missing scope')
  controller.show()
  const wrapper = mount(SimPinDialog, { props: { controller }, global: { stubs } })
  await flushPromises()
  return {
    controller,
    wrapper,
    close: () => {
      wrapper.unmount()
      scope.stop()
    },
  }
}

describe('SIM PIN dialog', () => {
  it('keeps the setting and credential form in one dialog and updates only after success', async () => {
    const { controller, wrapper, close } = await make()
    expect(wrapper.find('form').exists()).toBe(false)
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.get('h2').text()).toBe('simPin.title')
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(controller.status.value?.enabled).toBe(false)
    await wrapper.get('input').setValue('1234')
    api.status.mockResolvedValue({ ...controller.status.value, enabled: true })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.text()).toContain('simPin.actions.change')
    expect(wrapper.text()).not.toContain('simPin.saved')
    expect(wrapper.text()).not.toContain('simPin.refresh')
    expect(wrapper.find('input').exists()).toBe(false)
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.text()).toContain('simPin.actions.disable')
    expect(wrapper.text()).not.toContain('simPin.actions.change')
    expect(wrapper.findAll('input')).toHaveLength(1)
    close()
  })

  it('keeps the form open with a concise failure and clears the credential', async () => {
    const { wrapper, close } = await make()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('input').setValue('0000')
    api.submit.mockRejectedValue({ error_code: 'sim_pin_incorrect' })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('simPin.errors.sim_pin_incorrect')
    expect(wrapper.find('[role="alertdialog"]').exists()).toBe(false)
    expect(wrapper.get('input').element.value).toBe('')
    expect(wrapper.find('form').exists()).toBe(true)
    expect(api.submit).toHaveBeenCalledTimes(1)
    close()
  })
})
