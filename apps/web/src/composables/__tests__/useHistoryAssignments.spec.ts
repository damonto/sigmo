import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useHistoryAssignments } from '@/composables/useHistoryAssignments'
import type { SIMLine } from '@/types/communication'

const api = vi.hoisted(() => ({ lines: vi.fn(), assign: vi.fn() }))
vi.mock('@/apis/communication', () => ({ communicationApi: api }))

const line = (profileId: string, overrides: Partial<SIMLine> = {}): SIMLine => ({
  profileId,
  modemId: 'modem-1',
  label: profileId,
  number: '',
  region: '',
  kind: 'esim',
  seId: 'default',
  deletionState: '',
  successorId: '',
  messageCount: 0,
  callCount: 0,
  present: true,
  active: true,
  canMessage: true,
  canCall: true,
  ...overrides,
})
const deleted = line('deleted', { deletionState: 'deleted', present: false, active: false })

let scope: ReturnType<typeof effectScope>
const changed = vi.fn()
const createState = (sourceId?: string) => {
  const state = scope.run(() => useHistoryAssignments(ref(sourceId), changed))
  if (!state) throw new Error('missing history assignment state')
  return state
}

beforeEach(() => {
  vi.resetAllMocks()
  scope = effectScope()
})
afterEach(() => scope.stop())

describe('history assignment targets', () => {
  it('keeps active and inactive lines on the source modem and rejects other targets', async () => {
    api.lines.mockResolvedValue({
      items: [
        deleted,
        line('inactive', { active: false, canMessage: false, canCall: false }),
        line('foreign', { modemId: 'modem-2' }),
        line('absent', { present: false }),
        line('pending', { deletionState: 'pending' }),
        line('assigned', { deletionState: 'assigned' }),
        line('deleted-again', { deletionState: 'deleted' }),
        line('active'),
      ],
    })
    const state = createState('deleted')
    await flushPromises()

    expect(state.candidates.value.map((entry) => entry.profileId)).toEqual(['inactive', 'active'])
    expect(state.canAssign.value).toBe(false)
    state.target.value = 'foreign'
    expect(state.canAssign.value).toBe(false)
    await state.assign()
    expect(api.assign).not.toHaveBeenCalled()

    state.target.value = 'inactive'
    expect(state.canAssign.value).toBe(true)
    await state.assign()
    expect(api.assign).toHaveBeenCalledExactlyOnceWith('deleted', 'inactive')
    expect(changed).toHaveBeenCalledOnce()
  })

  it.each([
    { name: 'missing source', items: [line('foreign', { modemId: 'modem-2' })] },
    {
      name: 'unknown modem',
      items: [{ ...deleted, modemId: '' }, line('unknown', { modemId: '' })],
    },
    { name: 'live source', items: [line('deleted'), line('active')] },
    { name: 'no local target', items: [deleted, line('foreign', { modemId: 'modem-2' })] },
  ])('offers no targets for $name', async ({ items }) => {
    api.lines.mockResolvedValue({ items })
    const state = createState('deleted')
    await flushPromises()

    expect(state.candidates.value).toEqual([])
    state.target.value = items[0]?.profileId ?? ''
    expect(state.canAssign.value).toBe(false)
    await state.assign()
    expect(api.assign).not.toHaveBeenCalled()
  })

  it('uses the modem of the deleted line chosen from the pending list', async () => {
    const secondSource = line('second-deleted', {
      modemId: 'modem-2',
      deletionState: 'deleted',
      present: false,
    })
    api.lines.mockResolvedValue({
      items: [deleted, line('local'), secondSource, line('foreign', { modemId: 'modem-2' })],
    })
    const state = createState()
    await flushPromises()
    expect(state.candidates.value).toEqual([])

    await state.choose(deleted)
    expect(state.candidates.value.map((entry) => entry.profileId)).toEqual(['local'])
    state.target.value = 'local'

    await state.choose(secondSource)
    expect(state.candidates.value.map((entry) => entry.profileId)).toEqual(['foreign'])
    expect(state.target.value).toBe('')
    expect(state.canAssign.value).toBe(false)
  })
})
