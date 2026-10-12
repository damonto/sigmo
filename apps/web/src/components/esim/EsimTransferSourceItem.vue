<script setup lang="ts">
import { ChevronRight } from 'lucide-vue-next'

import EsimTransferSourceIcon from '@/components/esim/EsimTransferSourceIcon.vue'
import type { EsimTransferSource } from '@/types/esim'

const props = defineProps<{
  source: EsimTransferSource
  detail: string
}>()

const emit = defineEmits<{
  (event: 'select', source: EsimTransferSource): void
}>()
</script>

<template>
  <button
    type="button"
    class="flex w-full min-w-0 max-w-full items-center gap-3 rounded-lg border border-transparent bg-card px-4 py-3 text-left text-sm shadow-sm transition hover:bg-muted/40"
    @click="emit('select', props.source)"
  >
    <EsimTransferSourceIcon :type="props.source.type" />
    <span class="min-w-0 flex-1">
      <span class="block truncate font-medium">{{ props.source.name }}</span>
      <span class="block truncate text-xs text-muted-foreground">{{ props.detail }}</span>
    </span>
    <ChevronRight class="size-3.5 shrink-0 text-muted-foreground" />
  </button>
</template>
