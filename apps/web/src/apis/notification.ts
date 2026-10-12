import { fetchJson } from '@/lib/fetch'

import type { NotificationsResponse } from '@/types/notification'

export const useNotificationApi = () => {
  const getNotifications = (id: string) => {
    return fetchJson<NotificationsResponse>(`modems/${id}/notifications`)
  }

  const resendNotification = (id: string, seId: string, sequence: string) => {
    return fetchJson<void>(`modems/${id}/ses/${seId}/notifications/${sequence}/deliveries`, {
      method: 'POST',
    })
  }

  const deleteNotification = (id: string, seId: string, sequence: string) => {
    return fetchJson<void>(`modems/${id}/ses/${seId}/notifications/${sequence}`, {
      method: 'DELETE',
    })
  }

  return {
    getNotifications,
    resendNotification,
    deleteNotification,
  }
}
