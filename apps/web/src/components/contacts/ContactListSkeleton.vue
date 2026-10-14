<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Skeleton } from '@/components/ui/skeleton'

const { t } = useI18n()
</script>

<template>
  <div
    role="status"
    :aria-label="t('communication.loading')"
    class="divide-y rounded-xl border bg-card"
  >
    <span class="sr-only">{{ t('communication.loading') }}</span>
    <div
      v-for="index in 6"
      :key="index"
      class="flex min-h-16 items-center gap-3 p-3"
      aria-hidden="true"
    >
      <Skeleton class="size-10 shrink-0 rounded-full" />
      <div class="min-w-0 flex-1 space-y-2">
        <Skeleton class="h-4 w-1/3" />
        <Skeleton class="h-3 w-1/2" />
      </div>
    </div>
  </div>
</template>
