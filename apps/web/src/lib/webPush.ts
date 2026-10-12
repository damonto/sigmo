import { ref } from 'vue'
import { toast } from 'vue-sonner'

import {
  isWebPushPayload,
  notificationContent,
  notificationTargetURL,
} from '@/lib/webPushNotification'

export type WebPushSupportReason = 'supported' | 'insecure' | 'unsupported' | 'ios_setup_required'

export const hasWebPushSubscription = ref(false)

let registrationPromise: Promise<ServiceWorkerRegistration | null> | null = null
let messageListenerInstalled = false

const isIOS = () => {
  const touchMac = navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1
  return /iPad|iPhone|iPod/.test(navigator.userAgent) || touchMac
}

const isStandalone = () => {
  const iosNavigator = navigator as Navigator & { standalone?: boolean }
  return window.matchMedia('(display-mode: standalone)').matches || iosNavigator.standalone === true
}

export const webPushSupportReason = (): WebPushSupportReason => {
  if (!window.isSecureContext) return 'insecure'
  const missingRequiredAPIs =
    !('Notification' in window) || !('serviceWorker' in navigator) || !('PushManager' in window)
  if (isIOS() && (!isStandalone() || missingRequiredAPIs)) return 'ios_setup_required'
  if (missingRequiredAPIs) return 'unsupported'
  return 'supported'
}

export const notificationPermission = (): NotificationPermission =>
  'Notification' in window ? Notification.permission : 'denied'

export const registerWebPushServiceWorker = async () => {
  if (webPushSupportReason() !== 'supported') return null
  registrationPromise ??= navigator.serviceWorker
    .register(import.meta.env.PROD ? '/sw.js' : '/dev-sw.js?dev-sw', {
      scope: '/',
      ...(import.meta.env.DEV ? { type: 'module' as const } : {}),
    })
    .catch((err: unknown) => {
      console.error('[webPush] register service worker:', err)
      return null
    })
  return registrationPromise
}

export const currentPushSubscription = async () => {
  const registration = await registerWebPushServiceWorker()
  if (!registration) return null
  const subscription = await registration.pushManager.getSubscription()
  hasWebPushSubscription.value = subscription !== null
  return subscription
}

export const subscribeToWebPush = async (
  publicKey: string,
  options: { forceRenew?: boolean } = {},
) => {
  const registration = await registerWebPushServiceWorker()
  if (!registration) throw new Error('Web Push is not supported')
  const applicationServerKey = decodeBase64URL(publicKey)
  const existing = await registration.pushManager.getSubscription()
  if (existing) {
    if (
      !options.forceRenew &&
      subscriptionUsesApplicationServerKey(existing, applicationServerKey)
    ) {
      hasWebPushSubscription.value = true
      return existing
    }
    if (!(await existing.unsubscribe())) {
      throw new Error('Existing Web Push subscription could not be renewed')
    }
    hasWebPushSubscription.value = false
  }
  const subscription = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey,
  })
  hasWebPushSubscription.value = true
  return subscription
}

export const unsubscribeFromWebPush = async () => {
  const subscription = await currentPushSubscription()
  if (subscription) await subscription.unsubscribe()
  hasWebPushSubscription.value = false
}

export const decodeBase64URL = (value: string): ArrayBuffer => {
  const padded = value
    .replace(/-/g, '+')
    .replace(/_/g, '/')
    .padEnd(Math.ceil(value.length / 4) * 4, '=')
  const raw = window.atob(padded)
  const bytes = Uint8Array.from(raw, (char) => char.charCodeAt(0))
  return bytes.buffer
}

const applicationServerKeysEqual = (left: ArrayBuffer | null, right: ArrayBuffer) => {
  if (!left) return false
  const leftBytes = new Uint8Array(left)
  const rightBytes = new Uint8Array(right)
  if (leftBytes.length !== rightBytes.length) return false
  return leftBytes.every((value, index) => value === rightBytes[index])
}

const subscriptionUsesApplicationServerKey = (
  subscription: PushSubscription,
  applicationServerKey: ArrayBuffer,
) =>
  applicationServerKeysEqual(
    subscription.options?.applicationServerKey ?? null,
    applicationServerKey,
  )

export const pushSubscriptionMatchesVAPIDKey = (
  subscription: PushSubscription,
  publicKey: string,
) => subscriptionUsesApplicationServerKey(subscription, decodeBase64URL(publicKey))

export const defaultDeviceLabel = () => {
  const ua = navigator.userAgent
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /Firefox\//.test(ua)
      ? 'Firefox'
      : /CriOS\//.test(ua)
        ? 'Chrome'
        : /Chrome\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : 'Browser'
  return `${browser} on ${devicePlatform()}`
}

export const devicePlatform = () => {
  const navigatorWithData = navigator as Navigator & { userAgentData?: { platform?: string } }
  if (navigatorWithData.userAgentData?.platform) return navigatorWithData.userAgentData.platform
  if (isIOS()) return 'iOS'
  if (/Android/.test(navigator.userAgent)) return 'Android'
  return navigator.platform || 'Unknown device'
}

const showForegroundPayload = (payload: unknown) => {
  if (!isWebPushPayload(payload)) return false
  const content = notificationContent(payload, navigator.language)
  toast(content.title, {
    id: `web-push:${payload.tag}`,
    description: content.options.body,
    action: {
      label: content.actionLabel,
      onClick: () =>
        window.location.assign(notificationTargetURL(payload.url, window.location.origin)),
    },
  })
  return true
}

export const bootstrapWebPush = async () => {
  if (webPushSupportReason() !== 'supported') return
  if (!messageListenerInstalled) {
    messageListenerInstalled = true
    navigator.serviceWorker.addEventListener('message', (event) => {
      if (event.data?.type !== 'web-push') return
      if (showForegroundPayload(event.data.payload)) {
        event.ports[0]?.postMessage({ type: 'web-push-rendered' })
      }
    })
  }
  await currentPushSubscription()
}
