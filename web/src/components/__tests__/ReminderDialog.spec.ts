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

const mountDialog = (reminder?: { nextAt: string; repeatDays?: number; content: string }) =>
  mount(ReminderDialog, {
    props: {
      open: true,
      profileName: 'Travel',
      reminder,
      'onUpdate:open': () => {},
    },
    global: { stubs },
  })

describe('ReminderDialog', () => {
  it('submits browser-local time as UTC with optional repeat', async () => {
    const wrapper = mountDialog()
    const localDate = new Date(2026, 6, 18, 10, 30, 0, 0)

    await wrapper.get('#reminder-time').setValue('2026-07-18T10:30')
    await wrapper.get('#reminder-repeat').setValue('7')
    await wrapper.get('#reminder-content').setValue(' Renew the plan ')
    await flushPromises()
    await (wrapper.vm as unknown as { save: () => Promise<void> }).save()
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

  it('rejects repeat values above the product limit', async () => {
    const wrapper = mountDialog()

    await wrapper.get('#reminder-time').setValue('2099-07-18T10:30')
    await wrapper.get('#reminder-repeat').setValue('3651')
    await wrapper.get('#reminder-content').setValue('Renew the plan')
    await flushPromises()
    await (wrapper.vm as unknown as { save: () => Promise<void> }).save()
    await flushPromises()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(wrapper.text()).toContain('modemDetail.reminder.validation.repeat')
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
