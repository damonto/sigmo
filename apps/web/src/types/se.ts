export type SasUp = {
  name: string
  region?: string
}

export type SEItem = {
  id: string
  label: string
  aid?: string
  eid?: string
  freeSpace?: number
  sasUp?: SasUp
  certificates?: string[]
}

export type SEsResponse = {
  ses: SEItem[]
}
