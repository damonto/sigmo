import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import ReminderDialog from '@/components/ReminderDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      if (key === 'modemDetail.reminder.dayUnit') return 'day'
      if (key === 'modemDetail.reminder.dayUnitPlural') return 'days'
      return key
    },
  }),
}))

const passthrough = { template: '<div><slot /></div>' }
const stubs = {
  AlertDialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
  AlertDialogAction: { template: '<button type="button"><slot /></button>' },
  AlertDialogCancel: { template: '<button type="button"><slot /></button>' },
  AlertDialogContent: passthrough,
  AlertDialogDescription: passthrough,
  AlertDialogFooter: passthrough,
  AlertDialogHeader: passthrough,
  AlertDialogTitle: passthrough,
  Button: {
    props: ['disabled', 'type'],
    template: '<button :type="type || \'button\'" :disabled="disabled"><slot /></button>',
  },
  Dialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
  DialogContent: passthrough,
  DialogDescription: passthrough,
  DialogFooter: passthrough,
  DialogHeader: passthrough,
  DialogTitle: passthrough,
  Label: { template: '<label><slot /></label>' },
  Spinner: { template: '<span />' },
}

const mountDialog = (
  reminder?: { nextAt: string; repeatDays?: number; content: string },
  attachTo?: HTMLElement,
) =>
  mount(ReminderDialog, {
    props: {
      open: true,
      profileName: 'Travel',
      reminder,
      'onUpdate:open': () => {},
    },
    global: { stubs },
    attachTo,
  })

describe('ReminderDialog', () => {
  it.each([
    { id: 'reminder-time', value: '2026-07-18T10:30', error: 'timeRequired' },
    { id: 'reminder-content', value: 'Renew the plan', error: 'contentRequired' },
  ])('validates $id on blur and clears the error on change', async ({ id, value, error }) => {
    const wrapper = mountDialog()
    const input = wrapper.get('#' + id)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)

    await input.trigger('blur')
    await vi.waitFor(() => {
      expect(input.attributes('aria-invalid')).toBe('true')
    })
    expect(input.attributes('aria-describedby')).toBe(id + '-error')
    expect(wrapper.get('#' + id + '-error').text()).toBe('modemDetail.reminder.validation.' + error)

    await input.setValue(value)
    await vi.waitFor(() => {
      expect(input.attributes('aria-invalid')).toBe('false')
    })
    expect(input.attributes('aria-describedby')).toBeUndefined()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.emitted('save')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps the mobile header left aligned and constrains the datetime input', () => {
    const wrapper = mountDialog()
    const header = wrapper
      .findAll('[class]')
      .find((element) => element.classes().includes('text-left'))
    const timeGroup = wrapper.get('[data-testid="reminder-time-group"]')
    const timeInput = wrapper.get('#reminder-time')

    expect(header?.text()).toContain('modemDetail.reminder.description')
    expect(timeGroup.classes()).toContain('overflow-hidden')
    expect(timeInput.attributes('placeholder')).toBe('modemDetail.reminder.timePlaceholder')
    expect(timeInput.classes()).toEqual(
      expect.arrayContaining([
        'appearance-none',
        '[&::-webkit-calendar-picker-indicator]:opacity-0',
      ]),
    )
  })

  it('focuses the dialog heading instead of opening the iOS time picker', () => {
    const host = document.createElement('div')
    document.body.append(host)
    const wrapper = mountDialog(undefined, host)
    const event = new Event('openAutoFocus', { cancelable: true })
    const focusTarget = wrapper.get('[tabindex="-1"]')
    const vm = wrapper.vm as unknown as {
      handleOpenAutoFocus: (event: Event) => void
    }

    vm.handleOpenAutoFocus(event)

    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(focusTarget.element)

    wrapper.unmount()
    host.remove()
  })

  it('submits browser-local time as UTC with optional repeat', async () => {
    const wrapper = mountDialog()
    const localDate = new Date(2026, 6, 18, 10, 30, 0, 0)

    await wrapper.get('#reminder-time').setValue('2026-07-18T10:30')
    await wrapper.get('#reminder-repeat').setValue('7')
    await wrapper.get('#reminder-content').setValue(' Renew the plan ')
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.emitted('save')?.[0]?.[0]).toEqual({
      scheduledAt: localDate.toISOString(),
      repeatDays: 7,
      content: 'Renew the plan',
    })
  })

  it('prefills and clears an existing reminder', async () => {
    const nextAt = new Date(2026, 6, 18, 10, 30, 0, 0).toISOString()
    const wrapper = mountDialog({ nextAt, repeatDays: 3, content: 'Top up' })

    expect((wrapper.get('#reminder-time').element as HTMLInputElement).value).toBe(
      '2026-07-18T10:30',
    )
    expect((wrapper.get('#reminder-repeat').element as HTMLInputElement).value).toBe('3')
    expect((wrapper.get('#reminder-content').element as HTMLTextAreaElement).value).toBe('Top up')

    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('reminder.clear'))
      ?.trigger('click')
    const clearButtons = wrapper
      .findAll('button')
      .filter((button) => button.text().includes('reminder.clear'))
    await clearButtons[clearButtons.length - 1]?.trigger('click')

    expect(wrapper.emitted('clear')).toHaveLength(1)
  })

  it.each(['0', '-1', '1.5', '3651'])('rejects an invalid repeat interval %s', async (repeat) => {
    const wrapper = mountDialog()

    await wrapper.get('#reminder-time').setValue('2099-07-18T10:30')
    await wrapper.get('#reminder-repeat').setValue(repeat)
    await wrapper.get('#reminder-content').setValue('Renew the plan')
    await flushPromises()
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('modemDetail.reminder.validation.repeat')
    })
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('submits a one-time reminder when the repeat interval is empty', async () => {
    const wrapper = mountDialog()

    await wrapper.get('#reminder-time').setValue('2099-07-18T10:30')
    await wrapper.get('#reminder-content').setValue(' Renew the plan ')
    await wrapper.get('form').trigger('submit')

    await vi.waitFor(() => {
      expect(wrapper.emitted('save')?.[0]?.[0]).toEqual({
        scheduledAt: new Date(2099, 6, 18, 10, 30).toISOString(),
        repeatDays: null,
        content: 'Renew the plan',
      })
    })
  })

  it('renders the repeat unit inside the input group with English pluralization', async () => {
    const wrapper = mountDialog()
    const unit = wrapper.get('[data-testid="reminder-repeat-unit"]')

    expect(wrapper.find('[data-slot="input-group"]').exists()).toBe(true)
    expect(unit.text()).toBe('day')

    await wrapper.get('#reminder-repeat').setValue('1')
    expect(unit.text()).toBe('day')

    await wrapper.get('#reminder-repeat').setValue('2')
    expect(unit.text()).toBe('days')
  })
})
