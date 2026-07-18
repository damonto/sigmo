/// <reference lib="webworker" />

import {
  isWebPushPayload,
  notificationContent,
  notificationTargetURL,
} from '@/lib/webPushNotification'

declare let self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<{ revision?: string; url: string } | string>
}

const cachePrefix = 'sigmo-precache-'
const foregroundDeliveryTimeout = 1000
const precacheManifest = self.__WB_MANIFEST
const precacheURLs = precacheManifest.map((entry) =>
  typeof entry === 'string' ? entry : entry.url,
)
const precacheURLSet = new Set(
  precacheURLs.map((value) => new URL(value, self.location.origin).href),
)
const cacheName = `${cachePrefix}${hashManifest(precacheManifest)}`

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(cacheName)
      .then((cache) => cache.addAll(precacheURLs))
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((key) => key.startsWith(cachePrefix) && key !== cacheName)
            .map((key) => caches.delete(key)),
        ),
      )
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return

  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request).catch(async () => {
        const cache = await caches.open(cacheName)
        return (await cache.match('/index.html')) ?? (await cache.match('/')) ?? Response.error()
      }),
    )
    return
  }

  if (!precacheURLSet.has(url.href)) return
  event.respondWith(
    caches
      .open(cacheName)
      .then(async (cache) => (await cache.match(request)) ?? fetch(request))
      .catch(() => Response.error()),
  )
})

self.addEventListener('push', (event) => {
  let payload: unknown
  try {
    payload = event.data?.json()
  } catch (err) {
    console.error('[service-worker] parse push payload:', err)
    return
  }
  if (!isWebPushPayload(payload)) return
  event.waitUntil(deliverNotification(payload))
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const data = event.notification.data as { url?: unknown } | undefined
  event.waitUntil(openNotificationURL(data?.url))
})

async function deliverNotification(payload: Parameters<typeof notificationContent>[0]) {
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  const visible = windows.find((client) => client.visibilityState === 'visible')
  if (visible && (await deliverToPage(visible, payload))) return
  const content = notificationContent(payload, self.navigator.language)
  await self.registration.showNotification(content.title, content.options)
}

function deliverToPage(
  client: Client,
  payload: Parameters<typeof notificationContent>[0],
): Promise<boolean> {
  return new Promise((resolve) => {
    const channel = new MessageChannel()
    let timeout: ReturnType<typeof setTimeout> | undefined
    let settled = false
    const finish = (delivered: boolean) => {
      if (settled) return
      settled = true
      if (timeout !== undefined) clearTimeout(timeout)
      channel.port1.close()
      resolve(delivered)
    }
    channel.port1.onmessage = (event) => {
      finish(event.data?.type === 'web-push-rendered')
    }
    channel.port1.onmessageerror = () => finish(false)
    timeout = setTimeout(() => finish(false), foregroundDeliveryTimeout)
    try {
      client.postMessage({ type: 'web-push', payload }, [channel.port2])
    } catch (err) {
      console.warn('[service-worker] deliver push to page:', err)
      finish(false)
    }
  })
}

async function openNotificationURL(path: unknown) {
  const target = notificationTargetURL(path, self.location.origin)
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  const existing = windows[0]
  if (existing) {
    await existing.navigate(target)
    return existing.focus()
  }
  return self.clients.openWindow(target)
}

function hashManifest(entries: typeof precacheManifest) {
  const value = JSON.stringify(entries)
  let hash = 2166136261
  for (let index = 0; index < value.length; index++) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return (hash >>> 0).toString(16)
}

export {}
