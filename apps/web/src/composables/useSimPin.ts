import { computed, onScopeDispose, readonly, ref, watch, type Ref } from 'vue'

import { useSimPinApi } from '@/apis/simPin'
import type { SimPinCommand, SimPinStatus } from '@/types/simPin'

type Action = 'enable' | 'disable' | 'change' | 'pin' | 'puk'
const PIN = /^[0-9]{4,8}$/
const PUK = /^[0-9]{8}$/

function problemCode(error: unknown): string {
  if (
    typeof error === 'object' &&
    error !== null &&
    'error_code' in error &&
    typeof error.error_code === 'string'
  ) {
    return error.error_code
  }
  return 'sim_pin_result_unknown'
}

function isLegacy(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    'status' in error &&
    (error.status === 404 || error.status === 405)
  )
}

/** Owns ephemeral credentials and ignores responses from closed or changed SIM pages. */
export function useSimPin(modemId: Ref<string>, simIdentity: Ref<string>) {
  const api = useSimPinApi()
  const status = ref<SimPinStatus | null>(null)
  const legacy = ref(false)
  const open = ref(false)
  const loading = ref(false)
  const busy = ref(false)
  const action = ref<Action | null>(null)
  const pin = ref('')
  const newPin = ref('')
  const confirmation = ref('')
  const errorCode = ref('')
  const acknowledged = ref(false)
  const available = ref(false)
  let revision = 0
  let readRevision = 0
  let read: AbortController | undefined

  const needsNewPin = computed(() => action.value === 'change' || action.value === 'puk')
  const canSubmit = computed(() => {
    if (
      !action.value ||
      busy.value ||
      loading.value ||
      acknowledged.value ||
      !status.value?.supported
    )
      return false
    if (!(action.value === 'puk' ? PUK : PIN).test(pin.value)) return false
    return !needsNewPin.value || (PIN.test(newPin.value) && newPin.value === confirmation.value)
  })

  function clearCredentials() {
    pin.value = ''
    newPin.value = ''
    confirmation.value = ''
  }
  function resetForm(next: Action | null) {
    clearCredentials()
    action.value = next
  }

  function choose(next: Action | null) {
    if (busy.value || loading.value) return
    errorCode.value = ''
    acknowledged.value = false
    resetForm(next)
  }

  function inferAction() {
    if (!status.value?.supported) {
      resetForm(null)
      return
    }
    switch (status.value?.lock) {
      case 'pin':
      case 'puk':
        resetForm(status.value.lock)
        break
      default:
        resetForm(null)
    }
  }

  function retainFailedAction(submitted: Action, token: string) {
    const current = status.value
    if (
      acknowledged.value ||
      !current?.supported ||
      current.simToken !== token ||
      current.lock !== 'none' ||
      current.enabled === null
    ) {
      return
    }
    if (
      (submitted === 'change' && current.enabled) ||
      (submitted === 'enable' && !current.enabled) ||
      (submitted === 'disable' && current.enabled)
    ) {
      resetForm(submitted)
    }
  }

  function cancelRead() {
    read?.abort()
    read = undefined
    readRevision++
    loading.value = false
  }

  async function refresh(preserveError = false) {
    if (!preserveError) errorCode.value = ''
    cancelRead()
    const controller = new AbortController()
    read = controller
    const page = revision
    const request = readRevision
    loading.value = true
    try {
      const next = await api.status(modemId.value, controller.signal)
      if (page !== revision || request !== readRevision || controller.signal.aborted) return
      status.value = next
      available.value = next.supported
      legacy.value = false
      inferAction()
    } catch (error) {
      if (page !== revision || request !== readRevision || controller.signal.aborted) return
      status.value = null
      resetForm(null)
      legacy.value = isLegacy(error)
      available.value = !legacy.value && problemCode(error) !== 'sim_pin_unsupported'
      if (!legacy.value && !errorCode.value) errorCode.value = 'sim_pin_unavailable'
    } finally {
      if (page === revision && request === readRevision) loading.value = false
    }
  }

  function show() {
    if (busy.value || open.value) return
    clearCredentials()
    errorCode.value = ''
    acknowledged.value = false
    open.value = true
    void refresh()
  }

  function close() {
    if (busy.value) return
    revision++
    cancelRead()
    open.value = false
    resetForm(null)
    errorCode.value = ''
    acknowledged.value = false
  }

  async function submit(): Promise<boolean> {
    if (!canSubmit.value || !status.value || !action.value) return false
    const page = revision
    const id = modemId.value
    const simToken = status.value.simToken
    const submittedAction = action.value
    let command: SimPinCommand
    switch (action.value) {
      case 'enable':
      case 'disable':
        command = {
          kind: 'protection',
          enabled: action.value === 'enable',
          pin: pin.value,
          simToken,
        }
        break
      case 'change':
        command = { kind: 'change', oldPin: pin.value, newPin: newPin.value, simToken }
        break
      case 'pin':
        command = { kind: 'unlock', pin: pin.value, simToken }
        break
      case 'puk':
        command = { kind: 'unblock', puk: pin.value, newPin: newPin.value, simToken }
        break
    }
    busy.value = true
    errorCode.value = ''
    acknowledged.value = false
    cancelRead()
    let succeeded = false
    try {
      await api.submit(id, command)
      if (page !== revision) return false
      acknowledged.value = true
      succeeded = true
    } catch (error) {
      if (page !== revision) return false
      errorCode.value = problemCode(error)
      acknowledged.value = errorCode.value === 'enable_modem_after_unlock_failed'
    } finally {
      if (page === revision) {
        clearCredentials()
        status.value = null
        await refresh(true)
        if (page === revision) {
          retainFailedAction(submittedAction, simToken)
          busy.value = false
        }
      }
    }
    return page === revision && succeeded
  }

  watch(
    [modemId, simIdentity],
    () => {
      revision++
      cancelRead()
      resetForm(null)
      open.value = false
      status.value = null
      legacy.value = false
      available.value = false
      busy.value = false
      errorCode.value = ''
      acknowledged.value = false
      if (modemId.value && modemId.value !== 'unknown') void refresh()
    },
    // Invalidate credentials before any request continuation can observe a new target.
    { immediate: true, flush: 'sync' },
  )
  onScopeDispose(() => {
    revision++
    cancelRead()
    clearCredentials()
  })

  return {
    status: readonly(status),
    legacy: readonly(legacy),
    open: readonly(open),
    loading: readonly(loading),
    busy: readonly(busy),
    action: readonly(action),
    pin,
    newPin,
    confirmation,
    errorCode: readonly(errorCode),
    acknowledged: readonly(acknowledged),
    available: readonly(available),
    needsNewPin,
    canSubmit,
    choose,
    refresh,
    show,
    close,
    submit,
  }
}
