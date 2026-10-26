<script setup lang="ts">
import RegionFlag from '@/components/RegionFlag.vue'
import { useCarrierLogo } from '@/composables/useCarrierLogo'

/** Carrier identity with embedded icon, public PNG, then local flag fallback. */
const props = defineProps<{
  name: string
  icon?: string
  plmn?: string
  regionCode?: string
}>()

const { source, loaded, onLoad, onError } = useCarrierLogo(
  () => props.icon,
  () => props.plmn,
)
</script>

<template>
  <span
    class="relative inline-flex size-full shrink-0 items-center justify-center overflow-hidden rounded-[inherit]"
  >
    <img
      v-if="source"
      v-show="loaded"
      :key="source"
      :src="source"
      :alt="props.name"
      referrerpolicy="no-referrer"
      class="absolute inset-0 size-full object-contain"
      @load="onLoad"
      @error="onError"
    />
    <template v-if="!loaded">
      <RegionFlag
        v-if="props.regionCode"
        :region-code="props.regionCode"
        class="rounded-sm"
      />
      <span v-else>{{ props.name.slice(0, 1).toUpperCase() }}</span>
    </template>
  </span>
</template>
