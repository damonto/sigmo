import type { CarrierWebsheetInfo } from '@/types/websheet'
import type { Reminder } from '@/types/reminder'

export type EsimProfileApiResponse = {
  seId: string
  seLabel: string
  seEid?: string
  name: string
  serviceProviderName: string
  iccid: string
  isdPAID?: string
  icon: string
  profileName: string
  profileNickname?: string
  profileState: number
  profileStateName: string
  profileClass: string
  profileOwner: EsimProfileOwner
  regionCode?: string
  reminder?: Reminder
}

export type EsimProfileGroup = {
  id: string
  label: string
  aid?: string
  eid?: string
  profiles: EsimProfileApiResponse[]
}

export type EsimProfilesResponse = {
  ses: EsimProfileGroup[]
}

export type EsimDiscoverItem = {
  eventId: string
  address: string
}

export type EsimDiscoverResponse = EsimDiscoverItem[]

export type EsimDownloadPreview = {
  iccid: string
  serviceProviderName: string
  profileName: string
  profileNickname?: string
  profileState: string
  profileOwner: EsimProfileOwner
  icon?: string
  regionCode?: string
}

export type EsimProfileOwner = {
  mcc: string
  mnc: string
  gid1?: string
  gid2?: string
}

export type EsimProfile = {
  id: string
  seId: string
  seLabel: string
  seEid?: string
  name: string
  iccid: string
  isdPAID?: string
  enabled: boolean
  serviceProviderName: string
  profileName: string
  profileNickname?: string
  profileStateName: string
  profileClass: string
  profileOwner: EsimProfileOwner
  regionCode: string
  logoUrl?: string
  reminder?: Reminder
}

export type EsimTransferSource = {
  type: 'modem' | 'ccid'
  id: string
  name: string
  detail?: string
  requiresSourceImei: boolean
}

export type EsimTransferSourcesResponse = {
  sources: EsimTransferSource[]
  ccidError?: string
}

export type EsimTransferProfile = {
  id: string
  seId?: string
  type: 'esim' | 'physical'
  name: string
  serviceProviderName?: string
  iccid: string
  icon?: string
  regionCode?: string
  enabled: boolean
  supported: boolean
  unsupportedReason?: string
  carrierName?: string
}

export type EsimTransferWebsheet = CarrierWebsheetInfo
