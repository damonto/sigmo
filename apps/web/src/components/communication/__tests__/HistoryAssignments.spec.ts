import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'

import HistoryAssignments from '@/components/communication/HistoryAssignments.vue'
import type { SIMLine } from '@/types/communication'

enableAutoUnmount(afterEach)

const api = vi.hoisted(() => ({ lines: vi.fn(), assign: vi.fn() }))
vi.mock('@/apis/communication', () => ({ communicationApi: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const deleted: SIMLine = {
  profileId: 'deleted',
  modemId: 'modem-1',
  label: 'Deleted line',
  number: '',
  region: '',
  kind: 'esim',
  seId: 'default',
  deletionState: 'deleted',
  successorId: '',
  messageCount: 2,
  callCount: 0,
  present: false,
  active: false,
  canMessage: false,
  canCall: false,
}

const mountAssignments = (sourceId?: string) =>
  mount(HistoryAssignments, {
    props: { sourceId },
    global: {
      stubs: {
        Dialog: {
          props: ['open'],
          template: '<div v-if="open" role="dialog"><slot /></div>',
        },
        DialogContent: { template: '<div><slot /></div>' },
        DialogHeader: { template: '<div><slot /></div>' },
        DialogTitle: { template: '<h2><slot /></h2>' },
        DialogDescription: { template: '<p><slot /></p>' },
        Select: true,
      },
    },
  })

beforeEach(() => {
  vi.resetAllMocks()
  api.lines.mockResolvedValue({ items: [deleted] })
})

describe('history assignment presentation', () => {
  it('opens after deletion without adding a deleted-number banner to the page', async () => {
    api.lines.mockImplementation((cached: boolean) =>
      cached ? Promise.resolve({ items: [deleted] }) : new Promise(() => {}),
    )
    const wrapper = mountAssignments('deleted')
    await flushPromises()

    expect(wrapper.get('[role="dialog"]').text()).toContain('communication.assign')
    expect(wrapper.get('[role="status"]').text()).toBe('communication.loading')
    expect(wrapper.text()).not.toContain('communication.deletedLines')
    expect(wrapper.text()).not.toContain('communication.noLines')
    const buttons = wrapper.findAll('[role="dialog"] button')
    expect(buttons[0]?.attributes('disabled')).toBeUndefined()
    expect(buttons[1]?.attributes('disabled')).toBeDefined()
    await buttons[0]?.trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('shows inventory errors and allows retrying inside the dialog', async () => {
    api.lines.mockResolvedValueOnce({ items: [deleted] })
    api.lines.mockRejectedValueOnce(new Error('Inventory unavailable'))
    const wrapper = mountAssignments('deleted')
    await flushPromises()

    expect(wrapper.get('[role="dialog"] [role="alert"]').text()).toBe('Inventory unavailable')
    expect(wrapper.text()).not.toContain('communication.noLines')
    await wrapper.get('[role="dialog"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    expect(api.lines).toHaveBeenLastCalledWith(false)
  })

  it.each(['deleted', undefined])(
    'shows neither a banner nor a dialog for a deleted line without history: %s',
    async (sourceId) => {
      api.lines.mockResolvedValue({ items: [{ ...deleted, messageCount: 0 }] })
      const wrapper = mountAssignments(sourceId)
      await flushPromises()

      expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
      expect(wrapper.text()).toBe('')
    },
  )

  it('keeps the device page clear after postponing assignment', async () => {
    const wrapper = mountAssignments('deleted')
    await flushPromises()

    await wrapper.get('[role="dialog"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.text()).toBe('')
  })

  it('keeps deferred assignment accessible from communication pages', async () => {
    const wrapper = mountAssignments()
    await flushPromises()

    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('communication.deletedLines')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('Deleted line')
  })
})
