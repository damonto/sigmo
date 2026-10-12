<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { Badge } from '@/components/ui/badge'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { formatReminderTimestamp } from '@/lib/datetime'
import type { Reminder } from '@/types/reminder'

const props = defineProps<{
  reminder: Reminder
  profileName: string
}>()

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
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        type="button"
        class="max-w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        :aria-label="t('modemDetail.reminder.title')"
        @pointerenter="handlePointerEnter"
        @pointerleave="handlePointerLeave"
        @focus="handleFocus"
        @blur="open = false"
      >
        <Badge variant="secondary" class="max-w-full truncate text-[10px]">
          {{ t('modemDetail.reminder.nextAt') }}: {{ nextAt }}
        </Badge>
      </button>
    </PopoverTrigger>
    <PopoverContent side="top" align="start" class="w-64 text-xs">
      <p class="font-semibold">{{ props.profileName }}</p>
      <p class="mt-1 text-muted-foreground">{{ nextAt }} · {{ repeat }}</p>
      <p class="mt-2 whitespace-pre-wrap break-words">{{ props.reminder.content }}</p>
    </PopoverContent>
  </Popover>
</template>
