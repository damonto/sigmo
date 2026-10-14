<script setup lang="ts">
import { computed } from 'vue'

import MessageConversationRow from '@/components/communication/MessageConversationRow.vue'
import type { ConversationItem } from '@/composables/useModemMessages'

const props = defineProps<{
  item: ConversationItem
  modemId: string
  selected?: boolean
}>()

const emit = defineEmits<{
  (event: 'delete', item: ConversationItem): void
}>()

const threadRoute = computed(() => ({
  name: 'modem-message-thread',
  params: { id: props.modemId, participant: props.item.participantValue },
  query: { conversation: props.item.key, profile: props.item.profileId },
}))

const avatarLabel = computed(() => {
  const value = props.item.participantLabel.trim()
  const match = value.match(/[A-Za-z0-9]/)
  return (match?.[0] ?? '?').toUpperCase()
})

const avatarTones = [
  'bg-rose-100 text-rose-700 ring-rose-200/70 dark:bg-rose-500/15 dark:text-rose-200 dark:ring-rose-400/20',
  'bg-amber-100 text-amber-700 ring-amber-200/70 dark:bg-amber-500/15 dark:text-amber-200 dark:ring-amber-400/20',
  'bg-emerald-100 text-emerald-700 ring-emerald-200/70 dark:bg-emerald-500/15 dark:text-emerald-200 dark:ring-emerald-400/20',
  'bg-sky-100 text-sky-700 ring-sky-200/70 dark:bg-sky-500/15 dark:text-sky-200 dark:ring-sky-400/20',
  'bg-violet-100 text-violet-700 ring-violet-200/70 dark:bg-violet-500/15 dark:text-violet-200 dark:ring-violet-400/20',
  'bg-cyan-100 text-cyan-700 ring-cyan-200/70 dark:bg-cyan-500/15 dark:text-cyan-200 dark:ring-cyan-400/20',
]

const avatarTone = computed(() => {
  const value = props.item.participantValue || props.item.participantLabel
  const hash = Array.from(value).reduce((sum, char) => sum + char.charCodeAt(0), 0)
  return avatarTones[hash % avatarTones.length]
})
</script>

<template>
  <MessageConversationRow
    :to="threadRoute"
    :name="item.participantLabel"
    :preview="item.preview"
    :timestamp="item.timestampLabel"
    :line="item.lineLabel"
    :unread-count="item.unreadCount"
    :selected="selected"
    @delete="emit('delete', item)"
  >
    <template #avatar>
      <span
        class="flex size-11 shrink-0 items-center justify-center rounded-full text-base font-semibold"
        :class="avatarTone"
        aria-hidden="true"
        >{{ avatarLabel }}</span
      >
    </template>
  </MessageConversationRow>
</template>
