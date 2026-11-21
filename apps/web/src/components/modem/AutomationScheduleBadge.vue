<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Badge } from '@/components/ui/badge'
import { formatReminderTimestamp } from '@/lib/datetime'
import type { ScheduledAutomation } from '@/types/automation'

const props = defineProps<{ automation: ScheduledAutomation }>()
const emit = defineEmits<{ edit: [] }>()
const { t } = useI18n()
</script>

<template>
  <button
    type="button"
    class="max-w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
    @click="emit('edit')"
  >
    <Badge
      variant="secondary"
      class="max-w-full flex-wrap gap-x-1.5 gap-y-0 whitespace-normal"
    >
      <span>{{ t(`automation.${props.automation.actionType}`) }}</span>
      <time
        v-if="props.automation.nextAt"
        :datetime="props.automation.nextAt"
        class="font-normal text-muted-foreground"
      >
        {{ t('automation.next', { time: formatReminderTimestamp(props.automation.nextAt) }) }}
      </time>
    </Badge>
  </button>
</template>
