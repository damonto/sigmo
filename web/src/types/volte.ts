export type VoLTEQMINetworkDriver = 'qmap' | 'legacy_bam_dmux'
export type VoLTENetworkDriver = 'mbim' | VoLTEQMINetworkDriver

export type VoLTESettingsResponse = {
  enabled: boolean
  connected: boolean
  state: string
  durationSeconds: number
  modemRegistered: boolean
  networkDriver: VoLTENetworkDriver
  setIMSAPNAsDefault: boolean
  enablePCSCFViaPCO: boolean
}

export type UpdateVoLTESettingsRequest = {
  enabled: boolean
  networkDriver?: VoLTEQMINetworkDriver
  setIMSAPNAsDefault: boolean
  enablePCSCFViaPCO: boolean
}
