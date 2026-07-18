import { computed, ref } from 'vue'

import { useWebPushApi } from '@/apis/webPush'
import {
  currentPushSubscription,
  defaultDeviceLabel,
  devicePlatform,
  hasWebPushSubscription,
  notificationPermission,
  pushSubscriptionMatchesVAPIDKey,
  subscribeToWebPush,
  unsubscribeFromWebPush,
  webPushSupportReason,
} from '@/lib/webPush'
import type { WebPushOverviewResponse, WebPushSubscriptionResponse } from '@/types/webPush'

const overview = ref<WebPushOverviewResponse | null>(null)
const currentEndpoint = ref('')
const currentSubscriptionNeedsRenewal = ref(false)
const isLoading = ref(false)
const isUpdating = ref(false)
const errorMessage = ref('')
const supportReason = ref(webPushSupportReason())
const permission = ref(notificationPermission())

export const useWebPush = () => {
  const api = useWebPushApi()
  const subscriptions = computed(() => overview.value?.subscriptions ?? [])
  const enabled = computed(() => overview.value?.enabled ?? true)
  const currentSubscription = computed(() =>
    currentSubscriptionNeedsRenewal.value
      ? null
      : (subscriptions.value.find((item) => item.endpoint === currentEndpoint.value) ?? null),
  )

  const load = async () => {
    if (isLoading.value) return
    isLoading.value = true
    errorMessage.value = ''
    supportReason.value = webPushSupportReason()
    permission.value = notificationPermission()
    try {
      const [response, browserSubscription] = await Promise.all([
        api.getOverview(),
        currentPushSubscription(),
      ])
      const currentOverview = response.data.value ?? null
      overview.value = currentOverview
      currentEndpoint.value = browserSubscription?.endpoint ?? ''
      const serverSubscription = currentOverview?.subscriptions.find(
        (item) => item.endpoint === currentEndpoint.value,
      )
      const keyMatches = Boolean(
        browserSubscription &&
        currentOverview?.publicKey &&
        pushSubscriptionMatchesVAPIDKey(browserSubscription, currentOverview.publicKey),
      )
      currentSubscriptionNeedsRenewal.value = Boolean(
        browserSubscription && (!serverSubscription || !keyMatches),
      )
      hasWebPushSubscription.value = Boolean(
        browserSubscription && serverSubscription && keyMatches,
      )
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Load Web Push settings failed'
      throw err
    } finally {
      isLoading.value = false
    }
  }

  const setEnabled = async (value: boolean) => {
    isUpdating.value = true
    errorMessage.value = ''
    try {
      const response = await api.updateEnabled(value)
      overview.value = response.data.value ?? overview.value
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Update Web Push failed'
      throw err
    } finally {
      isUpdating.value = false
    }
  }

  const enableCurrentDevice = async () => {
    if (webPushSupportReason() !== 'supported') return
    isUpdating.value = true
    errorMessage.value = ''
    try {
      permission.value = await Notification.requestPermission()
      if (permission.value !== 'granted') return false
      if (!overview.value) await load()
      const publicKey = overview.value?.publicKey
      if (!publicKey) throw new Error('VAPID public key is missing')
      const previousEndpoint = currentEndpoint.value
      const previousServerSubscription = subscriptions.value.find(
        (item) => item.endpoint === previousEndpoint,
      )
      const subscription = await subscribeToWebPush(publicKey, {
        forceRenew:
          currentSubscriptionNeedsRenewal.value ||
          Boolean(previousEndpoint && !previousServerSubscription),
      })
      const serialized = subscription.toJSON()
      if (!serialized.endpoint || !serialized.keys?.p256dh || !serialized.keys.auth) {
        throw new Error('Browser returned an incomplete push subscription')
      }
      await api.registerSubscription({
        endpoint: serialized.endpoint,
        keys: { p256dh: serialized.keys.p256dh, auth: serialized.keys.auth },
        label: defaultDeviceLabel(),
        platform: devicePlatform(),
      })
      currentEndpoint.value = serialized.endpoint
      currentSubscriptionNeedsRenewal.value = false
      if (
        previousServerSubscription &&
        previousServerSubscription.endpoint !== serialized.endpoint
      ) {
        try {
          await api.deleteSubscription(previousServerSubscription.id)
        } catch (err) {
          console.warn('[webPush] remove replaced subscription:', err)
        }
      }
      const response = await api.getOverview()
      overview.value = response.data.value ?? overview.value
      return true
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Enable Web Push failed'
      throw err
    } finally {
      isUpdating.value = false
    }
  }

  const deleteSubscription = async (subscription: WebPushSubscriptionResponse) => {
    isUpdating.value = true
    errorMessage.value = ''
    try {
      await api.deleteSubscription(subscription.id)
      if (subscription.endpoint === currentEndpoint.value) {
        await unsubscribeFromWebPush()
        currentEndpoint.value = ''
        currentSubscriptionNeedsRenewal.value = false
      }
      if (overview.value) {
        overview.value.subscriptions = overview.value.subscriptions.filter(
          (item) => item.id !== subscription.id,
        )
      }
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Delete Web Push device failed'
      throw err
    } finally {
      isUpdating.value = false
    }
  }

  const renameSubscription = async (subscription: WebPushSubscriptionResponse, label: string) => {
    isUpdating.value = true
    errorMessage.value = ''
    try {
      const response = await api.renameSubscription(subscription.id, label)
      const updated = response.data.value
      if (updated && overview.value) {
        overview.value.subscriptions = overview.value.subscriptions.map((item) =>
          item.id === updated.id ? updated : item,
        )
      }
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : 'Rename Web Push device failed'
      throw err
    } finally {
      isUpdating.value = false
    }
  }

  return {
    overview,
    subscriptions,
    currentSubscription,
    enabled,
    supportReason,
    permission,
    isLoading,
    isUpdating,
    errorMessage,
    load,
    setEnabled,
    enableCurrentDevice,
    deleteSubscription,
    renameSubscription,
  }
}
