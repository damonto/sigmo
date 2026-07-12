export type VoLTESettingsResponse = {
  enabled: boolean
  connected: boolean
  state: string
  durationSeconds: number
  canEnable: boolean
  modemRegistered: boolean
}

export type UpdateVoLTESettingsRequest = {
  enabled: boolean
}
