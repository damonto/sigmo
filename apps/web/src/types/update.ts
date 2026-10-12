import type { AppInfoResponse } from './app'
import type { Licensee } from './license'

export type UpdateChannel = 'stable' | 'dev'

export type UpdateManifest = {
  schemaVersion: number
  edition: 'community' | 'pro'
  channel: UpdateChannel
  version: string
  commit: string
  publishedAt: string
  notes: string
}

export type UpdateSettings = {
  automatic: boolean
  channel: UpdateChannel
}

export type UpdateSnapshot = {
  settings: UpdateSettings
  current: AppInfoResponse
  latest?: UpdateManifest
  license?: Licensee
  state: 'idle' | 'checking' | 'downloading' | 'verifying' | 'restarting' | 'failed'
  checkedAt?: string
  updateAvailable: boolean
  selfUpdateSupported: boolean
  unsupportedReason?: 'container' | 'developer_build' | 'release_key_missing'
  errorCode?: string
  error?: string
}
