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

export type CellInfoResponse = {
  type: string
  typeValue: number
  serving: boolean
  operatorId?: string
  lac?: string
  tac?: string
  cellId?: string
  physicalCellId?: string
  arfcn?: number
  uarfcn?: number
  earfcn?: number
  nrarfcn?: number
  rsrp?: number
  rsrq?: number
  sinr?: number
  timingAdvance?: number
  bandwidth?: number
  servingCellType?: number
}

export type CellInfoListResponse = CellInfoResponse[]
