import { Ban, MessageSquare, SignalHigh, SignalLow, SignalMedium, SignalZero, type LucideIcon } from 'lucide-vue-next'

export const useModemDisplay = () => {
  /**
   * Get signal icon based on quality percentage (0-100)
   */
  const signalIcon = (percentage: number) => {
    if (percentage === 0) return SignalHigh
    if (percentage >= 70) return SignalHigh
    if (percentage >= 50) return SignalMedium
    if (percentage >= 30) return SignalLow
    return SignalZero
  }

  /**
   * Get signal color class based on quality percentage (0-100)
   */
  const signalTone = (percentage: number) => {
    if (percentage === 0) return 'text-muted-foreground'
    if (percentage >= 70) return 'text-emerald-500'
    if (percentage >= 50) return 'text-lime-500'
    if (percentage >= 30) return 'text-amber-500'
    return 'text-rose-500'
  }

  /**
   * Format signal quality as percentage
   */
  const formatSignal = (percentage: number) => `${Math.round(percentage)}%`

  /**
   * Get registration state icon based on state
   */
  const registrationStateIcon = (state: string): LucideIcon | null => {
    const normalized = state.trim()
    if (normalized === 'Denied') return Ban
    if (normalized.includes('SMS Only')) return MessageSquare
    return null
  }

  /**
   * Get registration state label (first letter)
   */
  const registrationStateLabel = (state: string): string | null => {
    const normalized = state.trim()
    if (normalized === 'Roaming' || normalized.includes('Roaming')) return 'R'
    return null
  }

  /**
   * Get registration state color class
   */
  const registrationStateTone = (state: string): string => {
    const normalized = state.trim()
    if (normalized === 'Denied') return 'text-rose-500'
    if (normalized.includes('SMS Only')) return 'text-blue-500'
    if (normalized === 'Roaming' || normalized.includes('Roaming')) return 'text-amber-500'
    return 'text-muted-foreground'
  }

  /**
   * Get signal color override based on registration state
   */
  const getSignalColorOverride = (state: string): string | null => {
    const normalized = state.trim()
    if (normalized === 'Emergency Only') return 'text-rose-500'
    if (normalized === 'Unknown' || normalized === 'Idle' || !normalized) {
      return 'text-muted-foreground'
    }
    return null
  }

  /**
   * Check if registration state should show icon
   */
  const shouldShowRegistrationIcon = (state: string): boolean => {
    const normalized = state.trim()
    return (
      normalized === 'Denied' ||
      normalized.includes('SMS Only') ||
      normalized === 'Roaming' ||
      normalized.includes('Roaming')
    )
  }

  return {
    formatSignal,
    signalIcon,
    signalTone,
    registrationStateIcon,
    registrationStateLabel,
    registrationStateTone,
    shouldShowRegistrationIcon,
    getSignalColorOverride,
  }
}
