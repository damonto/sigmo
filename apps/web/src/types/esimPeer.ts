import { z } from 'zod'
import type { EsimTransferProfile, EsimTransferSource } from '@/types/esim'

/** A source and its eSIMs, disclosed only to authenticated source-side clients. */
export interface EsimTransferSourceOption {
  source: EsimTransferSource
  profiles: EsimTransferProfile[]
}
const profileSchema = z.object({
  id: z.string(),
  seId: z.string().optional(),
  type: z.enum(['esim', 'physical']),
  name: z.string(),
  iccid: z.string(),
  enabled: z.boolean(),
  supported: z.boolean(),
  serviceProviderName: z.string().optional(),
  icon: z.string().optional(),
  regionCode: z.string().optional(),
  unsupportedReason: z.string().optional(),
  carrierName: z.string().optional(),
})
const sourceOptionSchema = z.object({
  source: z.object({
    type: z.enum(['modem', 'ccid']),
    id: z.string(),
    name: z.string(),
    detail: z.string().optional(),
    requiresSourceImei: z.boolean(),
  }),
  profiles: z.array(profileSchema),
})

/** The source owner selects one profile for the original connection and target. */
export interface EsimTransferSelection {
  sourceType: 'modem' | 'ccid'
  sourceId: string
  profileId: string
  sourceImei?: string
}

const requestSchema = z.object({
  id: z.string(),
  revision: z.number().int(),
  phase: z.string(),
  remoteAddress: z.string(),
  target: z.object({ name: z.string(), imei: z.string(), eid: z.string() }),
  sources: z.array(sourceOptionSchema).optional(),
  sourceName: z.string().optional(),
  profile: profileSchema.optional(),
  stage: z.string().optional(),
  outcome: z.string().optional(),
  expiresAt: z.string(),
})
/** Incoming request; only the authorization phase accepts a source selection and decision. */
export type EsimTransferRequest = z.infer<typeof requestSchema>
/** Decodes the snapshot used by global approval subscriptions. */
export const parseTransferRequests = (value: unknown): EsimTransferRequest[] =>
  z
    .object({
      type: z.literal('requests'),
      requests: z.array(requestSchema).optional(),
    })
    .parse(value).requests ?? []

/** Fixed schema shared by all events on a transfer connection. */
const transferMessageSchema = z.object({
  type: z.string(),
  version: z.number().optional(),
  sourceProfile: profileSchema.optional(),
  sourceName: z.string().optional(),
  stage: z.string().optional(),
  message: z.string().optional(),
  code: z.string().optional(),
  iccid: z.string().optional(),
  profile: z
    .object({
      iccid: z.string(),
      serviceProviderName: z.string(),
      profileName: z.string(),
      profileNickname: z.string().optional(),
      profileState: z.string(),
      icon: z.string().optional(),
      regionCode: z.string().optional(),
      profileOwner: z.object({
        mcc: z.string(),
        mnc: z.string(),
        gid1: z.string().optional(),
        gid2: z.string().optional(),
      }),
    })
    .optional(),
  input: z
    .object({
      text: z.string(),
      acceptLabel: z.string().optional(),
      rejectLabel: z.string().optional(),
      freeText: z.boolean(),
      freeTextHint: z.string().optional(),
    })
    .optional(),
  websheet: z
    .object({
      id: z.string(),
      embedUrl: z.string(),
      title: z.string().optional(),
      url: z.string(),
      method: z.string(),
    })
    .optional(),
})

/** Validates a message without rebuilding the protocol schema for each event. */
export const parseTransferMessage = (value: unknown) => transferMessageSchema.parse(value)

/** Only a pending request may submit a source-side approval decision. */
export const isTransferApproval = (request: EsimTransferRequest): boolean =>
  request.phase === 'authorization'
