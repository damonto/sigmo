<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ModemNotificationsItem from '@/components/modem/notifications/ModemNotificationsItem.vue'
import ModemNotificationsSkeletonList from '@/components/modem/notifications/ModemNotificationsSkeletonList.vue'
import type { NotificationItem } from '@/composables/useModemNotifications'

const props = defineProps<{
  items: NotificationItem[]
  isLoading: boolean
  resendingSequence: string | null
}>()

const emit = defineEmits<{
  (event: 'resend', item: NotificationItem): void
  (event: 'delete', item: NotificationItem): void
}>()

const { t } = useI18n()

const hasItems = computed(() => props.items.length > 0)
const hasMultipleSEs = computed(() => new Set(props.items.map((item) => item.seId)).size > 1)
const itemGroups = computed(() => {
  const groups = new Map<
    string,
    { id: string; label: string; eid?: string; items: NotificationItem[] }
  >()
  for (const item of props.items) {
    if (!groups.has(item.seId)) {
      groups.set(item.seId, {
        id: item.seId,
        label: item.seLabel,
        eid: item.seEid,
        items: [],
      })
    }
    groups.get(item.seId)?.items.push(item)
  }
  return Array.from(groups.values())
})
</script>

<template>
  <section class="space-y-3">
    <ModemNotificationsSkeletonList v-if="props.isLoading" />

    <div v-else-if="hasItems" class="space-y-4">
      <div v-for="group in itemGroups" :key="group.id" class="space-y-3">
        <p v-if="hasMultipleSEs" class="px-1 text-xs font-semibold text-muted-foreground">
          {{ group.label }}: {{ group.eid || 'N/A' }}
        </p>
        <ModemNotificationsItem
          v-for="item in group.items"
          :key="item.key"
          :item="item"
          :is-resending="props.resendingSequence === item.key"
          @resend="emit('resend', $event)"
          @delete="emit('delete', $event)"
        />
      </div>
    </div>

    <div
      v-else
      class="rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground"
    >
      {{ t('modemDetail.notifications.empty') }}
    </div>
  </section>
</template>
