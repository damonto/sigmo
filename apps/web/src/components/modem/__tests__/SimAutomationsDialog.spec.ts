import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import SimAutomationsDialog from '@/components/modem/SimAutomationsDialog.vue'
import type { AutomationAction } from '@/types/automation'

const api = vi.hoisted(() => ({ list: vi.fn(), save: vi.fn(), remove: vi.fn() }))
vi.mock('@/apis/automation', () => ({ automationApi: api }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) =>
      key === 'automation.result.restore_failed' ? 'Could not restore the original line' : key,
    n: String,
  }),
}))

const passthrough = { template: '<div><slot /></div>' }
const line = { profileId: 'travel', modemId: 'modem', seId: 'se', label: 'Travel' }
const mountDialog = (attachTo?: HTMLElement) =>
  mount(SimAutomationsDialog, {
    attachTo,
    props: { modelValue: line },
    global: {
      stubs: {
        Dialog: passthrough,
        DialogContent: passthrough,
        DialogHeader: passthrough,
        DialogTitle: passthrough,
        DialogDescription: passthrough,
      },
    },
  })

afterEach(() => vi.resetAllMocks())

describe('SIM automation editor', () => {
  it.each<{ action: AutomationAction; fields: [string, string][] }>([
    { action: { type: 'data', bytes: 2 * 1024 * 1024 }, fields: [['amount', '2']] },
    {
      action: { type: 'sms', to: '+12025550123', text: 'example', wifiCallingOnly: true },
      fields: [
        ['to', '+12025550123'],
        ['text', 'example'],
      ],
    },
    {
      action: { type: 'call', to: '+12025550123', durationSeconds: 20, wifiCallingOnly: false },
      fields: [
        ['to', '+12025550123'],
        ['duration', '20'],
      ],
    },
  ])('submits only the applicable fields for $action.type', async ({ action, fields }) => {
    api.list.mockResolvedValue({ items: [] })
    const wrapper = mountDialog()
    try {
      await flushPromises()
      await wrapper.get('#automation-type').setValue(action.type)
      await wrapper.get('#automation-time').setValue('2099-11-20T10:30')
      await wrapper.get('#automation-repeat').setValue('2')
      for (const [id, value] of fields) {
        await wrapper.get('#automation-' + id).setValue(value)
      }
      const route = wrapper.find('#automation-wifi-calling-only')
      if (action.type === 'data') {
        expect(route.exists()).toBe(false)
        expect(wrapper.find('#automation-wifi-calling-only-hint').exists()).toBe(false)
      } else {
        expect(route.attributes('aria-checked')).toBe('false')
        const hintId = route.attributes('aria-describedby')
        const hint = wrapper.get(`#${hintId}`).text()
        expect(hint).toContain('automation.wifiCallingOnlyHint')
        expect(hint.includes('automation.wifiCallingOnlySmsHint')).toBe(action.type === 'sms')
        if (action.wifiCallingOnly) await route.trigger('click')
      }
      expect(wrapper.get('[type="submit"]').attributes('disabled')).toBeUndefined()
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(api.save).toHaveBeenCalledExactlyOnceWith(
        line,
        {
          scheduledAt: new Date(2099, 10, 20, 10, 30).toISOString(),
          repeatDays: 2,
          action,
        },
        undefined,
      )
      expect(wrapper.emitted('changed')).toHaveLength(1)
    } finally {
      wrapper.unmount()
    }
  })

  it('opens the existing schedule directly and updates its task', async () => {
    const scheduledAt = new Date(2099, 10, 20, 10, 30).toISOString()
    const task = {
      id: 'sms-1',
      modemId: line.modemId,
      seId: line.seId,
      action: { type: 'sms', to: '+12025550123', text: 'original' },
      nextAt: scheduledAt,
      repeatDays: 3,
    }
    api.list.mockResolvedValue({
      items: [{ ...task, id: 'different-device', modemId: 'other' }, task],
    })
    const wrapper = mountDialog()
    try {
      await flushPromises()
      expect(wrapper.get('#automation-text').element).toHaveProperty('value', 'original')
      expect(wrapper.get('#automation-wifi-calling-only').attributes('aria-checked')).toBe('false')
      expect(wrapper.get('#automation-repeat').element).toHaveProperty('value', '3')
      await wrapper.get('#automation-text').setValue('updated')
      await wrapper.get('form').trigger('submit')
      await flushPromises()

      expect(api.save).toHaveBeenCalledExactlyOnceWith(
        line,
        {
          scheduledAt,
          repeatDays: 3,
          action: { ...task.action, text: 'updated', wifiCallingOnly: false },
        },
        'sms-1',
      )
      expect(wrapper.emitted('update:modelValue')).toEqual([[undefined]])
    } finally {
      wrapper.unmount()
    }
  })

  it('loads the saved Wi-Fi Calling Only option and sends false when cleared', async () => {
    const scheduledAt = new Date(2099, 10, 20, 10, 30).toISOString()
    const task = {
      id: 'call-1',
      modemId: line.modemId,
      seId: line.seId,
      action: { type: 'call', to: '+12025550123', durationSeconds: 20, wifiCallingOnly: true },
      nextAt: scheduledAt,
      repeatDays: null,
    }
    api.list.mockResolvedValue({ items: [task] })
    const wrapper = mountDialog()
    try {
      await flushPromises()
      const route = wrapper.get('#automation-wifi-calling-only')
      expect(route.attributes('aria-checked')).toBe('true')
      await route.trigger('click')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(api.save).toHaveBeenCalledExactlyOnceWith(
        line,
        { scheduledAt, repeatDays: null, action: { ...task.action, wifiCallingOnly: false } },
        task.id,
      )
    } finally {
      wrapper.unmount()
    }
  })

  it('blocks edits after a conflict until the task can be refreshed', async () => {
    const task = {
      id: 'sms-1',
      modemId: line.modemId,
      seId: line.seId,
      action: { type: 'sms', to: '+12025550123', text: 'original' },
      nextAt: '2099-11-20T10:30:00Z',
      repeatDays: null,
    }
    api.list
      .mockResolvedValueOnce({ items: [task] })
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ items: [{ ...task, action: { ...task.action, text: 'latest' } }] })
    api.save.mockRejectedValueOnce(Object.assign(new Error('busy'), { status: 409 }))
    const wrapper = mountDialog()
    try {
      const reload = () =>
        wrapper.findAll('button').find((button) => button.text() === 'automation.refresh')
      await flushPromises()
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(wrapper.text()).toContain('automation.conflict')
      expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()

      await reload()?.trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('automation.loadFailed')
      expect(wrapper.find('form').exists()).toBe(false)

      await reload()?.trigger('click')
      await flushPromises()
      expect(wrapper.get('#automation-text').element).toHaveProperty('value', 'latest')
      expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
      expect(api.save).toHaveBeenCalledTimes(1)
    } finally {
      wrapper.unmount()
    }
  })

  it('uses the schedule clear button and waits for confirmation', async () => {
    api.list.mockResolvedValue({
      items: [
        {
          id: 'automation-1',
          modemId: line.modemId,
          seId: line.seId,
          action: { type: 'data', bytes: 1024 },
          nextAt: null,
        },
      ],
    })
    api.remove.mockResolvedValue(undefined)
    const wrapper = mountDialog(document.body)
    try {
      await flushPromises()
      const clear = wrapper.get('[data-slot="alert-dialog-trigger"]')
      expect(clear.attributes('data-variant')).toBe('ghost')
      expect(clear.find('svg').exists()).toBe(true)

      await clear.trigger('click')
      expect(api.remove).not.toHaveBeenCalled()
      const dialog = document.querySelector('[role="alertdialog"]')
      expect(dialog?.textContent).toContain('automation.clearDescription')
      const confirm = Array.from(dialog?.querySelectorAll<HTMLButtonElement>('button') ?? []).find(
        (button) => button.textContent?.trim() === 'automation.clear',
      )
      confirm?.click()
      await flushPromises()

      expect(api.remove).toHaveBeenCalledExactlyOnceWith(line.profileId, 'automation-1')
      expect(wrapper.emitted('changed')).toHaveLength(1)
    } finally {
      wrapper.unmount()
    }
  })

  it('shows the last result and prevents changes during restoration', async () => {
    api.list.mockResolvedValue({
      items: [
        {
          id: 'sms-1',
          modemId: line.modemId,
          seId: line.seId,
          action: { type: 'sms', to: '+12025550123', text: 'example' },
          nextAt: null,
          repeatDays: null,
          running: false,
          restoring: true,
          lastRun: {
            finishedAt: '2026-11-20T08:00:00Z',
            status: 'unconfirmed',
            restoreCode: 'restore_failed',
          },
        },
      ],
    })
    const wrapper = mountDialog()
    try {
      await flushPromises()
      expect(wrapper.text()).toContain('Could not restore the original line')
      expect(wrapper.text()).toContain('automation.restoring')
      expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    } finally {
      wrapper.unmount()
    }
  })
})
