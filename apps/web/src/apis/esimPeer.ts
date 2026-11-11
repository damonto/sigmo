import { fetchJson } from '@/lib/fetch'
import type { EsimTransferRequest, EsimTransferSelection } from '@/types/esimPeer'

/** Submits a decision for the exact request revision shown to the user. */
export const decideEsimTransferRequest = (
  request: EsimTransferRequest,
  accept: boolean,
  selection?: EsimTransferSelection,
) =>
  fetchJson<void>(`esim-transfer-requests/${encodeURIComponent(request.id)}/decisions`, {
    method: 'POST',
    body: JSON.stringify({
      phase: request.phase,
      revision: request.revision,
      accept,
      selection: accept ? selection : undefined,
    }),
  })
