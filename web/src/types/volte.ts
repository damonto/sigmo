export type VoLTESettingsResponse = {
  enabled: boolean
  connected: boolean
  state: string
  durationSeconds: number
  modemRegistered: boolean
}

export type UpdateVoLTESettingsRequest = {
  enabled: boolean
}
