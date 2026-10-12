<script setup lang="ts">
import CarrierLogo from '@/components/CarrierLogo.vue'

const props = defineProps<{
  name: string
  icon?: string
  plmn?: string
  regionCode?: string
}>()
</script>

<template>
  <span
    class="flex size-11 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30 text-xs font-semibold text-muted-foreground"
  >
    <CarrierLogo
      :name="props.name"
      :icon="props.icon"
      :plmn="props.plmn"
      :region-code="props.regionCode"
      class="size-6 text-[18px]"
    />
  </span>
</template>
