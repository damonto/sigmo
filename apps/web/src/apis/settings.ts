import { fetchJson } from '@/lib/fetch'

import type {
  SettingsAuth,
  SettingsAuthTest,
  SettingsChannel,
  SettingsProxy,
  SettingsNotifications,
  SettingsResponse,
} from '@/types/settings'

export const useSettingsApi = () => {
  const getSettings = () => {
    return fetchJson<SettingsResponse>('settings')
  }

  const updateAuth = (payload: SettingsAuth) => {
    return fetchJson<SettingsResponse>('settings/auth', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  const testAuth = (payload: SettingsAuthTest) => {
    return fetchJson<void>('settings/auth-tests', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  }

  const updateProxy = (payload: SettingsProxy) => {
    return fetchJson<SettingsResponse>('settings/proxy', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  const updateNotificationChannel = (channel: string, payload: SettingsChannel) => {
    return fetchJson<SettingsResponse>(`settings/channels/${encodeURIComponent(channel)}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  }

  return {
    updateNotifications: (payload: SettingsNotifications) =>
      fetchJson<SettingsResponse>('settings/notifications', {
        method: 'PUT',
        body: JSON.stringify(payload),
      }),
    getSettings,
    testAuth,
    updateAuth,
    updateProxy,
    updateNotificationChannel,
  }
}
