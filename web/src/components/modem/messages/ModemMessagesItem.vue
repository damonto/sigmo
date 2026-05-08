<script setup lang="ts">
import { computed } from 'vue'
import { Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import { Button } from '@/components/ui/button'
import type { ConversationItem } from '@/composables/useModemMessages'

const props = defineProps<{
  item: ConversationItem
  modemId: string
}>()

const emit = defineEmits<{
  (event: 'delete', item: ConversationItem): void
}>()

const { t } = useI18n()

const threadRoute = computed(() => ({
  name: 'modem-message-thread',
  params: { id: props.modemId, participant: props.item.participantValue },
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
  <div class="group rounded-lg bg-card px-4 py-3 shadow-sm transition hover:shadow-md">
    <div class="flex items-center gap-3">
      <RouterLink :to="threadRoute" class="flex min-w-0 flex-1 items-center gap-3">
        <span
          class="flex size-11 shrink-0 items-center justify-center rounded-full text-base font-semibold shadow-sm ring-1"
          :class="avatarTone"
          aria-hidden="true"
        >
          {{ avatarLabel }}
        </span>

        <span class="min-w-0 flex-1 space-y-1">
          <span class="flex min-w-0 items-center justify-between gap-3">
            <span class="truncate text-sm font-semibold text-foreground">
              {{ props.item.participantLabel }}
            </span>
            <span class="shrink-0 text-xs font-medium text-muted-foreground">
              {{ props.item.timestampLabel }}
            </span>
          </span>
          <span class="block truncate text-xs text-muted-foreground">
            {{ props.item.preview }}
          </span>
        </span>
      </RouterLink>

      <Button
        variant="ghost"
        size="icon"
        type="button"
        class="size-8 shrink-0 opacity-80 transition hover:text-destructive focus-visible:opacity-100 md:opacity-0 md:group-hover:opacity-100"
        :aria-label="t('modemDetail.actions.delete')"
        @click.stop.prevent="emit('delete', props.item)"
      >
        <Trash2 class="size-4" />
      </Button>
    </div>
  </div>
</template>
