export type SettingsAuth = {
  authProviders: string[]
  otpRequired: boolean
}

export type SettingsProxy = {
  listenAddress: string
  httpPort: number
  socks5Port: number
  password: string
}

export type SettingsChannel = {
  enabled?: boolean
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
  proxy: SettingsProxy
  channels: Record<string, SettingsChannel>
}

export type SettingsFieldControl =
  | 'text'
  | 'password'
  | 'number'
  | 'switch'
  | 'select'
  | 'list'
  | 'keyValue'
  | 'channelList'

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
