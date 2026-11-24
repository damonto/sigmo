import { z } from 'zod'

export type InternetConnectionStatus = 'connected' | 'disconnected'

const trafficSampleSchema = z.object({
  sessionId: z.string().min(1),
  elapsedMilliseconds: z.int().nonnegative(),
  rxBytes: z.int().nonnegative(),
  txBytes: z.int().nonnegative(),
})

const trafficResponseSchema = z.object({
  connected: z.boolean(),
  sample: trafficSampleSchema.nullable(),
})

/** Byte totals and sample time for one Sigmo-managed connection. */
export type InternetTrafficSample = z.infer<typeof trafficSampleSchema>
export type InternetTrafficResponse = z.infer<typeof trafficResponseSchema>

/** Validates the counters before they are used to calculate rates and chart coordinates. */
export const parseInternetTrafficResponse = (value: unknown): InternetTrafficResponse =>
  trafficResponseSchema.parse(value)

export type InternetConnectionResponse = {
  status: InternetConnectionStatus
  apn: string
  ipType: string
  apnUsername: string
  apnPassword: string
  apnAuth: string
  defaultRoute: boolean
  proxyEnabled: boolean
  alwaysOn: boolean
  proxy: InternetProxy
  interfaceName?: string
  bearer: number
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

export type InternetProxy = {
  enabled: boolean
  username?: string
  password?: string
  httpAddress?: string
  socks5Address?: string
}

export type ConnectInternetPayload = {
  apn: string
  ipType: string
  apnUsername: string
  apnPassword: string
  apnAuth: string
  defaultRoute: boolean
  proxyEnabled: boolean
  alwaysOn: boolean
}

export type InternetConnectionPreferencesPayload = Pick<
  ConnectInternetPayload,
  'defaultRoute' | 'proxyEnabled' | 'alwaysOn'
>
