import { computed, ref, watch, type Ref } from 'vue'
import { communicationApi } from '@/apis/communication'
import { sessionRevision } from '@/lib/authSession'
import type { SIMLine } from '@/types/communication'

type AssignmentPhase = 'idle' | 'loading' | 'ready' | 'assigning'

const hasAssignableHistory = (line: SIMLine) =>
  line.deletionState === 'deleted' && (line.messageCount > 0 || line.callCount > 0)

/** Manages deleted-line history with freshly verified replacement candidates. */
export const useHistoryAssignments = (
  sourceId: Readonly<Ref<string | undefined>>,
  changed: () => void,
) => {
  const lines = ref<SIMLine[]>([])
  const source = ref<SIMLine>()
  const target = ref('')
  const phase = ref<AssignmentPhase>('idle')
  const busy = computed(() => phase.value === 'assigning')
  const loading = computed(() => phase.value === 'loading')
  const error = ref('')
  let generation = 0
  const pending = computed(() => lines.value.filter(hasAssignableHistory))
  const candidates = computed(() => {
    if (phase.value !== 'ready' && phase.value !== 'assigning') return []
    const sourceLine = source.value
    if (!sourceLine?.modemId || !hasAssignableHistory(sourceLine)) return []
    return lines.value.filter(
      (line) =>
        line.modemId === sourceLine.modemId &&
        line.present &&
        !line.deletionState &&
        line.profileId !== sourceLine.profileId,
    )
  })
  const canAssign = computed(
    () =>
      phase.value === 'ready' && candidates.value.some((line) => line.profileId === target.value),
  )
  const reset = () => {
    generation++
    source.value = undefined
    target.value = ''
    error.value = ''
    phase.value = 'idle'
  }
  const close = () => {
    if (!busy.value) reset()
  }
  const choose = async (line: SIMLine) => {
    if (busy.value || !hasAssignableHistory(line)) return
    const current = ++generation
    source.value = line
    target.value = ''
    error.value = ''
    phase.value = 'loading'
    try {
      const response = await communicationApi.lines(false)
      if (current !== generation) return
      lines.value = response.items
      const latest = pending.value.find((entry) => entry.profileId === line.profileId)
      if (!latest) {
        close()
        return
      }
      source.value = latest
      phase.value = 'ready'
    } catch (reason) {
      if (current !== generation) return
      error.value = reason instanceof Error ? reason.message : String(reason)
      phase.value = 'idle'
    }
  }
  const retry = () => {
    if (source.value) return choose(source.value)
  }
  watch(
    [sourceId, sessionRevision],
    async ([id, revision], previous, cleanup) => {
      reset()
      const current = generation
      cleanup(() => {
        generation++
      })
      lines.value = []
      try {
        // Deletion already updated the database; opening the dialog must not wait for hardware.
        const response = await communicationApi.lines(true)
        if (current !== generation) return
        lines.value = response.items
        const changedSession = previous[1] !== undefined && previous[1] !== revision
        const deleted = changedSession
          ? undefined
          : pending.value.find((line) => line.profileId === id)
        if (deleted) await choose(deleted)
      } catch (reason) {
        if (current === generation)
          error.value = reason instanceof Error ? reason.message : String(reason)
      }
    },
    { immediate: true, flush: 'sync' },
  )
  const assign = async () => {
    if (!source.value || !canAssign.value) return
    const current = generation
    phase.value = 'assigning'
    error.value = ''
    try {
      await communicationApi.assign(source.value.profileId, target.value)
      if (current !== generation) return
      source.value = undefined
      target.value = ''
      changed()
      const response = await communicationApi.lines(true)
      if (current === generation) lines.value = response.items
    } catch (reason) {
      if (current === generation)
        error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      if (current === generation) phase.value = source.value ? 'ready' : 'idle'
    }
  }
  return {
    choose,
    close,
    retry,
    source,
    target,
    busy,
    loading,
    error,
    pending,
    candidates,
    canAssign,
    assign,
  }
}
