<script setup lang="ts">
import { PhoneCall, PhoneIncoming, PhoneOutgoing } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { formatListTimestamp } from '@/lib/datetime'
import type { CallRecord } from '@/types/call'
defineProps<{
  call: CallRecord
  title: string
  state: string
  route: string
  duration: string
  source?: string
  expanded?: boolean
  calling?: boolean
  callbackDisabled?: boolean
}>()
const emit = defineEmits<{ select: []; callback: [] }>()
const { t } = useI18n()
</script>
<template>
  <div
    class="rounded-lg border border-border bg-card px-4 py-4 transition-colors hover:bg-muted/40"
    role="button"
    tabindex="0"
    :aria-expanded="expanded"
    @click="emit('select')"
    @keydown.enter.self.prevent="emit('select')"
    @keydown.space.self.prevent="emit('select')"
  >
    <div class="flex min-h-12 items-center gap-3">
      <slot name="avatar">
        <span
          class="flex size-11 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground"
          aria-hidden="true"
        >
          <PhoneIncoming
            v-if="call.direction === 'incoming'"
            class="size-5"
          /><PhoneOutgoing
            v-else
            class="size-5"
          />
        </span>
      </slot>
      <span class="min-w-0 flex-1 space-y-1">
        <span class="flex items-baseline justify-between gap-3">
          <span
            class="truncate text-sm font-semibold"
            :class="call.direction === 'incoming' && !call.answeredAt ? 'text-destructive' : ''"
            >{{ title }}</span
          >
          <span class="shrink-0 text-xs text-muted-foreground">{{
            formatListTimestamp(call.updatedAt)
          }}</span>
        </span>
        <span class="flex items-baseline justify-between gap-3 text-xs text-muted-foreground">
          <span class="min-w-0 wrap-anywhere"
            >{{ state }} · {{ route }}<template v-if="source"> · {{ source }}</template></span
          >
          <span
            v-if="duration"
            class="shrink-0"
            >{{ duration }}</span
          >
        </span>
      </span>
      <Button
        size="icon"
        variant="ghost"
        class="shrink-0 rounded-full text-primary"
        :disabled="callbackDisabled"
        :aria-busy="calling"
        :aria-label="t('modemDetail.calls.callBack')"
        @click.stop="emit('callback')"
      >
        <Spinner
          v-if="calling"
          class="size-4"
        /><PhoneCall
          v-else
          class="size-4"
        />
      </Button>
    </div>
    <slot />
  </div>
</template>
