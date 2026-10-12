<script setup lang="ts">
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import type { SEItem } from '@/types/se'

const props = defineProps<{
  ses: SEItem[]
  selectedSeId: string
}>()

const emit = defineEmits<{
  (event: 'update:selectedSeId', value: string): void
}>()

const formatBytes = (bytes?: number) => {
  if (bytes === null || bytes === undefined) return 'N/A'
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KiB', 'MiB', 'GiB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${Math.round((bytes / Math.pow(k, i)) * 100) / 100} ${sizes[i]}`
}

const selectSE = (value: string) => {
  if (!props.ses.some((item) => item.id === value)) return
  emit('update:selectedSeId', value)
}
</script>

<template>
  <RadioGroup
    class="gap-2"
    :model-value="props.selectedSeId"
    @update:model-value="(value) => selectSE(String(value))"
  >
    <label
      v-for="item in props.ses"
      :key="item.id"
      class="flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2 shadow-sm transition"
      :class="
        props.selectedSeId === item.id
          ? 'border-primary/40 bg-primary/5'
          : 'border-transparent bg-muted/30'
      "
    >
      <RadioGroupItem :id="`install-se-${item.id}`" :value="item.id" class="mt-1" />
      <span class="min-w-0 space-y-1">
        <span class="block min-w-0 break-all font-mono text-xs font-medium leading-5 text-foreground">
          {{ item.eid || 'N/A' }}
        </span>
        <span class="block text-xs text-muted-foreground">
          Storage Remaining {{ formatBytes(item.freeSpace) }}
        </span>
      </span>
    </label>
  </RadioGroup>
</template>
