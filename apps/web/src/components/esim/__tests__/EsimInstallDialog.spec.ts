import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import EsimInstallDialog from '@/components/esim/EsimInstallDialog.vue'
import EsimSESelector from '@/components/esim/EsimSESelector.vue'
import type { SEItem } from '@/types/se'

enableAutoUnmount(afterEach)

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock('vue-qrcode-reader', () => ({
  QrcodeStream: {
    template: '<div />',
  },
}))

const passthrough = {
  template: '<div><slot /></div>',
}

const button = {
  props: ['disabled'],
  template: '<button type="button" :disabled="disabled"><slot /></button>',
}

const mountDialog = (allowTransfer: boolean, ses: SEItem[] = []) =>
  mount(EsimInstallDialog, {
    props: {
      open: true,
      allowTransfer,
      ses,
      'onUpdate:open': () => {},
    },
    global: {
      stubs: {
        Button: button,
        Dialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
        DialogDescription: passthrough,
        DialogHeader: passthrough,
        DialogTitle: passthrough,
        EsimPersistentDialogContent: passthrough,
        RadioGroup: passthrough,
        RadioGroupItem: {
          props: ['id', 'value'],
          template: '<input :id="id" type="radio" :value="value" />',
        },
      },
    },
  })

describe('EsimInstallDialog', () => {
  it('validates an unchanged SM-DP+ address on blur and clears the error on change', async () => {
    const wrapper = mountDialog(false, [{ id: 'default', label: 'eUICC' }])
    await flushPromises()
    const input = wrapper.get('input[name="smdp"]')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)

    await input.trigger('blur')
    await vi.waitFor(() => {
      expect(input.attributes('aria-invalid')).toBe('true')
    })
    const errorId = input.attributes('aria-describedby')
    expect(wrapper.get('[id="' + errorId + '"]').text()).toBe(
      'modemDetail.esim.validation.smdpRequired',
    )

    await wrapper.get('input[name="activationCode"]').trigger('blur')
    await wrapper.get('input[name="confirmationCode"]').trigger('blur')
    expect(wrapper.get('input[name="activationCode"]').attributes('aria-invalid')).toBe('false')
    expect(wrapper.get('input[name="confirmationCode"]').attributes('aria-invalid')).toBe('false')

    await input.setValue('smdp.example.com')
    await vi.waitFor(() => {
      expect(input.attributes('aria-invalid')).toBe('false')
    })
    expect(input.attributes('aria-describedby')).toBeUndefined()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('gives each input its own accessible label', async () => {
    const wrapper = mountDialog(false, [{ id: 'default', label: 'eUICC' }])
    await flushPromises()
    const inputs = wrapper.findAll('input[name]')
    const ids = inputs.map((input) => input.attributes('id'))
    expect(new Set(ids).size).toBe(inputs.length)
    for (const input of inputs) {
      const label = wrapper.get('[id="' + input.attributes('aria-labelledby') + '"]')
      expect(label.attributes('for')).toBe(input.attributes('id'))
      expect(label.text()).not.toBe('')
    }
  })

  it.each(['', '   '])('rejects an empty SM-DP+ address %j', async (smdp) => {
    const wrapper = mountDialog(false, [{ id: 'default', label: 'eUICC' }])
    await flushPromises()

    await wrapper.get('input[name="smdp"]').setValue(smdp)
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('modemDetail.esim.validation.smdpRequired')
    })
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })

  it.each([
    { activationCode: '', confirmationCode: '', wantActivation: '', wantConfirmation: '' },
    {
      activationCode: ' matching id ',
      confirmationCode: ' 1234 ',
      wantActivation: 'matchingid',
      wantConfirmation: '1234',
    },
  ])('submits normalized optional codes: $activationCode', async (testCase) => {
    const wrapper = mountDialog(false, [{ id: 'default', label: 'eUICC' }])
    await flushPromises()

    await wrapper.get('input[name="smdp"]').setValue(' sm dp.example.com ')
    await wrapper.get('input[name="activationCode"]').setValue(testCase.activationCode)
    await wrapper.get('input[name="confirmationCode"]').setValue(testCase.confirmationCode)
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.emitted('confirm')).toEqual([
        [
          {
            smdp: 'smdp.example.com',
            activationCode: testCase.wantActivation,
            confirmationCode: testCase.wantConfirmation,
            seId: 'default',
          },
        ],
      ])
    })
  })

  it.each(['blur', 'submit'])('validates the LPA confirmation code on %s', async (event) => {
    const wrapper = mountDialog(false, [{ id: 'default', label: 'eUICC' }])
    await flushPromises()

    await wrapper.get('input[name="smdp"]').setValue('LPA:1$smdp.example.com$matching-id$$1')
    await flushPromises()
    const confirmation = wrapper.get('input[name="confirmationCode"]')
    if (event === 'blur') {
      await confirmation.trigger('blur')
    } else {
      await wrapper.get('form').trigger('submit')
    }
    await vi.waitFor(() => {
      expect(confirmation.attributes('aria-invalid')).toBe('true')
    })
    expect(wrapper.emitted('confirm')).toBeUndefined()

    await confirmation.setValue(' 1234 ')
    await vi.waitFor(() => {
      expect(confirmation.attributes('aria-invalid')).toBe('false')
    })
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.emitted('confirm')).toEqual([
        [
          {
            smdp: 'smdp.example.com',
            activationCode: 'matching-id',
            confirmationCode: '1234',
            seId: 'default',
          },
        ],
      ])
    })
  })

  it('submits a discovered address through form validation', async () => {
    const wrapper = mountDialog(false, [{ id: 'default', label: 'eUICC' }])
    await flushPromises()

    wrapper.vm.applyDiscoverAddress(' sm dp.example.com ')

    await vi.waitFor(() => {
      expect(wrapper.emitted('confirm')).toEqual([
        [{ smdp: 'smdp.example.com', activationCode: '', confirmationCode: '', seId: 'default' }],
      ])
    })
  })

  it('requires a confirmation code from LPA data and clears that requirement on reopen', async () => {
    const wrapper = mountDialog(false, [{ id: 'default', label: 'eUICC' }])
    await flushPromises()

    await wrapper.get('input[name="smdp"]').setValue('LPA:1$smdp.example.com$matching-id$$1')
    await flushPromises()
    expect(wrapper.get('input[name="confirmationCode"]').attributes('required')).toBeDefined()
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('modemDetail.validation.required')
    })
    expect(wrapper.emitted('confirm')).toBeUndefined()

    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(wrapper.get<HTMLInputElement>('input[name="smdp"]').element.value).toBe('')
    expect(wrapper.get('input[name="confirmationCode"]').attributes('required')).toBeUndefined()
    expect(wrapper.text()).not.toContain('modemDetail.validation.required')

    await wrapper.get('input[name="smdp"]').setValue('smdp.example.com')
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => {
      expect(wrapper.emitted('confirm')).toEqual([
        [{ smdp: 'smdp.example.com', activationCode: '', confirmationCode: '', seId: 'default' }],
      ])
    })
  })

  it.each([
    { name: 'enabled', allowTransfer: true, wantTransfer: true },
    { name: 'disabled', allowTransfer: false, wantTransfer: false },
  ])('controls the transfer entry when $name', ({ allowTransfer, wantTransfer }) => {
    const wrapper = mountDialog(allowTransfer)

    expect(wrapper.text().includes('modemDetail.esim.transferButton')).toBe(wantTransfer)
  })

  it('disables discover and install when no SE is selected', async () => {
    const wrapper = mountDialog(false)
    await flushPromises()

    const disabledButtons = wrapper
      .findAll('button')
      .filter((button) => button.attributes('disabled') !== undefined)

    expect(disabledButtons).toHaveLength(2)
    expect(disabledButtons[0]?.attributes('aria-label')).toBe('modemDetail.esim.discover')
    expect(disabledButtons[1]?.text()).toContain('modemDetail.esim.installConfirm')
  })

  it('enables discover and install after a single SE is loaded', async () => {
    const wrapper = mountDialog(false)
    await flushPromises()

    await wrapper.setProps({
      ses: [{ id: 'default', label: 'eUICC', eid: 'eid-1' }],
    })
    await flushPromises()

    const disabledButtons = wrapper
      .findAll('button')
      .filter((button) => button.attributes('disabled') !== undefined)

    expect(disabledButtons).toHaveLength(0)
  })

  it('shows dual SE choices with EID and storage on separate lines', async () => {
    const wrapper = mountDialog(false, [
      { id: 'se0', label: 'SE1', eid: 'eid-1', freeSpace: 102400 },
      { id: 'se1', label: 'SE2', eid: 'eid-2', freeSpace: 204800 },
    ])
    await flushPromises()

    const text = wrapper.text()

    expect(text).toContain('eid-1')
    expect(text).toContain('Storage Remaining 100 KiB')
    expect(text).toContain('eid-2')
    expect(text).toContain('Storage Remaining 200 KiB')
    expect(text).not.toContain('SE1 EID')
    expect(text).not.toContain('SE2 EID')
  })

  it('does not default to the first SE for dual SE cards', async () => {
    const wrapper = mountDialog(false, [
      { id: 'se0', label: 'SE1', eid: 'eid-1', freeSpace: 102400 },
      { id: 'se1', label: 'SE2', eid: 'eid-2', freeSpace: 204800 },
    ])
    await flushPromises()

    const disabledButtons = wrapper
      .findAll('button')
      .filter((button) => button.attributes('disabled') !== undefined)

    expect(disabledButtons).toHaveLength(2)
    expect(disabledButtons[0]?.attributes('aria-label')).toBe('modemDetail.esim.discover')
    expect(disabledButtons[1]?.text()).toContain('modemDetail.esim.installConfirm')
  })

  it('clears the selected SE when a dual SE dialog reopens', async () => {
    const wrapper = mountDialog(false, [
      { id: 'se0', label: 'SE1', eid: 'eid-1', freeSpace: 102400 },
      { id: 'se1', label: 'SE2', eid: 'eid-2', freeSpace: 204800 },
    ])
    await flushPromises()

    wrapper.findComponent(EsimSESelector).vm.$emit('update:selectedSeId', 'se1')
    await flushPromises()

    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true })
    await flushPromises()

    const disabledButtons = wrapper
      .findAll('button')
      .filter((button) => button.attributes('disabled') !== undefined)

    expect(disabledButtons).toHaveLength(2)
    expect(disabledButtons[0]?.attributes('aria-label')).toBe('modemDetail.esim.discover')
    expect(disabledButtons[1]?.text()).toContain('modemDetail.esim.installConfirm')
  })

  it('emits the selected SE when discovery starts', async () => {
    const wrapper = mountDialog(false, [
      { id: 'se0', label: 'SE1', eid: 'eid-1', freeSpace: 102400 },
      { id: 'se1', label: 'SE2', eid: 'eid-2', freeSpace: 204800 },
    ])
    await flushPromises()

    wrapper.findComponent(EsimSESelector).vm.$emit('update:selectedSeId', 'se1')
    await flushPromises()

    const discoverButton = wrapper
      .findAll('button')
      .find((button) => button.attributes('aria-label') === 'modemDetail.esim.discover')

    expect(discoverButton).toBeDefined()
    await discoverButton?.trigger('click')

    expect(wrapper.emitted('discover')).toEqual([['se1']])
  })
})
