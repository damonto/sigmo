import { z } from 'zod'

import { fetchJson } from '@/lib/fetch'
import type { SimPinCommand, SimPinStatus } from '@/types/simPin'

const statusSchema = z.object({
  supported: z.boolean(),
  enabled: z.boolean().nullable(),
  lock: z.enum(['none', 'pin', 'puk', 'permanently_blocked', 'other', 'unknown']).catch('unknown'),
  pinRetries: z.number().int().min(0).max(0xffffffff).nullable(),
  pukRetries: z.number().int().min(0).max(0xffffffff).nullable(),
  simToken: z.string().min(1),
})

/** SIM credential requests have no retries or global response logging. */
export const useSimPinApi = () => ({
  async status(id: string, signal?: AbortSignal): Promise<SimPinStatus> {
    const { data } = await fetchJson<unknown>(`modems/${id}/sim-pin`, { signal }, true)
    return statusSchema.parse(data.value)
  },
  async submit(id: string, command: SimPinCommand): Promise<void> {
    const { kind, ...body } = command
    const path =
      kind === 'protection'
        ? 'sim-pin/protection'
        : kind === 'change'
          ? 'sim-pin-changes'
          : 'sim-unlocks'
    await fetchJson<void>(
      `modems/${id}/${path}`,
      {
        method: kind === 'protection' ? 'PUT' : 'POST',
        body: JSON.stringify(body),
      },
      true,
    )
  },
})
