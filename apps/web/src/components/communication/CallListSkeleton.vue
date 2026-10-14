<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Skeleton } from '@/components/ui/skeleton'

const { t } = useI18n()
</script>

<template>
  <div
    role="status"
    :aria-label="t('communication.loading')"
    class="space-y-3"
  >
    <span class="sr-only">{{ t('communication.loading') }}</span>
    <div
      v-for="index in 5"
      :key="index"
      class="flex items-center gap-3 rounded-lg border bg-card px-4 py-4"
      aria-hidden="true"
    >
      <Skeleton class="size-11 shrink-0 rounded-full" />
      <div class="min-w-0 flex-1 space-y-2">
        <Skeleton class="h-4 w-2/5" />
        <Skeleton class="h-3 w-3/5" />
      </div>
      <Skeleton class="h-3 w-12" />
      <Skeleton class="size-9 shrink-0 rounded-full" />
    </div>
  </div>
</template>
