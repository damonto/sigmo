export type NetworkResponse = {
  status: string
  operatorName: string
  operatorShortName: string
  operatorCode: string
  accessTechnologies: string[]
}

export type NetworksResponse = NetworkResponse[]

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
  value: number
  label: string
  current: boolean
}

export type BandsResponse = {
  supported: BandResponse[]
  current: number[]
}

export type SetCurrentBandsRequest = {
  bands: number[]
}
