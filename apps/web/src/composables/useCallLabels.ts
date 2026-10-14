import { useI18n } from 'vue-i18n'
import type { CallRecord } from '@/types/call'

const liveDurationStates = new Set([
  'dialing',
  'ringing',
  'answering',
  'early_media',
  'active',
  'confirmed',
])

/** Formats history independently of the active audio session and page route. */
export const useCallLabels = (now: () => number = Date.now) => {
  const { t } = useI18n()
  const routeLabel = (value: string) => {
    switch (value) {
      case 'wifi_calling':
        return t('modemDetail.calls.routes.wifiCalling')
      case 'volte':
        return t('modemDetail.calls.routes.volte')
      case 'modem':
        return t('modemDetail.calls.routes.modem')
      default:
        return t('modemDetail.calls.routes.auto')
    }
  }

  const stateLabel = (value: string) => {
    switch (value) {
      case 'dialing':
        return t('modemDetail.calls.states.dialing')
      case 'ringing':
        return t('modemDetail.calls.states.ringing')
      case 'answering':
        return t('modemDetail.calls.states.answering')
      case 'early_media':
        return t('modemDetail.calls.states.earlyMedia')
      case 'active':
        return t('modemDetail.calls.states.active')
      case 'confirmed':
        return t('modemDetail.calls.states.confirmed')
      case 'ending':
        return t('modemDetail.calls.states.ending')
      case 'failed':
        return t('modemDetail.calls.states.failed')
      default:
        return t('modemDetail.calls.states.ended')
    }
  }

  const callStartedAt = (call: CallRecord) => Date.parse(call.answeredAt)

  const callEndedAt = (call: CallRecord) => {
    if (call.endedAt) return Date.parse(call.endedAt)
    if (liveDurationStates.has(call.state)) {
      return now()
    }
    return Date.parse(call.updatedAt)
  }

  const callDurationLabel = (call: CallRecord) => {
    const start = callStartedAt(call)
    if (!Number.isFinite(start)) return ''
    const end = callEndedAt(call)
    if (!Number.isFinite(start) || !Number.isFinite(end) || end < start)
      return t('modemDetail.calls.durationEmpty')
    const seconds = Math.max(0, Math.floor((end - start) / 1000))
    const minutes = Math.floor(seconds / 60)
    const remaining = seconds % 60
    if (minutes >= 60) {
      const hours = Math.floor(minutes / 60)
      const hourMinutes = minutes % 60
      return `${hours}:${String(hourMinutes).padStart(2, '0')}:${String(remaining).padStart(2, '0')}`
    }
    return `${minutes}:${String(remaining).padStart(2, '0')}`
  }

  return { routeLabel, stateLabel, callDurationLabel }
}
