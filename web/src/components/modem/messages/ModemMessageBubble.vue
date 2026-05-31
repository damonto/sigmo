<script setup lang="ts">
import { computed } from 'vue'
import { Wifi } from 'lucide-vue-next'

import type { ThreadMessageItem } from '@/composables/useModemMessageThread'

const props = defineProps<{
  item: ThreadMessageItem
}>()

const containerClass = computed(() => (props.item.incoming ? 'justify-start' : 'justify-end'))

const bubbleClass = computed(() =>
  props.item.incoming ? 'bg-muted/60 text-foreground' : 'bg-primary text-primary-foreground',
)

const metaClass = computed(() =>
  props.item.incoming ? 'text-muted-foreground' : 'font-medium text-primary-foreground/90',
)

const wifiIconClass = computed(() =>
  props.item.incoming ? 'text-sky-500' : 'text-primary-foreground/90',
)

const showStatus = computed(() => !props.item.incoming && Boolean(props.item.status))
</script>

<template>
  <div class="flex" :class="containerClass">
    <div class="max-w-[80%] rounded-2xl px-3 py-2 text-sm" :class="bubbleClass">
      <p class="whitespace-pre-wrap wrap-break-words">{{ props.item.text }}</p>
      <p class="mt-1 text-[10px]" :class="metaClass" data-testid="message-meta">
        <Wifi
          v-if="props.item.wifiCalling"
          class="mr-1 inline size-3 align-[-2px]"
          :class="wifiIconClass"
          title="Wi-Fi Calling"
          aria-label="Wi-Fi Calling"
        />
        <span>{{ props.item.timestampLabel }}</span>
        <span v-if="showStatus"> · {{ props.item.status }} </span>
      </p>
    </div>
  </div>
</template>
