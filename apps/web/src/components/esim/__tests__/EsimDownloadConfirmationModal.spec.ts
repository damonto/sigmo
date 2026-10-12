import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import EsimDownloadConfirmationModal from '@/components/esim/EsimDownloadConfirmationModal.vue'

enableAutoUnmount(afterEach)

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const passthrough = { template: '<div><slot /></div>' }

const mountModal = async (code = '') => {
  const wrapper = mount(EsimDownloadConfirmationModal, {
    props: {
      open: false,
      code,
      title: 'Confirm download',
      hint: 'Enter the confirmation code',
      placeholder: 'Confirmation code',
      confirmLabel: 'Confirm',
      cancelLabel: 'Cancel',
      'onUpdate:code': () => {},
    },
    global: {
      stubs: {
        Dialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
        DialogDescription: passthrough,
        DialogFooter: passthrough,
        DialogHeader: passthrough,
        DialogTitle: passthrough,
        EsimPersistentDialogContent: passthrough,
      },
    },
  })
  await wrapper.setProps({ open: true })
  await flushPromises()
  return wrapper
}

describe('EsimDownloadConfirmationModal', () => {
  it.each(['blur', 'submit'])(
    'clears a %s error as soon as the code is corrected',
    async (event) => {
      const wrapper = await mountModal()
      const input = wrapper.get('input[name="code"]')
      expect(input.attributes('aria-invalid')).toBe('false')
      expect(wrapper.find('[role="alert"]').exists()).toBe(false)

      if (event === 'blur') {
        await input.trigger('blur')
      } else {
        await wrapper.get('form').trigger('submit')
      }

      await vi.waitFor(() => {
        expect(input.attributes('aria-invalid')).toBe('true')
      })
      const errorId = input.attributes('aria-describedby')
      expect(wrapper.get('[id="' + errorId + '"]').text()).toBe('modemDetail.validation.required')
      expect(wrapper.emitted('submit')).toBeUndefined()

      await input.setValue('1234')
      await vi.waitFor(() => {
        expect(input.attributes('aria-invalid')).toBe('false')
      })
      expect(input.attributes('aria-describedby')).toBeUndefined()
      expect(wrapper.find('[role="alert"]').exists()).toBe(false)

      await input.trigger('blur')
      await wrapper.get('form').trigger('submit')
      await vi.waitFor(() => {
        expect(wrapper.emitted('submit')).toHaveLength(1)
      })
    },
  )

  it.each(['', '   '])('rejects an empty confirmation code %j', async (code) => {
    const wrapper = await mountModal(code)
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('modemDetail.validation.required')
    })
    const input = wrapper.get('input[name="code"]')
    const errorId = input.attributes('aria-describedby')
    expect(input.attributes('aria-invalid')).toBe('true')
    expect(errorId).toBeDefined()
    expect(wrapper.get('[id="' + errorId + '"]').text()).toContain(
      'modemDetail.validation.required',
    )
    expect(wrapper.find('label[for="' + input.attributes('id') + '"]').exists()).toBe(true)
    expect(wrapper.emitted('submit')).toBeUndefined()
  })

  it('prefills the model and submits a trimmed confirmation code', async () => {
    const wrapper = await mountModal('1234')
    expect(wrapper.get<HTMLInputElement>('input[name="code"]').element.value).toBe('1234')

    await wrapper.get('input[name="code"]').setValue(' 5678 ')
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.emitted('submit')).toHaveLength(1)
    })
    expect(wrapper.emitted('update:code')).toEqual([['5678']])
  })

  it('clears validation errors and reloads the model when reopened', async () => {
    const wrapper = await mountModal()
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('modemDetail.validation.required')
    })

    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true, code: '9876' })
    await flushPromises()

    expect(wrapper.get<HTMLInputElement>('input[name="code"]').element.value).toBe('9876')
    expect(wrapper.text()).not.toContain('modemDetail.validation.required')
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => {
      expect(wrapper.emitted('submit')).toHaveLength(1)
    })
  })
})
