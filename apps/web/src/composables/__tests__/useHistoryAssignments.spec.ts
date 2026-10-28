import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useHistoryAssignments } from '@/composables/useHistoryAssignments'
import { invalidateSession } from '@/lib/authSession'
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
const deleted = line('deleted', {
  deletionState: 'deleted',
  present: false,
  active: false,
  messageCount: 2,
})

let scope: ReturnType<typeof effectScope>
const changed = vi.fn()
const createState = (sourceId = ref<string>()) => {
  const state = scope.run(() => useHistoryAssignments(sourceId, changed))
  if (!state) throw new Error('missing history assignment state')
  return state
}

beforeEach(() => {
  vi.resetAllMocks()
  scope = effectScope()
})
afterEach(() => scope.stop())

describe('history assignment targets', () => {
  it.each([
    { messageCount: 2, callCount: 0 },
    { messageCount: 0, callCount: 1 },
  ])('opens for existing history before hardware inventory completes: %o', async (counts) => {
    const source = { ...deleted, ...counts }
    let finishInventory: (response: { items: SIMLine[] }) => void = () => {}
    const inventory = new Promise<{ items: SIMLine[] }>((resolve) => {
      finishInventory = resolve
    })
    api.lines.mockImplementation((cached: boolean) =>
      cached ? Promise.resolve({ items: [source] }) : inventory,
    )

    const state = createState(ref('deleted'))
    await flushPromises()

    expect(api.lines.mock.calls).toEqual([[true], [false]])
    expect(state.source.value).toEqual(source)
    expect(state.loading.value).toBe(true)
    expect(state.busy.value).toBe(false)
    expect(state.canAssign.value).toBe(false)

    finishInventory({ items: [source, line('active')] })
    await flushPromises()
    expect(state.loading.value).toBe(false)
    expect(state.candidates.value.map((entry) => entry.profileId)).toEqual(['active'])
  })

  it('skips the dialog and hardware inventory when the deleted line has no history', async () => {
    const empty = { ...deleted, messageCount: 0 }
    api.lines.mockResolvedValue({ items: [empty, line('active')] })
    const state = createState(ref('deleted'))
    await flushPromises()

    expect(state.source.value).toBeUndefined()
    expect(state.pending.value).toEqual([])
    expect(state.busy.value).toBe(false)
    expect(api.lines).toHaveBeenCalledExactlyOnceWith(true)
    await state.choose(empty)
    expect(state.source.value).toBeUndefined()
    expect(api.lines).toHaveBeenCalledOnce()
  })

  it('lists only deleted lines with message or call history for later assignment', async () => {
    const calls = { ...deleted, profileId: 'calls', messageCount: 0, callCount: 1 }
    api.lines.mockResolvedValue({
      items: [
        deleted,
        calls,
        { ...deleted, profileId: 'empty', messageCount: 0 },
        { ...deleted, profileId: 'pending', deletionState: 'pending' },
        { ...deleted, profileId: 'assigned', deletionState: 'assigned' },
        line('active', { messageCount: 1 }),
      ],
    })
    const state = createState()
    await flushPromises()

    expect(state.pending.value.map((entry) => entry.profileId)).toEqual(['deleted', 'calls'])
    expect(state.source.value).toBeUndefined()
    expect(api.lines).toHaveBeenCalledExactlyOnceWith(true)
  })

  it('keeps the dialog open and reports an inventory error', async () => {
    api.lines.mockResolvedValueOnce({ items: [deleted] })
    api.lines.mockRejectedValueOnce(new Error('Inventory unavailable'))
    const state = createState(ref('deleted'))
    await flushPromises()

    expect(state.source.value).toEqual(deleted)
    expect(state.error.value).toBe('Inventory unavailable')
    expect(state.busy.value).toBe(false)
    expect(state.canAssign.value).toBe(false)
  })

  it('ignores a delayed inventory from a previous deletion', async () => {
    let finishInventory: (response: { items: SIMLine[] }) => void = () => {}
    const inventory = new Promise<{ items: SIMLine[] }>((resolve) => {
      finishInventory = resolve
    })
    const secondSource = { ...deleted, profileId: 'second-deleted' }
    const items = [deleted, secondSource, line('active')]
    api.lines.mockResolvedValue({ items })
    api.lines.mockResolvedValueOnce({ items })
    api.lines.mockReturnValueOnce(inventory)
    const sourceId = ref('deleted')
    const state = createState(sourceId)
    await flushPromises()
    state.target.value = 'active'

    sourceId.value = 'second-deleted'
    await flushPromises()
    expect(state.source.value).toEqual(secondSource)
    expect(state.target.value).toBe('')
    expect(state.busy.value).toBe(false)

    finishInventory({ items: [deleted, line('stale')] })
    await flushPromises()
    expect(state.source.value).toEqual(secondSource)
    expect(state.candidates.value.map((entry) => entry.profileId)).toEqual(['active'])
  })

  it('allows postponing during inventory and ignores its late failure', async () => {
    let failInventory: (reason: Error) => void = () => {}
    const inventory = new Promise<never>((_, reject) => {
      failInventory = reject
    })
    api.lines.mockResolvedValueOnce({ items: [deleted] })
    api.lines.mockReturnValueOnce(inventory)
    const state = createState(ref('deleted'))
    await flushPromises()

    state.close()
    expect(state.source.value).toBeUndefined()
    expect(state.loading.value).toBe(false)
    failInventory(new Error('Late failure'))
    await flushPromises()
    expect(state.source.value).toBeUndefined()
    expect(state.error.value).toBe('')
  })

  it('rejects stale candidates after an inventory failure and supports retry', async () => {
    api.lines.mockResolvedValue({ items: [deleted, line('active')] })
    const state = createState(ref('deleted'))
    await flushPromises()
    state.target.value = 'active'
    expect(state.canAssign.value).toBe(true)

    api.lines.mockRejectedValueOnce(new Error('Inventory unavailable'))
    await state.choose(deleted)
    state.target.value = 'active'
    expect(state.candidates.value).toEqual([])
    expect(state.canAssign.value).toBe(false)
    await state.assign()
    expect(api.assign).not.toHaveBeenCalled()

    await state.retry()
    state.target.value = 'active'
    expect(state.error.value).toBe('')
    expect(state.canAssign.value).toBe(true)
  })

  it.each([
    { ...deleted, messageCount: 0 },
    { ...deleted, deletionState: 'assigned' },
    { ...deleted, deletionState: '' },
  ])('closes when fresh inventory no longer has assignable history: %o', async (latest) => {
    api.lines.mockResolvedValueOnce({ items: [deleted] })
    api.lines.mockResolvedValueOnce({ items: [latest, line('active')] })
    const state = createState(ref('deleted'))
    await flushPromises()

    expect(state.source.value).toBeUndefined()
    expect(state.pending.value).toEqual([])
    expect(state.loading.value).toBe(false)
    expect(state.canAssign.value).toBe(false)
  })

  it('prevents duplicate assignments and keeps submission locked until completion', async () => {
    let finishAssignment: () => void = () => {}
    api.assign.mockReturnValue(
      new Promise<void>((resolve) => {
        finishAssignment = resolve
      }),
    )
    api.lines.mockResolvedValue({ items: [deleted, line('active')] })
    const state = createState(ref('deleted'))
    await flushPromises()
    state.target.value = 'active'

    const assignment = state.assign()
    await state.assign()
    state.close()
    expect(state.source.value).toEqual(deleted)
    expect(state.busy.value).toBe(true)
    expect(state.canAssign.value).toBe(false)
    expect(api.assign).toHaveBeenCalledExactlyOnceWith('deleted', 'active')

    finishAssignment()
    await assignment
    expect(state.source.value).toBeUndefined()
    expect(changed).toHaveBeenCalledOnce()
  })

  it('refreshes history after confirmed assignment even if the metadata reload fails', async () => {
    api.lines.mockResolvedValue({ items: [deleted, line('active')] })
    const state = createState(ref('deleted'))
    await flushPromises()
    state.target.value = 'active'
    api.lines.mockRejectedValueOnce(new Error('Metadata unavailable'))

    await state.assign()
    expect(changed).toHaveBeenCalledOnce()
    expect(state.source.value).toBeUndefined()
    expect(state.error.value).toBe('Metadata unavailable')
    expect(state.busy.value).toBe(false)
  })

  it('keeps the source and target available for retry after assignment fails', async () => {
    api.lines.mockResolvedValue({ items: [deleted, line('active')] })
    api.assign.mockRejectedValueOnce(new Error('Assignment unavailable'))
    const state = createState(ref('deleted'))
    await flushPromises()
    state.target.value = 'active'

    await state.assign()
    expect(state.source.value).toEqual(deleted)
    expect(state.target.value).toBe('active')
    expect(state.canAssign.value).toBe(true)
    expect(state.error.value).toBe('Assignment unavailable')
    expect(changed).not.toHaveBeenCalled()

    await state.assign()
    expect(api.assign).toHaveBeenCalledTimes(2)
    expect(changed).toHaveBeenCalledOnce()
    expect(state.source.value).toBeUndefined()
  })

  it('discards directory metadata received after the session changes', async () => {
    let finishDirectory: (response: { items: SIMLine[] }) => void = () => {}
    api.lines.mockReturnValueOnce(
      new Promise<{ items: SIMLine[] }>((resolve) => {
        finishDirectory = resolve
      }),
    )
    api.lines.mockResolvedValue({ items: [] })
    const state = createState(ref('deleted'))

    invalidateSession()
    await flushPromises()
    finishDirectory({ items: [deleted] })
    await flushPromises()

    expect(state.source.value).toBeUndefined()
    expect(state.pending.value).toEqual([])
    expect(api.lines.mock.calls).toEqual([[true], [true]])
  })

  it('discards an old inventory after the session changes without reopening the dialog', async () => {
    let finishInventory: (response: { items: SIMLine[] }) => void = () => {}
    api.lines.mockResolvedValueOnce({ items: [deleted] })
    api.lines.mockReturnValueOnce(
      new Promise<{ items: SIMLine[] }>((resolve) => {
        finishInventory = resolve
      }),
    )
    api.lines.mockResolvedValue({ items: [] })
    const state = createState(ref('deleted'))
    await flushPromises()

    invalidateSession()
    expect(state.source.value).toBeUndefined()
    await flushPromises()
    finishInventory({ items: [deleted, line('active')] })
    await flushPromises()

    expect(state.source.value).toBeUndefined()
    expect(state.pending.value).toEqual([])
    expect(state.canAssign.value).toBe(false)
  })

  it('ignores a previous assignment completion after another source is selected', async () => {
    let finishAssignment: () => void = () => {}
    api.assign.mockReturnValue(
      new Promise<void>((resolve) => {
        finishAssignment = resolve
      }),
    )
    const second = { ...deleted, profileId: 'second-deleted' }
    api.lines.mockResolvedValue({ items: [deleted, second, line('active')] })
    const sourceId = ref('deleted')
    const state = createState(sourceId)
    await flushPromises()
    state.target.value = 'active'
    const assignment = state.assign()

    sourceId.value = second.profileId
    await flushPromises()
    finishAssignment()
    await assignment
    expect(state.source.value).toEqual(second)
    expect(changed).not.toHaveBeenCalled()
    expect(state.target.value).toBe('')
  })

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
    const state = createState(ref('deleted'))
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
    const state = createState(ref('deleted'))
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
      callCount: 1,
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
