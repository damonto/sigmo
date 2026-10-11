import type { CallRecord } from '@/types/call'

let clientId: string | undefined
let preparation: Promise<void> | undefined
const STORAGE_KEY = 'sigmo-call-client'

const newClientId = () =>
  crypto.randomUUID?.() ??
  Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('')

const remember = (id: string) => {
  clientId = id
  try {
    sessionStorage.setItem(STORAGE_KEY, id)
  } catch {
    /* Calls also work when browser storage is disabled. */
  }
  return id
}

/** Claim the tab's saved identity before mounting any call controls. */
export function prepareCallClient(): Promise<void> {
  if (preparation) return preparation
  preparation = new Promise<void>((ready) => {
    if (clientId) {
      ready()
      return
    }
    const locks = navigator.locks
    if (!locks) {
      // Without cross-document locks, a cloned sessionStorage is not evidence
      // that this document owns the earlier tab's call.
      remember(newClientId())
      ready()
      return
    }
    let saved: string | null = null
    try {
      saved = sessionStorage.getItem(STORAGE_KEY)
    } catch {
      /* Use a new identity. */
    }
    const claim = (id: string) => {
      void locks
        .request(`sigmo-call-client:${id}`, { ifAvailable: true }, (lock) => {
          if (!lock) {
            claim(newClientId())
            return
          }
          remember(id)
          ready()
          // The browser releases this lock when the document is destroyed. A
          // reload can reclaim it; duplicating a live tab must use a new owner.
          return new Promise<void>(() => {})
        })
        .catch(() => {
          remember(newClientId())
          ready()
        })
    }
    claim(saved && /^[A-Za-z0-9_-]{1,128}$/.test(saved) ? saved : newClientId())
  })
  return preparation
}

/** Socket reconnects within one document always keep the same media owner. */
export function callClientId(): string {
  return clientId ?? remember(newClientId())
}

/** Absent ownership metadata retains compatibility with older backends. */
export function ownsCall(call: CallRecord): boolean {
  return call.ownerClientId === undefined || call.ownerClientId === callClientId()
}

export function callInUseElsewhere(call: CallRecord): boolean {
  return !!call.ownerClientId && !ownsCall(call)
}
