export type SlotInfo = {
  active: boolean
  operatorName: string
  operatorIdentifier: string
  regionCode: string
  identifier: string
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
  sim: SlotInfo
  slots: SlotInfo[]
  accessTechnology: string | null
  registrationState: string
  registeredOperator: RegisteredOperator
  signalQuality: number
  supportsEsim: boolean
}

export type ModemListResponse = ModemApiResponse[]
export type ModemDetailResponse = ModemApiResponse

export type Modem = ModemApiResponse

export type ModemSettings = {
  alias: string
  compatible: boolean
  mss: number
}

export type ModemSettingsResponse = ModemSettings
