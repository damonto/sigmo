import type { WebPushPayload } from '@/types/webPush'

export type WebPushNotificationContent = {
  title: string
  options: NotificationOptions
  actionLabel: string
}

// This module also runs inside the service worker, so it keeps its own tiny
// message table instead of pulling vue-i18n and the full locale files into
// the worker bundle.
type Locale = 'en' | 'zh'

type Messages = {
  view: string
  unknownNumber: string
  incomingCall: string
  emptyMessage: string
  smsTitle: (from: string) => string
  reminderTitle: (profile: string) => string
  defaultProfile: string
  defaultReminder: string
}

const messages: Record<Locale, Messages> = {
  en: {
    view: 'View',
    unknownNumber: 'Unknown number',
    incomingCall: 'Incoming call',
    emptyMessage: 'Empty message',
    smsTitle: (from) => `New message from ${from}`,
    reminderTitle: (profile) => `Reminder: ${profile}`,
    defaultProfile: 'SIM',
    defaultReminder: 'Scheduled reminder',
  },
  zh: {
    view: '查看',
    unknownNumber: '未知号码',
    incomingCall: '来电',
    emptyMessage: '空短信',
    smsTitle: (from) => `来自 ${from} 的新短信`,
    reminderTitle: (profile) => `提醒：${profile}`,
    defaultProfile: 'SIM 卡',
    defaultReminder: '到期提醒',
  },
}

const localeFor = (language: string): Locale =>
  language.toLowerCase().startsWith('zh') ? 'zh' : 'en'

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
  const t = messages[localeFor(language)]
  const from = payload.from?.trim() || t.unknownNumber

  if (payload.type === 'call') {
    const to = payload.to?.trim()
    return build(payload, t, t.incomingCall, to ? `${from} → ${to}` : from)
  }

  if (payload.type === 'reminder') {
    const profile = payload.profileName?.trim() || payload.profileId?.trim() || t.defaultProfile
    return build(payload, t, t.reminderTitle(profile), payload.text?.trim() || t.defaultReminder)
  }

  return build(payload, t, t.smsTitle(from), payload.text?.trim() || t.emptyMessage)
}

const build = (
  payload: WebPushPayload,
  t: Messages,
  title: string,
  body: string,
): WebPushNotificationContent => {
  const modem = payload.modem.trim()
  return {
    title,
    actionLabel: t.view,
    options: {
      body: modem ? `${body} · ${modem}` : body,
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
