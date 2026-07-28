<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  regionCode?: string
}>()

defineOptions({
  inheritAttrs: false,
})

const displayCode = computed(() => props.regionCode?.trim().toUpperCase() ?? '')
const flagCode = computed(() => {
  if (!/^[A-Z]{2}$/.test(displayCode.value)) return ''
  return displayCode.value.toLowerCase()
})
const flagClass = computed(() => (flagCode.value ? `fi fi-${flagCode.value}` : ''))
</script>

<template>
  <span
    v-if="flagClass"
    v-bind="$attrs"
    :class="flagClass"
    :aria-label="displayCode"
    :title="displayCode"
  />
  <span
    v-else-if="displayCode"
    v-bind="$attrs"
    class="font-semibold text-muted-foreground"
    :aria-label="displayCode"
    :title="displayCode"
  >
    {{ displayCode }}
  </span>
</template>
