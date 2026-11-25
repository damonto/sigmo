import type { ConnectionProgress } from '@/types/connectionProgress'

export type VoLTEQMIDataPath = 'qmap' | 'legacy_bam_dmux' | 'qualcomm_410'
export type VoLTEDataPath = 'mbim' | VoLTEQMIDataPath

export type VoLTESettingsResponse = {
  progress?: ConnectionProgress
  enabled: boolean
  connected: boolean
  state: string
  durationSeconds: number
  modemRegistered: boolean
  dataPath: VoLTEDataPath
}

export type UpdateVoLTESettingsRequest = {
  enabled: boolean
  dataPath?: VoLTEQMIDataPath
}
