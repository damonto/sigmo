import { computed, ref, watch, type Ref } from 'vue'
import { communicationApi } from '@/apis/communication'
import type { SIMLine } from '@/types/communication'

/** Retries only the database handover after hardware deletion has completed. */
export const useHistoryAssignments = (
  sourceID: Readonly<Ref<string | undefined>>,
  changed: () => void,
) => {
  const lines = ref<SIMLine[]>([])
  const source = ref<SIMLine>()
  const target = ref('')
  const busy = ref(false)
  const error = ref('')
  const pending = computed(() => lines.value.filter((line) => line.deletionState === 'deleted'))
  const candidates = computed(() => {
    const sourceLine = source.value
    if (!sourceLine?.modemId || sourceLine.deletionState !== 'deleted') return []
    return lines.value.filter(
      (line) =>
        line.modemId === sourceLine.modemId &&
        line.present &&
        !line.deletionState &&
        line.profileId !== sourceLine.profileId,
    )
  })
  const canAssign = computed(() => candidates.value.some((line) => line.profileId === target.value))
  const load = async (cached = false) => {
    lines.value = (await communicationApi.lines(cached)).items
  }
  watch(
    sourceID,
    async (id, _, cleanup) => {
      let cancelled = false
      cleanup(() => {
        cancelled = true
      })
      try {
        const response = await communicationApi.lines(!id)
        if (cancelled) return
        lines.value = response.items
        if (id) source.value = lines.value.find((line) => line.profileId === id)
      } catch (reason) {
        if (!cancelled) error.value = reason instanceof Error ? reason.message : String(reason)
      }
    },
    { immediate: true },
  )
  const assign = async () => {
    if (!source.value || !canAssign.value) return
    busy.value = true
    error.value = ''
    try {
      await communicationApi.assign(source.value.profileId, target.value)
      source.value = undefined
      target.value = ''
      await load(true)
      changed()
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }
  const choose = async (line: SIMLine) => {
    source.value = line
    target.value = ''
    busy.value = true
    try {
      await load()
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }
  return { choose, source, target, busy, error, pending, candidates, canAssign, assign }
}
