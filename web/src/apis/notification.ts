import { fetchJson } from '@/lib/fetch'

import type { NotificationsResponse } from '@/types/notification'

export const useNotificationApi = () => {
  const getNotifications = (id: string) => {
    return fetchJson<NotificationsResponse>(`modems/${id}/notifications`)
  }

  const resendNotification = (id: string, sequence: string) => {
    return fetchJson<void>(`modems/${id}/notifications/${sequence}/deliveries`, {
      method: 'POST',
    })
  }

  const deleteNotification = (id: string, sequence: string) => {
    return fetchJson<void>(`modems/${id}/notifications/${sequence}`, {
      method: 'DELETE',
    })
  }

  return {
    getNotifications,
    resendNotification,
    deleteNotification,
  }
}
