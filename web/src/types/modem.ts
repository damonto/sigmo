import type { CarrierWebsheetInfo } from '@/types/websheet'
import type { Reminder } from '@/types/reminder'

export type SlotInfo = {
  active: boolean
  operatorName: string
  operatorIdentifier: string
  regionCode: string
  identifier: string
  reminder?: Reminder
}

export type RegisteredOperator = {
  name: string
  code: string
}

export type ModemApiResponse = {
  manufacturer: string
  id: string
  firmwareRevision: string
  hardwareRevision: string
  name: string
  number: string
  state: string
  unlockRequired: string
  unlockSupported: boolean
  sim: SlotInfo
  slots: SlotInfo[]
  accessTechnology: string | null
  registrationState: string
  registeredOperator: RegisteredOperator
  signalQuality: number
  airplaneMode: boolean
  supportsEsim: boolean
  wifiCallingEnabled?: boolean
  wifiCallingPreferred?: boolean
  wifiCallingConnected?: boolean
}

export type ModemListResponse = ModemApiResponse[]
export type ModemDetailResponse = ModemApiResponse

export type Modem = ModemApiResponse

export type ModemSettings = {
  alias: string
  mss: number
}

export type ModemSettingsResponse = ModemSettings

export type WiFiCallingSettings = {
  enabled: boolean
  preferred: boolean
}

export type WiFiCallingSettingsResponse = WiFiCallingSettings & {
  connected: boolean
  state: string
  durationSeconds: number
  emergencyAddressUpdateAvailable: boolean
  websheet?: CarrierWebsheetInfo
}

export type WiFiCallingWebsheetResponse = CarrierWebsheetInfo

export type WiFiCallingEmergencyAddressWebsheetResponse = CarrierWebsheetInfo
