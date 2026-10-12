<script setup lang="ts">
import { computed } from 'vue'

import type { UssdMessageItem } from '@/composables/useUssdSession'

const props = defineProps<{
  item: UssdMessageItem
}>()

const containerClass = computed(() =>
  props.item.incoming ? 'justify-start' : 'justify-end',
)

const bubbleClass = computed(() =>
  props.item.incoming ? 'bg-muted/60 text-foreground' : 'bg-primary text-primary-foreground',
)
</script>

<template>
  <div class="flex" :class="containerClass">
    <div class="max-w-[80%] rounded-2xl px-3 py-2 text-sm" :class="bubbleClass">
      <p class="whitespace-pre-wrap wrap-break-words">{{ props.item.text }}</p>
      <p class="mt-1 text-[10px] text-muted-foreground">
        {{ props.item.timestampLabel }}
      </p>
    </div>
  </div>
</template>
