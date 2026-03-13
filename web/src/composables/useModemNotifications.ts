import { computed, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useNotificationApi } from '@/apis/notification'
import type { NotificationResponse } from '@/types/notification'

export type NotificationItem = {
  key: string
  sequenceNumber: string
  iccid: string
  smdp: string
  operationLabel: string
}

export const useModemNotifications = (modemId: ComputedRef<string>) => {
  const { t } = useI18n()
  const notificationApi = useNotificationApi()

  const notifications = ref<NotificationResponse[]>([])
  const isLoading = ref(false)

  const count = computed(() => notifications.value.length)

  const items = computed<NotificationItem[]>(() =>
    notifications.value.map((notification) => {
      const operation = notification.operation.trim()
      return {
        key: notification.sequenceNumber,
        sequenceNumber: notification.sequenceNumber,
        iccid: notification.iccid,
        smdp: notification.smdp,
        operationLabel: operation
          ? operation.toLowerCase()
          : t('modemDetail.notifications.operationUnknown'),
      }
    }),
  )

  const fetchNotifications = async (id?: string) => {
    const targetId = id ?? modemId.value
    if (!targetId || targetId === 'unknown') return
    if (isLoading.value) return
    isLoading.value = true
    try {
      const { data } = await notificationApi.getNotifications(targetId)
      notifications.value = data.value ?? []
    } finally {
      isLoading.value = false
    }
  }

  const resendNotification = async (sequence: string) => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!sequence.trim()) return
    await notificationApi.resendNotification(targetId, sequence)
  }

  const deleteNotification = async (sequence: string) => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!sequence.trim()) return
    await notificationApi.deleteNotification(targetId, sequence)
    await fetchNotifications(targetId)
  }

  watch(
    modemId,
    async (id) => {
      if (!id || id === 'unknown') return
      await fetchNotifications(id)
    },
    { immediate: true },
  )

  return {
    notifications,
    items,
    count,
    isLoading,
    fetchNotifications,
    resendNotification,
    deleteNotification,
  }
}
