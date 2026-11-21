<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { Badge } from '@/components/ui/badge'
import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover'
import { formatReminderTimestamp } from '@/lib/datetime'
import type { Reminder } from '@/types/reminder'

const props = defineProps<{
  reminder: Reminder
  profileName: string
}>()
const emit = defineEmits<{ edit: [] }>()

const { t } = useI18n()
const open = ref(false)
const nextAt = computed(() => formatReminderTimestamp(props.reminder.nextAt))
const repeat = computed(() =>
  props.reminder.repeatDays
    ? t('modemDetail.reminder.everyDays', { days: props.reminder.repeatDays })
    : t('modemDetail.reminder.once'),
)

const handlePointerEnter = (event: PointerEvent) => {
  if (event.pointerType === 'mouse') open.value = true
}

const handlePointerLeave = (event: PointerEvent) => {
  if (event.pointerType === 'mouse') open.value = false
}

const handleFocus = (event: FocusEvent) => {
  const target = event.currentTarget
  if (target instanceof HTMLElement && target.matches(':focus-visible')) open.value = true
}

const editReminder = () => {
  open.value = false
  emit('edit')
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverAnchor as-child>
      <button
        type="button"
        class="max-w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        :aria-label="t('modemDetail.reminder.title')"
        aria-haspopup="dialog"
        @pointerenter="handlePointerEnter"
        @pointerleave="handlePointerLeave"
        @focus="handleFocus"
        @blur="open = false"
        @click="editReminder"
      >
        <Badge
          variant="secondary"
          class="max-w-full truncate text-[10px]"
        >
          {{ t('modemDetail.reminder.nextAt') }}: {{ nextAt }}
        </Badge>
      </button>
    </PopoverAnchor>
    <PopoverContent
      side="top"
      align="start"
      class="w-64 text-xs"
      @open-auto-focus.prevent
      @close-auto-focus.prevent
    >
      <p class="font-semibold">{{ props.profileName }}</p>
      <p class="mt-1 text-muted-foreground">{{ nextAt }} · {{ repeat }}</p>
      <p class="mt-2 whitespace-pre-wrap break-words">{{ props.reminder.content }}</p>
    </PopoverContent>
  </Popover>
</template>
