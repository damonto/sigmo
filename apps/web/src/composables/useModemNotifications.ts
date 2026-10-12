import { computed, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useNotificationApi } from '@/apis/notification'
import type { NotificationGroupResponse } from '@/types/notification'

export type NotificationItem = {
  key: string
  seId: string
  seLabel: string
  seEid?: string
  sequenceNumber: string
  iccid: string
  smdp: string
  operationLabel: string
}

export const useModemNotifications = (modemId: ComputedRef<string>) => {
  const { t } = useI18n()
  const notificationApi = useNotificationApi()

  const notificationGroups = ref<NotificationGroupResponse[]>([])
  const isLoading = ref(false)

  const count = computed(() =>
    notificationGroups.value.reduce((total, group) => total + group.notifications.length, 0),
  )

  const items = computed<NotificationItem[]>(() =>
    notificationGroups.value.flatMap((group) =>
      group.notifications.map((notification) => {
        const operation = notification.operation.trim()
        return {
          key: `${notification.seId || group.id}:${notification.sequenceNumber}`,
          seId: notification.seId || group.id,
          seLabel: notification.seLabel || group.label,
          seEid: notification.seEid || group.eid,
          sequenceNumber: notification.sequenceNumber,
          iccid: notification.iccid,
          smdp: notification.smdp,
          operationLabel: operation
            ? operation.toLowerCase()
            : t('modemDetail.notifications.operationUnknown'),
        }
      }),
    ),
  )

  const fetchNotifications = async (id?: string) => {
    const targetId = id ?? modemId.value
    if (!targetId || targetId === 'unknown') return
    if (isLoading.value) return
    isLoading.value = true
    try {
      const { data } = await notificationApi.getNotifications(targetId)
      notificationGroups.value = data.value?.ses ?? []
    } finally {
      isLoading.value = false
    }
  }

  const resendNotification = async (seId: string, sequence: string) => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!sequence.trim()) return
    await notificationApi.resendNotification(targetId, seId, sequence)
  }

  const deleteNotification = async (seId: string, sequence: string) => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!sequence.trim()) return
    await notificationApi.deleteNotification(targetId, seId, sequence)
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
    notificationGroups,
    items,
    count,
    isLoading,
    fetchNotifications,
    resendNotification,
    deleteNotification,
  }
}
