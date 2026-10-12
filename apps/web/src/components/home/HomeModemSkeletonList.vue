<script setup lang="ts">
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

const props = withDefaults(
  defineProps<{
    count?: number
  }>(),
  {
    count: 4,
  },
)
</script>

<template>
  <div class="grid gap-3 md:grid-cols-2">
    <Card
      v-for="index in props.count"
      :key="`skeleton-${index}`"
      class="h-full border-0 shadow-sm transition duration-300 group-hover:-translate-y-0.5 py-4"
    >
      <CardContent class="flex items-center gap-3 px-4">
        <Skeleton class="h-12 w-12 shrink-0 rounded-2xl bg-muted/80" />
        <div class="flex flex-1 flex-col gap-1.5">
          <Skeleton class="h-4 w-36 rounded bg-muted/60" />
          <Skeleton class="h-3.5 w-40 rounded bg-muted/40" />
          <Skeleton class="h-3 w-28 rounded bg-muted/30" />
        </div>
      </CardContent>
    </Card>
  </div>
</template>
