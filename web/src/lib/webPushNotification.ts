import type { WebPushPayload } from '@/types/webPush'

export type WebPushNotificationContent = {
  title: string
  options: NotificationOptions
  actionLabel: string
}

export const isWebPushPayload = (value: unknown): value is WebPushPayload => {
  if (!value || typeof value !== 'object') return false
  const payload = value as Partial<WebPushPayload>
  return (
    (payload.type === 'sms' || payload.type === 'call' || payload.type === 'reminder') &&
    typeof payload.id === 'string' &&
    typeof payload.modemId === 'string' &&
    typeof payload.modem === 'string' &&
    (payload.type === 'reminder' || typeof payload.from === 'string') &&
    typeof payload.url === 'string' &&
    typeof payload.tag === 'string'
  )
}

export const notificationContent = (
  payload: WebPushPayload,
  language: string,
): WebPushNotificationContent => {
  const chinese = language.toLowerCase().startsWith('zh')
  const actionLabel = chinese ? '查看' : 'View'
  const modem = payload.modem.trim()
  const from = payload.from?.trim() || (chinese ? '未知号码' : 'Unknown number')

  if (payload.type === 'call') {
    return {
      title: chinese ? '来电' : 'Incoming call',
      actionLabel,
      options: {
        body: modem ? `${from} · ${modem}` : from,
        tag: payload.tag,
        icon: '/icons/icon-192.png',
        data: { url: payload.url, type: payload.type, id: payload.id },
      },
    }
  }

  if (payload.type === 'reminder') {
    const profile =
      payload.profileName?.trim() || payload.profileId?.trim() || (chinese ? 'SIM 卡' : 'SIM')
    const content = payload.text?.trim() || (chinese ? '到期提醒' : 'Scheduled reminder')
    return {
      title: chinese ? `提醒：${profile}` : `Reminder: ${profile}`,
      actionLabel,
      options: {
        body: modem ? `${content} · ${modem}` : content,
        tag: payload.tag,
        icon: '/icons/icon-192.png',
        data: { url: payload.url, type: payload.type, id: payload.id },
      },
    }
  }

  const preview = payload.text?.trim() || (chinese ? '空短信' : 'Empty message')
  return {
    title: chinese ? `来自 ${from} 的新短信` : `New message from ${from}`,
    actionLabel,
    options: {
      body: modem ? `${preview} · ${modem}` : preview,
      tag: payload.tag,
      icon: '/icons/icon-192.png',
      data: { url: payload.url, type: payload.type, id: payload.id },
    },
  }
}

export const notificationTargetURL = (value: unknown, origin: string) => {
  const fallback = new URL('/', origin).href
  if (typeof value !== 'string') return fallback
  try {
    const target = new URL(value, origin)
    return target.origin === new URL(origin).origin ? target.href : fallback
  } catch {
    return fallback
  }
}
