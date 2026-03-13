export type EsimProfileApiResponse = {
  name: string
  serviceProviderName: string
  iccid: string
  icon: string
  profileState: number
  regionCode?: string
}

export type EsimProfilesResponse = EsimProfileApiResponse[]

export type EsimDiscoverItem = {
  eventId: string
  address: string
}

export type EsimDiscoverResponse = EsimDiscoverItem[]

export type EsimProfile = {
  id: string
  name: string
  iccid: string
  enabled: boolean
  regionCode: string
  logoUrl?: string
}
