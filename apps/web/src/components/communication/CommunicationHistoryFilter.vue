<script setup lang="ts">
import { ToggleGroupItem, ToggleGroupRoot, type AcceptableValue } from 'reka-ui'

defineProps<{ label: string; allLabel: string; filteredLabel: string }>()
const filtered = defineModel<boolean>({ required: true })
const select = (value: AcceptableValue | AcceptableValue[]) => {
  if (value === 'all' || value === 'filtered') filtered.value = value === 'filtered'
}
</script>

<template>
  <ToggleGroupRoot
    type="single"
    :model-value="filtered ? 'filtered' : 'all'"
    :aria-label="label"
    class="grid w-full shrink-0 grid-cols-2 rounded-lg bg-muted p-1"
    @update:model-value="select"
  >
    <ToggleGroupItem
      value="all"
      class="rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-sm"
    >
      {{ allLabel }}
    </ToggleGroupItem>
    <ToggleGroupItem
      value="filtered"
      class="rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-sm"
    >
      {{ filteredLabel }}
    </ToggleGroupItem>
  </ToggleGroupRoot>
</template>
