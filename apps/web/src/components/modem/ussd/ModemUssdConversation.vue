<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import ModemUssdMessageBubble from '@/components/modem/ussd/ModemUssdMessageBubble.vue'
import type { UssdMessageItem } from '@/composables/useUssdSession'

const props = defineProps<{
  items: UssdMessageItem[]
  hasEntries: boolean
}>()

const { t } = useI18n()
</script>

<template>
  <div class="flex-1 min-h-0 overflow-y-auto py-3 pr-1">
    <div v-if="props.hasEntries" class="space-y-3">
      <ModemUssdMessageBubble v-for="item in props.items" :key="item.id" :item="item" />
    </div>

    <p v-else class="text-sm text-muted-foreground">
      {{ t('modemDetail.ussd.empty') }}
    </p>
  </div>
</template>
