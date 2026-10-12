<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton'
const props = withDefaults(
  defineProps<{
    count?: number
  }>(),
  {
    count: 3,
  },
)
</script>

<template>
  <div class="space-y-3">
    <div
      v-for="index in props.count"
      :key="`notification-skeleton-${index}`"
      class="flex items-center justify-between rounded-lg border border-border bg-card px-4 py-3"
    >
      <div class="flex min-w-0 flex-1 flex-col gap-1.5">
        <Skeleton class="h-4 w-48 rounded bg-muted/60" />
        <Skeleton class="h-3 w-full rounded bg-muted/40" />
      </div>
      <Skeleton class="h-5 w-20 rounded-full bg-muted/50" />
    </div>
  </div>
</template>
