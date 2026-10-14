<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ModemMessageBubble from '@/components/modem/messages/ModemMessageBubble.vue'
import MessageThreadSkeleton from '@/components/communication/MessageThreadSkeleton.vue'
import type { ThreadMessageItem } from '@/composables/useModemMessageThread'

const props = defineProps<{
  items: ThreadMessageItem[]
  isLoading: boolean
  participantLabel: string
}>()

const { t } = useI18n()

const hasItems = computed(() => props.items.length > 0)
const emptyLabel = computed(() =>
  t('modemDetail.messages.threadEmpty', { participant: props.participantLabel }),
)
</script>

<template>
  <div
    v-if="hasItems"
    class="space-y-3"
  >
    <div
      v-for="item in props.items"
      :key="item.key"
      :data-message-id="item.key"
    >
      <ModemMessageBubble :item="item" />
    </div>
  </div>

  <MessageThreadSkeleton v-else-if="props.isLoading" />

  <p
    v-else
    class="text-sm text-muted-foreground"
  >
    {{ emptyLabel }}
  </p>
</template>
