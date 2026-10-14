import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useModemApi } from '@/apis/modem'
import { communicationApi } from '@/apis/communication'
import { useCommunicationDial } from '@/composables/useCommunicationDial'
import { sessionRevision } from '@/lib/authSession'
import type { Modem } from '@/types/modem'

/** A reply stays on its original modem; a sole remaining modem needs no picker. */
export const chooseCommunicationModem = (modems: Modem[], originalID = ''): string => {
  if (modems.some((modem) => modem.id === originalID)) return originalID
  return modems.length === 1 ? (modems[0]?.id ?? '') : ''
}

/** Resolves the active SIM at submit time; device pages can fix the modem instead of offering a choice. */
export const useCommunicationComposer = (
  participant: Readonly<Ref<string>>,
  mode: Readonly<Ref<'message' | 'call'>>,
  originalModemID: Readonly<Ref<string>> = ref(''),
  fixedModemID: Readonly<Ref<string>> = ref(''),
) => {
  const { t } = useI18n()
  const api = useModemApi()
  const dial = useCommunicationDial()
  const modems = ref<Modem[]>([])
  const selection = ref('')
  let explicitModemID = ''
  let disposed = false
  let generation = 0
  onScopeDispose(() => {
    disposed = true
  })
  const selectedID = computed({
    get: () => selection.value,
    set: (id: string) => {
      explicitModemID = id
      selection.value = id
    },
  })
  const message = ref('')
  const loading = ref(false)
  const busy = ref(false)
  const error = ref('')
  const unavailable = ref(false)
  const selected = computed(() => modems.value.find((modem) => modem.id === selectedID.value))
  const canSubmit = computed(
    () =>
      !!selected.value &&
      !!participant.value.trim() &&
      !loading.value &&
      !busy.value &&
      (mode.value === 'call' || !!message.value.trim()),
  )
  watch(
    [originalModemID, sessionRevision, fixedModemID],
    async (_, previous, cleanup) => {
      let cancelled = false
      cleanup(() => {
        cancelled = true
        generation++
      })
      if (previous?.[1] !== sessionRevision() || previous?.[2] !== fixedModemID.value) {
        explicitModemID = ''
        message.value = ''
      }
      if (previous?.[0] !== originalModemID.value) explicitModemID = ''
      selection.value = ''
      modems.value = []
      loading.value = true
      unavailable.value = false
      error.value = ''
      try {
        let available: Modem[]
        if (fixedModemID.value) {
          const { data } = await api.getModem(fixedModemID.value)
          if (cancelled) return
          if (!data.value) throw new Error(t('communication.currentModemUnavailable'))
          available = [data.value]
        } else {
          const { data } = await api.getModems()
          available = data.value ?? []
        }
        if (cancelled) return
        modems.value = available
        const preferred = fixedModemID.value || explicitModemID || originalModemID.value
        selection.value = chooseCommunicationModem(modems.value, preferred)
        unavailable.value = !!preferred && !modems.value.some((modem) => modem.id === preferred)
        error.value = ''
      } catch (reason) {
        if (!cancelled) error.value = reason instanceof Error ? reason.message : String(reason)
      } finally {
        if (!cancelled) loading.value = false
      }
    },
    { immediate: true, flush: 'sync' },
  )
  const resetSelection = () => {
    explicitModemID = ''
    selection.value = chooseCommunicationModem(
      modems.value,
      fixedModemID.value || originalModemID.value,
    )
  }
  const submit = async (): Promise<boolean> => {
    const id = selected.value?.id
    if (!id || !canSubmit.value) return false
    const revision = sessionRevision(),
      current = generation,
      number = participant.value,
      text = message.value,
      action = mode.value
    const isCurrent = () => !disposed && current === generation && revision === sessionRevision()
    busy.value = true
    error.value = ''
    try {
      // Refresh the chosen device, then pin its current SIM for the actual request.
      const { data } = await api.getModem(id)
      if (!isCurrent()) return false
      const modem = data.value
      if (!modem?.sim.identifier || modem.state === 'locked')
        throw new Error(
          t(
            fixedModemID.value
              ? 'communication.currentModemUnavailable'
              : 'communication.modemUnavailable',
          ),
        )
      const target = { modemId: id, profileId: modem.sim.identifier }
      if (action === 'call') {
        const accepted = (await dial?.(number, target)) ?? false
        if (!isCurrent()) return false
        if (!accepted) error.value = t('communication.callBusy')
        return accepted
      }
      await communicationApi.send(target, number, text)
      if (!isCurrent()) return false
      message.value = ''
      return true
    } catch (reason) {
      if (isCurrent()) error.value = reason instanceof Error ? reason.message : String(reason)
      return false
    } finally {
      busy.value = false
    }
  }
  return {
    modems,
    selectedID,
    message,
    loading,
    busy,
    error,
    unavailable,
    canSubmit,
    submit,
    resetSelection,
  }
}
