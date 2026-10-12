<script setup lang="ts">
import { CreditCard, Smartphone } from 'lucide-vue-next'
import type { EsimTransferSource } from '@/types/esim'

const props = defineProps<{
  type: EsimTransferSource['type']
}>()
</script>

<template>
  <span class="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
    <CreditCard v-if="props.type === 'ccid'" class="size-3.5" />
    <Smartphone v-else class="size-3.5" />
  </span>
</template>
