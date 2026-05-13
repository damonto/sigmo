export type SasUp = {
  name: string
  region?: string
}

export type EuiccApiResponse = {
  eid: string
  freeSpace: number
  sasUp: SasUp
  certificates: string[]
}

export type EuiccDetailResponse = EuiccApiResponse
export type EuiccResponse = EuiccApiResponse
