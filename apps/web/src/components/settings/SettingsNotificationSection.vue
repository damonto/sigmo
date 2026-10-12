<script setup lang="ts">
import { ChevronDown } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import SettingsField from '@/components/settings/SettingsField.vue'
import SettingsKeyValueField from '@/components/settings/SettingsKeyValueField.vue'
import SettingsSaveButton from '@/components/settings/SettingsSaveButton.vue'
import { Card } from '@/components/ui/card'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Switch } from '@/components/ui/switch'
import type { SettingsChannel, SettingsChannelSchema } from '@/types/settings'

const props = defineProps<{
  channels: Record<string, SettingsChannel>
  disabled?: boolean
  expandedChannels: Record<string, boolean>
  savingChannels: Record<string, boolean>
  schemas: SettingsChannelSchema[]
}>()

const emit = defineEmits<{
  'toggle-channel': [schema: SettingsChannelSchema, enabled: boolean]
  'toggle-details': [channel: string]
  'update-field': [channel: string, key: string, value: unknown]
  'save-channel': [channel: string]
}>()

const { t, te } = useI18n()

const schemaText = (value: string | undefined) => {
  return value && te(value) ? t(value) : (value ?? '')
}

const fieldID = (key: string, channel: string) => {
  return `settings-channel-${channel}-${key}`
}

const channelValue = (channel: string, key: string) => {
  return props.channels[channel]?.[key as keyof SettingsChannel]
}

const isChannelEnabled = (channel: string) => {
  return props.channels[channel]?.enabled === true
}

const isChannelExpanded = (channel: string) => {
  return props.expandedChannels[channel] === true
}

const isWideField = (control: string) => {
  return control === 'list' || control === 'keyValue'
}

const isFullWidthField = (fields: SettingsChannelSchema['fields'], index: number) => {
  if (isWideField(fields[index]?.control ?? '')) return true

  let start = index
  while (start > 0 && !isWideField(fields[start - 1]?.control ?? '')) start -= 1

  let end = index
  while (end < fields.length - 1 && !isWideField(fields[end + 1]?.control ?? '')) end += 1

  return index === end && (end - start + 1) % 2 === 1
}

const isChannelSaving = (channel: string) => {
  return props.savingChannels[channel] === true
}

const isChannelDisabled = (channel: string) => {
  return props.disabled === true || isChannelSaving(channel)
}
</script>

<template>
  <div class="space-y-3">
    <Card
      v-for="channel in schemas"
      :key="channel.key"
      :data-channel="channel.key"
      class="gap-0 overflow-hidden border-0 py-0 shadow-sm"
    >
      <Collapsible
        :open="isChannelExpanded(channel.key)"
        :disabled="isChannelDisabled(channel.key)"
        @update:open="emit('toggle-details', channel.key)"
      >
        <div class="flex items-start gap-3 p-4">
          <CollapsibleTrigger as-child>
            <button
              type="button"
              class="flex min-w-0 flex-1 cursor-pointer items-start gap-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-default"
              :disabled="isChannelDisabled(channel.key)"
              :aria-controls="fieldID('details', channel.key)"
            >
              <ChevronDown
                class="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform"
                :class="{ 'rotate-180': isChannelExpanded(channel.key) }"
              />
              <span class="min-w-0 space-y-1">
                <span class="block text-sm font-semibold text-foreground">
                  {{ schemaText(channel.label) }}
                </span>
                <span
                  v-if="schemaText(channel.description)"
                  class="block text-xs leading-5 text-muted-foreground"
                >
                  {{ schemaText(channel.description) }}
                </span>
              </span>
            </button>
          </CollapsibleTrigger>

          <div class="shrink-0" @click.stop>
            <Switch
              :id="fieldID('enabled', channel.key)"
              :model-value="isChannelEnabled(channel.key)"
              :disabled="isChannelDisabled(channel.key)"
              @update:model-value="emit('toggle-channel', channel, $event === true)"
            />
          </div>
        </div>

        <CollapsibleContent
          :id="fieldID('details', channel.key)"
          class="grid gap-4 border-t px-4 py-4 sm:grid-cols-2"
        >
          <div
            v-for="(field, index) in channel.fields"
            :key="field.key"
            :data-field="field.key"
            class="space-y-2"
            :class="{ 'sm:col-span-2': isFullWidthField(channel.fields, index) }"
          >
            <SettingsKeyValueField
              v-if="field.control === 'keyValue'"
              :field="field"
              :model-value="channelValue(channel.key, field.key)"
              :disabled="isChannelDisabled(channel.key)"
              @update:model-value="emit('update-field', channel.key, field.key, $event)"
            />
            <SettingsField
              v-else
              :id="fieldID(field.key, channel.key)"
              :field="field"
              :model-value="channelValue(channel.key, field.key)"
              :disabled="isChannelDisabled(channel.key)"
              @update:model-value="emit('update-field', channel.key, field.key, $event)"
            />
          </div>

          <div class="flex justify-end border-t pt-4 sm:col-span-2">
            <SettingsSaveButton
              :id="fieldID('save', channel.key)"
              class="w-full sm:w-auto"
              :disabled="isChannelDisabled(channel.key)"
              :saving="isChannelSaving(channel.key)"
              @save="emit('save-channel', channel.key)"
            />
          </div>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  </div>
</template>
