/** SettingsAuth is the schema editor's flattened representation of the backend policy. */
export interface SettingsAuth {
  mode: 'none' | 'otp' | 'oidc' | 'cloudflare_access'
  sessionMaxDays: number
  otpChannels: string[]
  oidcIssuer: string
  oidcClientId: string
  oidcClientSecret: string
  oidcAllowedSubjects: string[]
  accessTeamDomain: string
  accessAudience: string
  accessAllowedSubjects: string[]
}

export interface SettingsAuthTest {
  channels: string[]
}

export type SettingsAuthTestStatus = 'pending' | 'succeeded' | 'failed'

export interface SettingsAuthTestResult {
  status: SettingsAuthTestStatus
  reason?: string
  subject?: string
}

export interface SettingsOIDCTest extends SettingsAuthTestResult {
  expiresAt: string
}

export interface SettingsOIDCTestRequest {
  client: 'web' | 'native'
  basePath: string
  state: string
  auth: SettingsAuth
}

export interface SettingsOIDCTestSession {
  id: string
  authorizationUrl: string
  expiresAt: string
}

export interface SettingsNotifications {
  channels: string[]
}

export type SettingsProxy = {
  listenAddress: string
  httpPort: number
  socks5Port: number
  password: string
}

export type SettingsChannel = {
  endpoint?: string
  botToken?: string
  recipients?: string[]
  headers?: Record<string, string>
  smtpHost?: string
  smtpPort?: number
  smtpUsername?: string
  smtpPassword?: string
  from?: string
  tlsPolicy?: string
  ssl?: boolean
  priority?: number
}

export type SettingsValues = {
  auth: SettingsAuth
  notifications: SettingsNotifications
  proxy: SettingsProxy
  channels: Record<string, SettingsChannel>
}

export type SettingsFieldControl =
  'text' | 'password' | 'number' | 'switch' | 'select' | 'list' | 'keyValue' | 'channelList'

export type SettingsOption = {
  label: string
  value: string
}

export type SettingsField = {
  key: string
  label: string
  description?: string
  control: SettingsFieldControl
  required?: boolean
  secret?: boolean
  placeholder?: string
  min?: number
  max?: number
  options?: SettingsOption[]
}

export type SettingsChannelSchema = {
  key: string
  label: string
  description?: string
  fields: SettingsField[]
}

export type SettingsSchema = {
  auth: SettingsField[]
  proxy: SettingsField[]
  channels: SettingsChannelSchema[]
}

export type SettingsResponse = {
  schema: SettingsSchema
  values: SettingsValues
}
