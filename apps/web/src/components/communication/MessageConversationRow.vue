<script setup lang="ts">
import { Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import { Button } from '@/components/ui/button'

defineProps<{
  to: RouteLocationRaw
  name: string
  preview: string
  timestamp?: string
  line?: string
  unreadCount: number
  markedUnread?: boolean
  selected?: boolean
}>()
const emit = defineEmits<{ delete: [] }>()
const { t } = useI18n()
</script>
<template>
  <div
    class="flex min-h-20 items-center gap-3 rounded-lg border border-border bg-card px-4 py-3 transition-colors"
    :class="selected ? 'bg-primary/5' : 'hover:bg-muted/40'"
  >
    <RouterLink
      :to="to"
      class="flex min-w-0 flex-1 items-center gap-3"
    >
      <slot name="avatar" />
      <span class="min-w-0 flex-1 space-y-1">
        <span class="flex items-baseline justify-between gap-3">
          <span class="truncate text-sm font-semibold">{{ name }}</span>
          <span class="shrink-0 text-xs text-muted-foreground">{{ timestamp }}</span>
        </span>
        <span class="block truncate text-xs text-muted-foreground">{{ preview }}</span>
        <span
          v-if="line"
          class="block truncate text-xs text-muted-foreground"
          >{{ line }}</span
        >
        <span
          v-if="unreadCount || markedUnread"
          class="block text-xs font-semibold text-primary"
          >{{ t('modemDetail.messages.unread')
          }}<template v-if="unreadCount"> · {{ unreadCount }}</template></span
        >
      </span>
    </RouterLink>
    <Button
      variant="ghost"
      size="icon"
      class="shrink-0 rounded-full text-muted-foreground"
      :aria-label="t('modemDetail.actions.delete')"
      @click.stop.prevent="emit('delete')"
      ><Trash2 class="size-4"
    /></Button>
  </div>
</template>
