<script setup lang="ts">
import { computed } from 'vue'

import { RadioGroupItem } from '@/components/ui/radio-group'
import type { NetworkResponse } from '@/types/network'

const props = defineProps<{
  network: NetworkResponse
  isSelected: boolean
}>()

const displayName = computed(
  () => props.network.operatorName || props.network.operatorShortName || props.network.operatorCode,
)

const accessLabel = computed(() => props.network.accessTechnologies.join(' / '))
const showAccess = computed(() => props.network.accessTechnologies.length > 0)
</script>

<template>
  <label
    class="flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2 shadow-sm transition"
    :class="props.isSelected ? 'border-primary/40 bg-primary/5' : 'border-transparent bg-muted/30'"
  >
    <RadioGroupItem
      :id="`network-${props.network.operatorCode}`"
      :value="props.network.operatorCode"
      class="mt-1"
    />
    <div class="min-w-0 space-y-1">
      <p class="text-sm font-semibold text-foreground">
        {{ displayName }}
      </p>
      <p class="text-xs text-muted-foreground">
        {{ props.network.operatorCode }} · {{ props.network.status }}
      </p>
      <p v-if="showAccess" class="text-xs text-muted-foreground">
        {{ accessLabel }}
      </p>
    </div>
  </label>
</template>
