<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'

const { t } = useI18n()
</script>

<template>
  <div
    role="status"
    :aria-label="t('communication.loading')"
    class="grid gap-4 rounded-xl border bg-card p-6"
  >
    <span class="sr-only">{{ t('communication.loading') }}</span>
    <div
      aria-hidden="true"
      class="space-y-4"
    >
      <div class="flex flex-col items-center gap-3 lg:flex-row lg:gap-4">
        <Skeleton class="size-20 shrink-0 rounded-full lg:size-14" />
        <div class="space-y-2">
          <Skeleton class="h-6 w-32" />
        </div>
      </div>
      <div class="flex justify-center gap-2 lg:justify-start">
        <Skeleton class="size-9 lg:w-24" />
        <Skeleton class="size-9 lg:w-20" />
      </div>
    </div>
    <div
      aria-hidden="true"
      class="space-y-5"
    >
      <Skeleton class="h-4 w-1/3" />
      <Skeleton class="h-4 w-1/2" />
    </div>
    <Separator />
    <div
      aria-hidden="true"
      class="flex flex-col gap-2 lg:flex-row lg:justify-end"
    >
      <Skeleton class="h-9 w-full lg:w-28" />
      <Skeleton class="h-9 w-full lg:w-20" />
    </div>
  </div>
</template>
