<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import type { SettingsChannelSchema } from '@/types/settings'

const props = defineProps<{
  id: string
  channels: SettingsChannelSchema[]
  disabled?: boolean
}>()

const selected = defineModel<string[]>({ required: true })
const { t, te } = useI18n()

const toggleChannel = (channel: string, checked: boolean) => {
  if (props.disabled) return
  selected.value = checked
    ? [...new Set([...selected.value, channel])].sort()
    : selected.value.filter((item) => item !== channel)
}
</script>

<template>
  <div
    v-if="channels.length > 0"
    class="grid gap-3 sm:grid-cols-3"
  >
    <div
      v-for="channel in channels"
      :key="channel.key"
      class="flex items-center gap-2"
    >
      <Checkbox
        :id="`${id}-${channel.key}`"
        :model-value="selected.includes(channel.key)"
        :disabled="disabled"
        @update:model-value="toggleChannel(channel.key, $event === true)"
      />
      <Label
        :for="`${id}-${channel.key}`"
        class="cursor-pointer text-sm font-normal"
      >
        {{ te(channel.label) ? t(channel.label) : channel.label }}
      </Label>
    </div>
  </div>
  <p
    v-else
    class="text-xs text-muted-foreground"
  >
    {{ t('settings.noConfiguredChannels') }}
  </p>
</template>
