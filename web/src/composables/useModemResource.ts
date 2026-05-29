import { computed, ref, shallowRef, watch, type ComputedRef, type Ref } from 'vue'

import { useModemApi } from '@/apis/modem'
import type { Modem } from '@/types/modem'

type ModemResourceState = {
  modem: Ref<Modem | null>
  isLoading: Ref<boolean>
  error: Ref<string | null>
  load: () => Promise<void>
  refresh: () => Promise<void>
}

const resources = new Map<string, ModemResourceState>()

const usableModemID = (value: string) => {
  const id = value.trim()
  return id && id !== 'unknown' ? id : ''
}

const createResource = (id: string, modemApi: ReturnType<typeof useModemApi>): ModemResourceState => {
  const modem = ref<Modem | null>(null)
  const isLoading = ref(false)
  const error = ref<string | null>(null)
  let loaded = false
  let request: Promise<void> | null = null

  const loadWith = (force: boolean) => {
    if (request) return request
    if (loaded && !force) return Promise.resolve()

    isLoading.value = true
    error.value = null
    request = (async () => {
      try {
        const { data } = await modemApi.getModem(id)
        modem.value = data.value ?? null
        loaded = true
      } catch (err) {
        modem.value = null
        loaded = false
        error.value = err instanceof Error ? err.message : 'Load modem failed'
        console.warn('[useModemResource] load modem:', err)
      } finally {
        isLoading.value = false
        request = null
      }
    })()
    return request
  }

  return {
    modem,
    isLoading,
    error,
    load: () => loadWith(false),
    refresh: () => loadWith(true),
  }
}

const resourceFor = (id: string, modemApi: ReturnType<typeof useModemApi>) => {
  const existing = resources.get(id)
  if (existing) return existing

  const resource = createResource(id, modemApi)
  resources.set(id, resource)
  return resource
}

export const useModemResource = (modemId: ComputedRef<string>) => {
  const modemApi = useModemApi()
  const current = shallowRef<ModemResourceState | null>(null)

  watch(
    modemId,
    (value) => {
      const id = usableModemID(value)
      if (!id) {
        current.value = null
        return
      }

      current.value = resourceFor(id, modemApi)
      void current.value.load()
    },
    { immediate: true, flush: 'sync' },
  )

  const refresh = async () => {
    await current.value?.refresh()
  }

  return {
    modem: computed(() => current.value?.modem.value ?? null),
    isLoading: computed(() => current.value?.isLoading.value ?? false),
    error: computed(() => current.value?.error.value ?? null),
    refresh,
  }
}
