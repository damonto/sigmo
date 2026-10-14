<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton'
import { useI18n } from 'vue-i18n'
const props = withDefaults(
  defineProps<{
    count?: number
  }>(),
  {
    count: 6,
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
      :key="`thread-skeleton-${index}`"
      class="flex"
      :class="index % 2 === 0 ? 'justify-end' : ''"
      aria-hidden="true"
    >
      <Skeleton class="h-8 w-2/3 rounded-2xl bg-muted/60" />
    </div>
  </div>
</template>
