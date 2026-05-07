export type ConfigApp = {
  environment: string
  listenAddress: string
  authProviders: string[]
  otpRequired: boolean
}

export type ConfigProxy = {
  listenAddress: string
  httpPort: number
  socks5Port: number
  password: string
}

export type ConfigChannel = {
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

export type ConfigValues = {
  app: ConfigApp
  proxy: ConfigProxy
  channels: Record<string, ConfigChannel>
}

export type ConfigFieldControl =
  | 'text'
  | 'password'
  | 'number'
  | 'switch'
  | 'select'
  | 'list'
  | 'keyValue'
  | 'channelList'

export type ConfigOption = {
  label: string
  value: string
}

export type ConfigField = {
  key: string
  label: string
  description?: string
  control: ConfigFieldControl
  required?: boolean
  secret?: boolean
  placeholder?: string
  min?: number
  max?: number
  options?: ConfigOption[]
}

export type ConfigChannelSchema = {
  key: string
  label: string
  description?: string
  fields: ConfigField[]
}

export type ConfigSchema = {
  app: ConfigField[]
  proxy: ConfigField[]
  channels: ConfigChannelSchema[]
}

export type ConfigResponse = {
  path: string
  schema: ConfigSchema
  values: ConfigValues
  restartRequiredFields?: string[]
}
