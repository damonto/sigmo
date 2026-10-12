import { onScopeDispose, ref, watch } from 'vue'
import type { Ref } from 'vue'

import { useEsimApi } from '@/apis/esim'
import { isLocalAuthenticationError } from '@/lib/apiProblem'
import { clearStoredToken, getStoredToken } from '@/lib/authStorage'
import router from '@/router'
import type { Modem } from '@/types/modem'

interface ModemIcon {
  identity: string
  icon?: string
  pending: boolean
}

/** Home-only metadata reads never delay the modem list or its refresh indicator. */
export const useModemIcons = (modems: Readonly<Ref<Modem[]>>) => {
  const api = useEsimApi()
  const icons = ref<Record<string, ModemIcon>>({})
  const requests = new Map<string, AbortController>()
  let sessionToken = getStoredToken()
  let disposed = false

  const clear = () => {
    for (const request of requests.values()) request.abort()
    requests.clear()
    icons.value = {}
  }

  const read = async (modem: Modem, identity: string, request: AbortController) => {
    const token = sessionToken
    let icon: string | undefined
    try {
      const result = await api.getEsimsQuietly(modem.id, request.signal)
      const matches = result.ses
        .flatMap((group) => group.profiles)
        .filter((profile) => profile.iccid === modem.sim.identifier && profile.profileState === 1)
      // An ambiguous multi-SE response must not select another profile's brand.
      if (matches.length === 1) icon = matches[0]?.icon || undefined
    } catch (error) {
      if (
        !request.signal.aborted &&
        token === getStoredToken() &&
        isLocalAuthenticationError(error)
      ) {
        clearStoredToken()
        clear()
        void router.replace({ name: 'auth' })
      }
    } finally {
      if (
        !disposed &&
        !request.signal.aborted &&
        requests.get(modem.id) === request &&
        token === getStoredToken()
      ) {
        icons.value = { ...icons.value, [modem.id]: { identity, icon, pending: false } }
        requests.delete(modem.id)
      }
    }
  }

  const update = () => {
    if (sessionToken !== getStoredToken()) {
      clear()
      sessionToken = getStoredToken()
    }
    const eligible = modems.value.filter(
      (modem) =>
        modem.simKind === 'euicc' && modem.state !== 'locked' && Boolean(modem.sim.identifier),
    )
    const present = new Set(eligible.map((modem) => modem.id))
    for (const id of Object.keys(icons.value)) {
      if (present.has(id)) continue
      requests.get(id)?.abort()
      requests.delete(id)
      delete icons.value[id]
    }
    for (const modem of eligible) {
      const identity = JSON.stringify([modem.sim.slot, modem.sim.identifier])
      if (icons.value[modem.id]?.identity === identity) continue
      requests.get(modem.id)?.abort()
      const request = new AbortController()
      requests.set(modem.id, request)
      icons.value[modem.id] = { identity, pending: true }
      void read(modem, identity, request)
    }
  }

  watch(modems, update, { immediate: true })
  onScopeDispose(() => {
    disposed = true
    clear()
  })

  const refreshIcons = () => {
    clear()
    update()
  }
  return { icons, refreshIcons }
}
