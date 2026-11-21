import { onMounted, onUnmounted, ref, watch, type Ref } from 'vue'

import { useModemApi } from '@/apis/modem'
import type { WiFiProxy, WiFiProxyCheck, WiFiProxySettings } from '@/types/modem'

export const useWiFiCallingProxies = (modemId: string, savedProxyId: Ref<string | undefined>) => {
  const api = useModemApi()
  const proxies = ref<WiFiProxy[]>([])
  const selected = ref(savedProxyId.value ?? '')
  const result = ref<WiFiProxyCheck>()
  const loading = ref(true)
  const saving = ref(false)
  const checking = ref(false)
  const failure = ref<'listFailed' | 'saveFailed' | 'checkFailed'>()
  let checkRequest: AbortController | undefined

  watch(savedProxyId, (id, previous) => {
    if (selected.value === (previous ?? '')) selected.value = id ?? ''
  })

  const clearCheck = () => {
    checkRequest?.abort()
    checkRequest = undefined
    checking.value = false
    result.value = undefined
    if (failure.value === 'checkFailed') failure.value = undefined
  }
  watch(selected, clearCheck, { flush: 'sync' })
  onUnmounted(clearCheck)

  const load = async () => {
    loading.value = true
    failure.value = undefined
    try {
      const { data } = await api.listWiFiProxies()
      proxies.value = data.value ?? []
    } catch {
      failure.value = 'listFailed'
    } finally {
      loading.value = false
    }
  }
  onMounted(load)

  const save = async (id: string, settings: WiFiProxySettings) => {
    saving.value = true
    failure.value = undefined
    try {
      const { data } = await api.saveWiFiProxy(id, settings)
      const proxy = data.value
      if (!proxy) throw new Error('Missing saved Wi-Fi Calling proxy')
      proxies.value = [...proxies.value.filter((entry) => entry.id !== proxy.id), proxy]
      clearCheck()
      selected.value = proxy.id
      return true
    } catch {
      failure.value = 'saveFailed'
      return false
    } finally {
      saving.value = false
    }
  }

  const check = async () => {
    const id = selected.value
    clearCheck()
    const request = new AbortController()
    checkRequest = request
    checking.value = true
    try {
      const { data } = await api.checkWiFiProxy(modemId, id, request.signal)
      if (checkRequest === request) result.value = data.value ?? undefined
    } catch {
      if (checkRequest === request) failure.value = 'checkFailed'
    } finally {
      if (checkRequest === request) {
        checkRequest = undefined
        checking.value = false
      }
    }
  }

  return { proxies, selected, result, loading, saving, checking, failure, load, save, check }
}
