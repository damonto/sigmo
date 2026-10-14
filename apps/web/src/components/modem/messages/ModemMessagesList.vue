<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ModemMessagesItem from '@/components/modem/messages/ModemMessagesItem.vue'
import MessageListSkeleton from '@/components/communication/MessageListSkeleton.vue'
import type { ConversationItem } from '@/composables/useModemMessages'

const props = defineProps<{
  items: ConversationItem[]
  modemId: string
  isLoading: boolean
  emptyLabel?: string
  selectedParticipant?: string
  selectedConversation?: string
}>()

const emit = defineEmits<{
  (event: 'delete', item: ConversationItem): void
}>()

const { t } = useI18n()

const hasItems = computed(() => props.items.length > 0)
const emptyStateLabel = computed(() => props.emptyLabel || t('modemDetail.messages.empty'))
</script>

<template>
  <section class="space-y-3">
    <MessageListSkeleton v-if="props.isLoading && !hasItems" />

    <div
      v-else-if="hasItems"
      class="space-y-3"
    >
      <ModemMessagesItem
        v-for="item in props.items"
        :key="item.key"
        :item="item"
        :modem-id="props.modemId"
        :selected="
          props.selectedConversation
            ? item.key === props.selectedConversation
            : item.participantValue === props.selectedParticipant
        "
        @delete="emit('delete', $event)"
      />
    </div>

    <div
      v-else
      class="rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground"
    >
      {{ emptyStateLabel }}
    </div>
  </section>
</template>
