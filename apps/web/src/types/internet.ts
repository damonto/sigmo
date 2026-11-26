import { z } from 'zod'

export type InternetConnectionStatus = 'connected' | 'disconnected'

const trafficSampleSchema = z.object({
  elapsedMilliseconds: z.int().nonnegative(),
  rxBytes: z.int().nonnegative(),
  txBytes: z.int().nonnegative(),
})

const trafficResponseSchema = z
  .object({ connected: z.boolean(), sessionId: z.string(), sample: trafficSampleSchema.nullable() })
  .refine(
    ({ connected, sessionId, sample }) =>
      connected ? sessionId !== '' : sessionId === '' && sample === null,
    'Traffic samples require an active connection session',
  )

/** Byte totals and sample time for one Sigmo-managed connection. */
export type InternetTrafficSample = z.infer<typeof trafficSampleSchema>
export type InternetTrafficResponse = z.infer<typeof trafficResponseSchema>

/** Validates the counters before they are used to calculate rates and chart coordinates. */
export const parseInternetTrafficResponse = (value: unknown): InternetTrafficResponse =>
  trafficResponseSchema.parse(value)

export type InternetConnectionResponse = {
  status: InternetConnectionStatus
  sessionId: string
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

const publicIPResponseSchema = z.object({
  ip: z
    .string()
    .trim()
    .pipe(z.union([z.literal(''), z.ipv4(), z.ipv6()]))
    .optional(),
  country: z.string().optional(),
  organization: z.string().optional(),
  isp: z.string().optional(),
})

/** Public address and associated provider of the modem connection. */
export type InternetPublicResponse = z.infer<typeof publicIPResponseSchema>

/** Validates optional provider fields before they are used in the egress card. */
export const parseInternetPublicResponse = (value: unknown): InternetPublicResponse =>
  publicIPResponseSchema.parse(value)

const speedTestResultSchema = z.object({
  downloadBps: z.int().positive(),
  uploadBps: z.int().positive(),
  testedAt: z.iso.datetime(),
})

const speedTestResponseSchema = z.object({
  iccid: z.string(),
  result: speedTestResultSchema.nullable(),
})

/** Supported total payload sizes in decimal MB, starting with the default. */
export const INTERNET_SPEED_TEST_SIZES = [10, 25, 50, 100] as const
export type InternetSpeedTestSize = (typeof INTERNET_SPEED_TEST_SIZES)[number]

/** Last successful measurements for the active SIM. */
export type InternetSpeedTestResult = z.infer<typeof speedTestResultSchema>
export type InternetSpeedTestResponse = z.infer<typeof speedTestResponseSchema>

/** Validates rates and the completion time before rendering a saved test. */
export const parseInternetSpeedTestResponse = (value: unknown): InternetSpeedTestResponse =>
  speedTestResponseSchema.parse(value)

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
