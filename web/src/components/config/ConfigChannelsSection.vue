<script setup lang="ts">
import { ChevronDown } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import ConfigField from '@/components/config/ConfigField.vue'
import ConfigKeyValueField from '@/components/config/ConfigKeyValueField.vue'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Switch } from '@/components/ui/switch'
import type { ConfigChannel, ConfigChannelSchema } from '@/types/config'

const props = defineProps<{
  id: string
  title: string
  description: string
  channels: Record<string, ConfigChannel>
  disabled?: boolean
  expandedChannels: Record<string, boolean>
  schemas: ConfigChannelSchema[]
}>()

const emit = defineEmits<{
  'toggle-channel': [schema: ConfigChannelSchema, enabled: boolean]
  'toggle-details': [channel: string]
  'update-field': [channel: string, key: string, value: unknown]
}>()

const { t, te } = useI18n()

const schemaText = (value: string | undefined) => {
  return value && te(value) ? t(value) : (value ?? '')
}

const fieldID = (key: string, channel: string) => {
  return `config-channel-${channel}-${key}`
}

const channelValue = (channel: string, key: string) => {
  return props.channels[channel]?.[key as keyof ConfigChannel]
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
</script>

<template>
  <section :id="id" class="scroll-mt-8 space-y-4 md:border-t md:pt-8">
    <div>
      <h2 class="text-lg font-semibold text-foreground">{{ title }}</h2>
      <p class="text-sm text-muted-foreground">{{ description }}</p>
    </div>

    <div class="divide-y border-y">
      <Collapsible
        v-for="channel in schemas"
        :key="channel.key"
        :open="isChannelEnabled(channel.key) && isChannelExpanded(channel.key)"
        :disabled="disabled || !isChannelEnabled(channel.key)"
        class="py-4"
        @update:open="emit('toggle-details', channel.key)"
      >
        <div class="flex items-center gap-3">
          <CollapsibleTrigger as-child>
            <button
              type="button"
              class="flex min-w-0 flex-1 items-center gap-3 text-left"
              :class="isChannelEnabled(channel.key) ? 'cursor-pointer' : 'cursor-default'"
              :disabled="disabled || !isChannelEnabled(channel.key)"
              :aria-controls="fieldID('details', channel.key)"
            >
              <ChevronDown
                class="size-4 shrink-0 text-muted-foreground transition-transform"
                :class="{
                  'rotate-180': isChannelExpanded(channel.key),
                  'opacity-30': !isChannelEnabled(channel.key),
                }"
              />
              <span class="min-w-0 space-y-1">
                <span class="block text-sm font-medium text-foreground">
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
              :disabled="disabled"
              @update:model-value="emit('toggle-channel', channel, $event === true)"
            />
          </div>
        </div>

        <CollapsibleContent
          :id="fieldID('details', channel.key)"
          class="mt-4 grid gap-4 sm:grid-cols-2"
        >
          <div
            v-for="field in channel.fields"
            :key="field.key"
            class="space-y-2"
            :class="{ 'sm:col-span-2': isWideField(field.control) }"
          >
            <ConfigKeyValueField
              v-if="field.control === 'keyValue'"
              :field="field"
              :model-value="channelValue(channel.key, field.key)"
              :disabled="disabled"
              @update:model-value="emit('update-field', channel.key, field.key, $event)"
            />
            <ConfigField
              v-else
              :id="fieldID(field.key, channel.key)"
              :field="field"
              :model-value="channelValue(channel.key, field.key)"
              :disabled="disabled"
              @update:model-value="emit('update-field', channel.key, field.key, $event)"
            />
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  </section>
</template>
