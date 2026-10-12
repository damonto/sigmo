<script setup lang="ts">
import RegionFlag from '@/components/RegionFlag.vue'

const props = defineProps<{
  name: string
  icon?: string
  regionCode?: string
}>()
</script>

<template>
  <span
    class="flex size-11 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30 text-xs font-semibold text-muted-foreground"
  >
    <img
      v-if="props.icon"
      :src="props.icon"
      :alt="`${props.name} logo`"
      class="size-6 object-contain"
    />
    <RegionFlag
      v-else-if="props.regionCode"
      :region-code="props.regionCode"
      class="rounded-sm text-[18px]"
    />
    <span v-else>{{ props.name.slice(0, 1).toUpperCase() }}</span>
  </span>
</template>
