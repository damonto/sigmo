<script setup lang="ts">
import { computed } from 'vue'
import { RefreshCw, Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import type { NotificationItem } from '@/composables/useModemNotifications'

const props = defineProps<{
  item: NotificationItem
  isResending: boolean
}>()

const emit = defineEmits<{
  (event: 'resend', item: NotificationItem): void
  (event: 'delete', item: NotificationItem): void
}>()

const { t } = useI18n()

const iccidLabel = computed(() =>
  props.item.iccid ? props.item.iccid : t('modemDetail.notifications.unknownIccid'),
)
const smdpLabel = computed(() =>
  props.item.smdp ? props.item.smdp : t('modemDetail.notifications.unknownSmdp'),
)
const operationLabel = computed(() => props.item.operationLabel.toUpperCase())
</script>

<template>
  <div class="rounded-lg bg-card px-4 py-3 shadow-sm">
    <div class="flex flex-col gap-2">
      <div class="flex items-center justify-between gap-3">
        <div class="flex min-w-0 items-center gap-2">
          <span class="shrink-0 text-xs font-medium text-muted-foreground">
            #{{ props.item.sequenceNumber }}
          </span>
          <p class="truncate text-sm font-semibold text-foreground">
            {{ iccidLabel }}
          </p>
        </div>
        <Badge
          variant="secondary"
          class="text-[10px] font-semibold tracking-[0.12em] uppercase text-foreground"
        >
          {{ operationLabel }}
        </Badge>
      </div>
      <div class="flex items-center justify-between gap-3">
        <p class="truncate text-xs text-muted-foreground">
          {{ smdpLabel }}
        </p>
        <div class="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            type="button"
            :disabled="props.isResending"
            :aria-label="t('modemDetail.actions.resend')"
            @click="emit('resend', props.item)"
          >
            <Spinner v-if="props.isResending" class="size-4 text-muted-foreground" />
            <RefreshCw v-else class="size-4 text-muted-foreground" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            type="button"
            :aria-label="t('modemDetail.actions.delete')"
            @click="emit('delete', props.item)"
          >
            <Trash2 class="size-4 text-muted-foreground" />
          </Button>
        </div>
      </div>
    </div>
  </div>
</template>
