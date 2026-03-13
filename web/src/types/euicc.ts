export type EuiccApiResponse = {
  eid: string
  freeSpace: number
  sasUp: string
  certificates: string[]
}

export type EuiccDetailResponse = EuiccApiResponse
export type EuiccResponse = EuiccApiResponse
