import { computed, ref, shallowRef } from 'vue'

import { useUpdateApi } from '@/apis/update'
import { isApiError } from '@/lib/apiProblem'
import type { UpdateSnapshot } from '@/types/update'

type UpdatePhase = 'idle' | 'starting' | 'monitoring' | 'reconnecting' | 'reloading'

/** AppUpdateFailure describes why monitoring stopped, independently of the server's update state. */
export type AppUpdateFailure = { kind: 'timeout' } | { kind: 'request'; error: unknown }

const REQUEST_TIMEOUT = 10_000
const RECOVERY_TIMEOUT = 180_000
const api = useUpdateApi()
const snapshot = shallowRef<UpdateSnapshot>()
const failure = shallowRef<AppUpdateFailure>()
const phase = ref<UpdatePhase>('idle')
let revision = 0
let retryDelay = 1000
let pollTimer: ReturnType<typeof setTimeout> | undefined
let recoveryTimer: ReturnType<typeof setTimeout> | undefined
let request: { controller: AbortController; timer: ReturnType<typeof setTimeout> } | undefined

const isInstallationRunning = (value: UpdateSnapshot) =>
  ['downloading', 'verifying', 'restarting'].includes(value.state)

const canRetry = (error: unknown) => {
  if (isApiError(error)) return [502, 503, 504].includes(error.status)
  return (
    error instanceof TypeError ||
    (error instanceof DOMException && ['AbortError', 'TimeoutError'].includes(error.name))
  )
}

const clearRecoveryTimeout = () => {
  clearTimeout(recoveryTimer)
  recoveryTimer = undefined
}

const stopMonitoring = (nextPhase: UpdatePhase = 'idle') => {
  revision++
  clearTimeout(pollTimer)
  pollTimer = undefined
  clearRecoveryTimeout()
  if (request) {
    clearTimeout(request.timer)
    request.controller.abort()
    request = undefined
  }
  phase.value = nextPhase
}

const fail = (value: AppUpdateFailure) => {
  stopMonitoring()
  failure.value = value
}

const startRecoveryTimeout = () => {
  if (recoveryTimer !== undefined) return
  recoveryTimer = setTimeout(() => fail({ kind: 'timeout' }), RECOVERY_TIMEOUT)
}

const withRequestTimeout = async <T>(perform: (signal: AbortSignal) => Promise<T>): Promise<T> => {
  const controller = new AbortController()
  const pending = {
    controller,
    timer: setTimeout(
      () => controller.abort(new DOMException('update request timed out', 'TimeoutError')),
      REQUEST_TIMEOUT,
    ),
  }
  request = pending
  try {
    return await perform(controller.signal)
  } finally {
    clearTimeout(pending.timer)
    if (request === pending) request = undefined
  }
}

const schedulePoll = (run: number, delay: number) => {
  pollTimer = setTimeout(() => void poll(run), delay)
}

const acceptSnapshot = (value: UpdateSnapshot, run: number) => {
  snapshot.value = value
  if (value.state === 'idle') {
    // Keep background requests paused until the new client has loaded.
    stopMonitoring('reloading')
    window.location.reload()
    return
  }
  if (!isInstallationRunning(value)) {
    stopMonitoring()
    return
  }

  phase.value = 'monitoring'
  retryDelay = 1000
  if (value.state === 'restarting') startRecoveryTimeout()
  else clearRecoveryTimeout()
  schedulePoll(run, 1000)
}

const poll = async (run: number) => {
  pollTimer = undefined
  try {
    const value = await withRequestTimeout((signal) => api.installation(signal))
    if (run !== revision) return
    acceptSnapshot(value, run)
  } catch (error) {
    if (run !== revision) return
    if (!canRetry(error)) {
      fail({ kind: 'request', error })
      return
    }
    // A transport failure does not prove that the server has started restarting.
    phase.value = 'reconnecting'
    startRecoveryTimeout()
    schedulePoll(run, retryDelay)
    retryDelay = Math.min(retryDelay * 2, 5000)
  }
}

const observe = (value: UpdateSnapshot) => {
  if (phase.value !== 'idle') return
  snapshot.value = value
  failure.value = undefined
  if (!isInstallationRunning(value)) return

  phase.value = 'monitoring'
  retryDelay = 1000
  if (value.state === 'restarting') startRecoveryTimeout()
  schedulePoll(++revision, 500)
}

const install = async () => {
  if (phase.value !== 'idle') return
  phase.value = 'starting'
  failure.value = undefined
  const run = ++revision
  try {
    const { data } = await withRequestTimeout((signal) => api.install(signal))
    if (run !== revision) return
    if (!data.value) throw new Error('update installation returned an empty response')
    acceptSnapshot(data.value, run)
  } catch (error) {
    if (run !== revision) return
    stopMonitoring()
    // Installation is a user action; its API errors retain normal notifications.
    throw error
  }
}

const dispose = () => {
  stopMonitoring()
  snapshot.value = undefined
  failure.value = undefined
}

const appUpdate = {
  snapshot: computed(() => snapshot.value),
  failure: computed(() => failure.value),
  updating: computed(() => phase.value !== 'idle'),
  reconnecting: computed(() => phase.value === 'reconnecting'),
  observe,
  install,
  dispose,
}

/**
 * useAppUpdate shares update monitoring across routes. The application root owns
 * disposal; individual pages must not stop a backend installation's observer.
 */
export const useAppUpdate = (): typeof appUpdate => appUpdate
