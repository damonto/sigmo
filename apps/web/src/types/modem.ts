import type { ConnectionProgress } from '@/types/connectionProgress'
import type { CarrierWebsheetInfo } from '@/types/websheet'
import type { Reminder } from '@/types/reminder'

export type SlotInfo = {
  slot: number
  active: boolean
  operatorName: string
  operatorIdentifier: string
  regionCode: string
  identifier: string
  /** SIM/eSIM display label supplied without a separate profile inventory request. */
  label?: string
  /** Embedded active-profile icon supplied by the modem endpoint. */
  icon?: string
  reminder?: Reminder
}

export type RegisteredOperator = {
  name: string
  code: string
}

export type SIMKind = 'unknown' | 'physical' | 'euicc'

export type ModemApiResponse = {
  manufacturer: string
  id: string
  primaryPort: string
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
  simKind: SIMKind
  internetConnected?: boolean
  wifiCallingEnabled?: boolean
  wifiCallingConnected?: boolean
  volteEnabled?: boolean
  volteConnected?: boolean
}

export type ModemListResponse = ModemApiResponse[]
export type ModemDetailResponse = ModemApiResponse

export type Modem = ModemApiResponse

export type ModemSettings = {
  alias: string
  mss: number
}

export type ModemSettingsResponse = ModemSettings

export type WiFiCallingUnderlayMode = 'system' | 'self' | 'modem' | 'socks5'

export type WiFiDirectUnderlay = {
  mode: Exclude<WiFiCallingUnderlayMode, 'socks5'>
  modemId?: string
}

export type WiFiCallingUnderlay = {
  mode: WiFiCallingUnderlayMode
  modemId?: string
  proxyId?: string
  directUnderlay?: WiFiDirectUnderlay
}

export type WiFiProxySettings = {
  name: string
  host: string
  port: number
  username: string
  password: string
}

export type WiFiProxy = Omit<WiFiProxySettings, 'password'> & {
  id: string
  hasPassword: boolean
}

export type WiFiProxyCheckStep = {
  durationMs: number
  errorCode?: string
  serverAddress?: string
  relayAddress?: string
  ip?: string
  port?: number
  addresses?: string[]
}

export type WiFiProxyCheck = {
  tcp: WiFiProxyCheckStep
  udp: WiFiProxyCheckStep
  dns: WiFiProxyCheckStep
}

export type WiFiCallingSettings = {
  enabled: boolean
  underlay?: WiFiCallingUnderlay
}

export type WiFiCallingSettingsRequest = {
  enabled: boolean
  underlay: WiFiCallingUnderlay
}

export type WiFiCallingSettingsResponse = Omit<WiFiCallingSettings, 'underlay'> & {
  progress?: ConnectionProgress
  lastError?: string
  underlay: WiFiCallingUnderlay
  connected: boolean
  state: string
  durationSeconds: number
  emergencyAddressUpdateAvailable: boolean
  websheet?: CarrierWebsheetInfo
}

export type WiFiCallingWebsheetResponse = CarrierWebsheetInfo

export type WiFiCallingEmergencyAddressWebsheetResponse = CarrierWebsheetInfo
