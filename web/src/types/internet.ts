export type InternetConnectionStatus = 'connected' | 'disconnected'

export type InternetConnectionResponse = {
  status: InternetConnectionStatus
  apn: string
  defaultRoute: boolean
  interfaceName?: string
  bearer?: string
  ipv4Addresses: string[]
  ipv6Addresses: string[]
  dns: string[]
  durationSeconds: number
  txBytes: number
  rxBytes: number
  routeMetric: number
}

export type InternetPublicResponse = {
  ip?: string
  country?: string
  organization?: string
}

export type ConnectInternetPayload = {
  apn: string
  defaultRoute: boolean
}
