import { useFetch } from '@/lib/fetch'

import type { NotificationsResponse } from '@/types/notification'

export const useNotificationApi = () => {
  const getNotifications = (id: string) => {
    return useFetch<NotificationsResponse>(`modems/${id}/notifications`).get().json()
  }

  const resendNotification = (id: string, sequence: string) => {
    return useFetch<void>(`modems/${id}/notifications/${sequence}/deliveries`, {
      method: 'POST',
    }).json()
  }

  const deleteNotification = (id: string, sequence: string) => {
    return useFetch<void>(`modems/${id}/notifications/${sequence}`, {
      method: 'DELETE',
    }).json()
  }

  return {
    getNotifications,
    resendNotification,
    deleteNotification,
  }
}
