import { computed, ref, watch, type Ref } from 'vue'

import { automationApi } from '@/apis/automation'
import { sessionRevision } from '@/lib/authSession'
import { dateTimeLocalToISOString, formatDateTimeLocal } from '@/lib/datetime'
import type {
  AutomationAction,
  AutomationLine,
  AutomationSchedule,
  SimAutomation,
} from '@/types/automation'

const kib = 1024
const mib = kib * kib
const maxData = 100 * mib

interface Draft {
  type: AutomationAction['type']
  scheduledAt: string
  repeatDays: string | number
  amount: string | number
  unit: 'KiB' | 'MiB'
  to: string
  text: string
  duration: string | number
  wifiCallingOnly: boolean
}

const emptyDraft = (): Draft => ({
  type: 'data',
  scheduledAt: '',
  repeatDays: '',
  amount: '',
  unit: 'MiB',
  to: '',
  text: '',
  duration: 10,
  wifiCallingOnly: false,
})

/** Loads and edits the automation for the selected modem, ICCID and secure element. */
export const useSimAutomations = (
  line: Readonly<Ref<AutomationLine | undefined>>,
  onChanged: () => void,
  onSaved: () => void,
) => {
  const task = ref<SimAutomation>()
  const form = ref<Draft>(emptyDraft())
  const loading = ref(false)
  const loaded = ref(false)
  const busy = ref(false)
  const error = ref('')
  const locked = computed(() => !!(task.value?.running || task.value?.restoring))
  let generation = 0
  let refresh: (() => Promise<void>) | undefined

  const draftFor = (current?: SimAutomation): Draft => {
    const draft = emptyDraft()
    if (!current) return draft

    draft.scheduledAt = current.nextAt ? formatDateTimeLocal(current.nextAt) : ''
    draft.repeatDays = current.repeatDays ?? ''
    const action = current.action
    draft.type = action.type
    if (action.type === 'data') {
      const inMiB = action.bytes % mib === 0
      draft.unit = inMiB ? 'MiB' : 'KiB'
      draft.amount = action.bytes / (inMiB ? mib : kib)
    } else {
      draft.to = action.to
      draft.wifiCallingOnly = action.wifiCallingOnly ?? false
      if (action.type === 'sms') draft.text = action.text
      else draft.duration = action.durationSeconds
    }
    return draft
  }

  watch(
    [line, sessionRevision],
    ([selected], _previous, cleanup) => {
      const revision = ++generation
      task.value = undefined
      form.value = emptyDraft()
      busy.value = false
      loaded.value = false
      loading.value = false
      error.value = ''
      refresh = undefined
      if (!selected) return

      const controller = new AbortController()
      refresh = async () => {
        if (loading.value || busy.value) return
        loading.value = true
        loaded.value = false
        error.value = ''
        try {
          const { items } = await automationApi.list(selected.profileId, controller.signal)
          if (revision !== generation) return
          task.value = items.find(
            (item) =>
              item.modemId === selected.modemId && (item.seId ?? '') === (selected.seId ?? ''),
          )
          form.value = draftFor(task.value)
          loaded.value = true
        } catch {
          if (revision === generation) error.value = 'loadFailed'
        } finally {
          if (revision === generation) loading.value = false
        }
      }
      void refresh()
      cleanup(() => {
        generation++
        controller.abort()
      })
    },
    { immediate: true, flush: 'sync' },
  )

  const action = computed<AutomationAction | undefined>(() => {
    const draft = form.value
    if (draft.type === 'data') {
      const bytes = Number(draft.amount) * (draft.unit === 'MiB' ? mib : kib)
      if (Number.isSafeInteger(bytes) && bytes >= kib && bytes <= maxData && bytes % kib === 0) {
        return { type: 'data', bytes }
      }
      return
    }
    if (!draft.to.trim()) return
    const { wifiCallingOnly } = draft
    if (draft.type === 'sms') {
      if (draft.text.trim()) return { type: 'sms', to: draft.to, text: draft.text, wifiCallingOnly }
      return
    }
    const durationSeconds = Number(draft.duration)
    if (Number.isInteger(durationSeconds) && durationSeconds >= 1 && durationSeconds <= 300) {
      return { type: 'call', to: draft.to, durationSeconds, wifiCallingOnly }
    }
  })

  const schedule = computed<AutomationSchedule | undefined>(() => {
    const scheduledAt = dateTimeLocalToISOString(form.value.scheduledAt)
    const repeat = form.value.repeatDays
    const days = Number(repeat)
    if (
      !scheduledAt ||
      !action.value ||
      (repeat !== '' && (!Number.isInteger(days) || days < 1 || days > 3650))
    )
      return
    return { scheduledAt, repeatDays: repeat === '' ? null : days, action: action.value }
  })

  const mutate = async (failure: string, operation: () => Promise<unknown>) => {
    if (!loaded.value || locked.value || busy.value || loading.value || error.value === 'conflict')
      return
    const revision = generation
    busy.value = true
    error.value = ''
    try {
      await operation()
      onChanged()
      if (revision === generation) onSaved()
    } catch (reason) {
      if (revision !== generation) return
      const status = reason instanceof Error && 'status' in reason ? reason.status : undefined
      if (status === 409) error.value = 'conflict'
      else error.value = failure
    } finally {
      if (revision === generation) busy.value = false
    }
  }

  const save = async () => {
    const selected = line.value
    const value = schedule.value
    const taskId = task.value?.id
    if (!selected || !value) return
    if (Date.parse(value.scheduledAt) <= Date.now()) {
      error.value = 'futureTime'
      return
    }
    await mutate('saveFailed', () => automationApi.save(selected, value, taskId))
  }

  const remove = async () => {
    const selected = line.value
    const taskId = task.value?.id
    if (!selected || !taskId) return
    await mutate('deleteFailed', () => automationApi.remove(selected.profileId, taskId))
  }

  return {
    task,
    form,
    loaded,
    loading,
    locked,
    busy,
    error,
    schedule,
    save,
    remove,
    reload: () => refresh?.(),
  }
}
