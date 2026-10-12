<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { toast } from 'vue-sonner'

import ModemNotificationsDeleteDialog from '@/components/modem/notifications/ModemNotificationsDeleteDialog.vue'
import ModemNotificationsHeader from '@/components/modem/notifications/ModemNotificationsHeader.vue'
import ModemNotificationsList from '@/components/modem/notifications/ModemNotificationsList.vue'
import { useModemNotifications, type NotificationItem } from '@/composables/useModemNotifications'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => (route.params.id ?? 'unknown') as string)

const { items, count, isLoading, resendNotification, deleteNotification } =
  useModemNotifications(modemId)

const deleteOpen = ref(false)
const deleteLoading = ref(false)
const deleteTarget = ref<NotificationItem | null>(null)
const resendSequence = ref<string | null>(null)

const deleteTargetLabel = computed(() => deleteTarget.value?.iccid ?? '')

const openDeleteDialog = (item: NotificationItem) => {
  deleteTarget.value = item
  deleteOpen.value = true
}

const closeDeleteDialog = () => {
  deleteOpen.value = false
  deleteTarget.value = null
}

const confirmDelete = async () => {
  if (!deleteTarget.value) return
  deleteLoading.value = true
  try {
    await deleteNotification(deleteTarget.value.seId, deleteTarget.value.sequenceNumber)
  } catch (err) {
    console.error('[ModemNotificationsView] Failed to delete notification:', err)
  } finally {
    deleteLoading.value = false
    closeDeleteDialog()
  }
}

const handleResend = async (item: NotificationItem) => {
  if (resendSequence.value) return
  resendSequence.value = item.key
  try {
    await resendNotification(item.seId, item.sequenceNumber)
    toast.success(t('modemDetail.notifications.resendSuccess'))
  } catch (err) {
    console.error('[ModemNotificationsView] Failed to resend notification:', err)
  } finally {
    resendSequence.value = null
  }
}
</script>

<template>
  <div class="space-y-6">
    <ModemNotificationsHeader :count="count" :is-loading="isLoading" :modem-id="modemId" />

    <ModemNotificationsList
      :items="items"
      :is-loading="isLoading"
      :resending-sequence="resendSequence"
      @resend="handleResend"
      @delete="openDeleteDialog"
    />
  </div>

  <ModemNotificationsDeleteDialog
    v-model:open="deleteOpen"
    :target-label="deleteTargetLabel"
    :is-deleting="deleteLoading"
    @confirm="confirmDelete"
  />
</template>
