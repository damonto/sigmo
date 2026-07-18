import { describe, expect, it, vi } from 'vitest'

import { decodeBase64URL, webPushSupportReason } from '@/lib/webPush'
import {
  isWebPushPayload,
  notificationContent,
  notificationTargetURL,
} from '@/lib/webPushNotification'
import type { WebPushPayload } from '@/types/webPush'

describe('webPush', () => {
  it('shows the iOS setup reminder when Web Push APIs are unavailable', () => {
    const restore = [
      replaceProperty(window, 'isSecureContext', true),
      replaceProperty(window, 'matchMedia', vi.fn().mockReturnValue({ matches: false })),
      replaceProperty(navigator, 'userAgent', 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0)'),
      replaceProperty(navigator, 'platform', 'iPhone'),
      replaceProperty(navigator, 'maxTouchPoints', 5),
      removeProperty(window, 'Notification'),
      removeProperty(window, 'PushManager'),
      removeProperty(navigator, 'serviceWorker'),
    ]
    try {
      expect(webPushSupportReason()).toBe('ios_setup_required')
    } finally {
      for (const restoreProperty of restore.reverse()) restoreProperty()
    }
  })

  it('decodes URL-safe VAPID public keys', () => {
    expect([...new Uint8Array(decodeBase64URL('AQIDBA'))]).toEqual([1, 2, 3, 4])
  })

  it('validates supported notification payloads', () => {
    const tests: Array<{ payload: unknown; want: boolean }> = [
      {
        payload: {
          type: 'sms',
          id: 'sms-1',
          modemId: 'modem-1',
          modem: 'Office',
          from: '10086',
          url: '/modems/modem-1/messages/10086',
          tag: 'sms:sms-1',
        },
        want: true,
      },
      {
        payload: {
          type: 'reminder',
          id: 'reminder-1',
          modemId: 'modem-1',
          modem: 'Office',
          url: '/modems/modem-1',
          tag: 'reminder:esim:iccid',
        },
        want: true,
      },
      { payload: { type: 'sms' }, want: false },
    ]
    for (const test of tests) {
      expect(isWebPushPayload(test.payload)).toBe(test.want)
    }
  })

  it('renders SMS, call, and reminder notifications', () => {
    const tests: Array<{ payload: WebPushPayload; language: string; title: string }> = [
      {
        payload: {
          type: 'sms',
          id: 'sms-1',
          modemId: 'modem-1',
          modem: 'Office',
          from: '10086',
          text: 'hello',
          url: '/modems/modem-1/messages/10086',
          tag: 'sms:sms-1',
        },
        language: 'en-US',
        title: 'New message from 10086',
      },
      {
        payload: {
          type: 'call',
          id: 'call-1',
          modemId: 'modem-1',
          modem: 'Office',
          from: '10010',
          url: '/modems/modem-1/phone',
          tag: 'call:call-1',
        },
        language: 'zh-CN',
        title: '来电',
      },
      {
        payload: {
          type: 'reminder',
          id: 'reminder-1',
          modemId: 'modem-1',
          modem: 'Office',
          profileName: 'Travel eSIM',
          text: 'Renew package',
          url: '/modems/modem-1',
          tag: 'reminder:esim:iccid',
        },
        language: 'en-US',
        title: 'Reminder: Travel eSIM',
      },
    ]
    for (const test of tests) {
      expect(notificationContent(test.payload, test.language).title).toBe(test.title)
    }
  })

  it('keeps notification navigation on the current origin', () => {
    const origin = 'https://sigmo.example'
    const tests = [
      {
        name: 'relative application path',
        value: '/modems/modem-1/phone',
        want: 'https://sigmo.example/modems/modem-1/phone',
      },
      {
        name: 'same-origin absolute URL',
        value: 'https://sigmo.example/settings',
        want: 'https://sigmo.example/settings',
      },
      { name: 'protocol-relative external URL', value: '//example.com/path', want: `${origin}/` },
      { name: 'script URL', value: 'javascript:alert(1)', want: `${origin}/` },
      { name: 'missing URL', value: undefined, want: `${origin}/` },
    ]
    for (const test of tests) {
      expect(notificationTargetURL(test.value, origin), test.name).toBe(test.want)
    }
  })
})

const replaceProperty = (target: object, key: PropertyKey, value: unknown) => {
  const descriptor = Object.getOwnPropertyDescriptor(target, key)
  Object.defineProperty(target, key, { configurable: true, value })
  return () => {
    if (descriptor) {
      Object.defineProperty(target, key, descriptor)
      return
    }
    Reflect.deleteProperty(target, key)
  }
}

const removeProperty = (target: object, key: PropertyKey) => {
  const descriptor = Object.getOwnPropertyDescriptor(target, key)
  Reflect.deleteProperty(target, key)
  return () => {
    if (descriptor) Object.defineProperty(target, key, descriptor)
  }
}
