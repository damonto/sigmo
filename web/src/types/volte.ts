export type VoLTEQMIDataPath = 'qmap' | 'legacy_bam_dmux'
export type VoLTEDataPath = 'mbim' | VoLTEQMIDataPath

export type VoLTESettingsResponse = {
  enabled: boolean
  connected: boolean
  state: string
  durationSeconds: number
  modemRegistered: boolean
  dataPath: VoLTEDataPath
  setIMSAPNAsDefault: boolean
  enablePCSCFViaPCO: boolean
}

export type UpdateVoLTESettingsRequest = {
  enabled: boolean
  dataPath?: VoLTEQMIDataPath
  setIMSAPNAsDefault: boolean
  enablePCSCFViaPCO: boolean
}
