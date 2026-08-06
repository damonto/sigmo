export type NetworkResponse = {
  status: string
  operatorName: string
  operatorShortName: string
  operatorCode: string
  accessTechnologies: string[]
}

export type NetworksResponse = NetworkResponse[]

export type NetworkScanStatus = 'running' | 'completed' | 'failed' | 'canceled'

export type NetworkScanResponse = {
  id: string
  status: NetworkScanStatus
  networks?: NetworksResponse
  errorCode?: string
}

export type ModeResponse = {
  allowed: number
  preferred: number
  allowedLabel: string
  preferredLabel: string
  current: boolean
}

export type ModesResponse = {
  supported: ModeResponse[]
  current: ModeResponse
}

export type SetCurrentModesRequest = {
  allowed: number
  preferred: number
}

export type BandResponse = {
  value: BandValue
  label: string
  current: boolean
}

export type BandValue = {
  technology: number
  number: number
}

export type BandsResponse = {
  supported: BandResponse[]
  current: BandValue[]
}

export type SetCurrentBandsRequest = {
  bands: BandValue[]
}

export type AirplaneModeResponse = {
  supported: boolean
  enabled: boolean
}

export type SetAirplaneModeRequest = {
  enabled: boolean
}
