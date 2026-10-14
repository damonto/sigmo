<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CallRecord } from '@/types/call'
defineProps<{ call: CallRecord; state: string; route: string; duration: string }>()
const { t } = useI18n()
const formatDetailTimestamp = (value: string) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(date)
}
</script>
<template>
  <dl class="grid grid-cols-2 gap-3 text-sm xl:grid-cols-3">
    <div class="space-y-1">
      <dt class="text-xs font-medium text-muted-foreground">
        {{ t('modemDetail.calls.details.direction') }}
      </dt>
      <dd>{{ t(`modemDetail.calls.${call.direction}`) }}</dd>
    </div>
    <div class="space-y-1">
      <dt class="text-xs font-medium text-muted-foreground">
        {{ t('modemDetail.calls.details.state') }}
      </dt>
      <dd>{{ state }}</dd>
    </div>
    <div class="space-y-1">
      <dt class="text-xs font-medium text-muted-foreground">
        {{ t('modemDetail.calls.details.route') }}
      </dt>
      <dd>{{ route }}</dd>
    </div>
    <div class="space-y-1">
      <dt class="text-xs font-medium text-muted-foreground">
        {{ t('modemDetail.calls.details.duration') }}
      </dt>
      <dd>{{ duration || t('modemDetail.calls.durationEmpty') }}</dd>
    </div>
    <div class="space-y-1">
      <dt class="text-xs font-medium text-muted-foreground">
        {{ t('modemDetail.calls.details.startedAt') }}
      </dt>
      <dd>{{ formatDetailTimestamp(call.startedAt) }}</dd>
    </div>
    <div class="space-y-1">
      <dt class="text-xs font-medium text-muted-foreground">
        {{ t('modemDetail.calls.details.answeredAt') }}
      </dt>
      <dd>
        {{
          call.answeredAt
            ? formatDetailTimestamp(call.answeredAt)
            : t('modemDetail.calls.details.notAnswered')
        }}
      </dd>
    </div>
    <div class="space-y-1">
      <dt class="text-xs font-medium text-muted-foreground">
        {{ t('modemDetail.calls.details.endedAt') }}
      </dt>
      <dd>
        {{
          call.endedAt
            ? formatDetailTimestamp(call.endedAt)
            : t('modemDetail.calls.details.inProgress')
        }}
      </dd>
    </div>
    <div
      v-if="call.reason"
      class="space-y-1"
    >
      <dt class="text-xs font-medium text-muted-foreground">
        {{ t('modemDetail.calls.details.reason') }}
      </dt>
      <dd>{{ call.reason }}</dd>
    </div>
  </dl>
</template>
