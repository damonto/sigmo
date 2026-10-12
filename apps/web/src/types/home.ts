import type { SIMKind } from '@/types/modem'

export type HomeModemItem = {
  id: string
  name: string
  regionCode: string
  operatorName: string
  registeredOperatorName: string
  registeredOperatorCode: string
  registrationState: string
  accessTechnology: string | null
  simKind: SIMKind
  number: string
  signalQuality: number
  airplaneMode: boolean
  wifiCallingConnected: boolean
}
