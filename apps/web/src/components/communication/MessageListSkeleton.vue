<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton'
import { useI18n } from 'vue-i18n'
const props = withDefaults(
  defineProps<{
    count?: number
  }>(),
  {
    count: 4,
  },
)
const { t } = useI18n()
</script>

<template>
  <div
    class="space-y-3"
    role="status"
    :aria-label="t('communication.loading')"
  >
    <span class="sr-only">{{ t('communication.loading') }}</span>
    <div
      v-for="index in props.count"
      :key="`message-skeleton-${index}`"
      aria-hidden="true"
      class="flex items-center justify-between rounded-lg border border-border bg-card px-4 py-3"
    >
      <div class="flex min-w-0 flex-1 items-center gap-3">
        <Skeleton class="size-11 shrink-0 rounded-full bg-muted/70" />
        <div class="flex min-w-0 flex-1 flex-col gap-1.5">
          <Skeleton class="h-4 w-2/3 rounded bg-muted/60" />
          <Skeleton class="h-3 w-full rounded bg-muted/40" />
        </div>
      </div>
      <Skeleton class="h-5 w-16 rounded-full bg-muted/50" />
    </div>
  </div>
</template>
