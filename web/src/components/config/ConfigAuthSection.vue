<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ConfigField from '@/components/config/ConfigField.vue'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import type {
  ConfigApp,
  ConfigChannelSchema,
  ConfigField as ConfigFieldSchema,
} from '@/types/config'

const props = defineProps<{
  app: ConfigApp | null
  enabledChannels: ConfigChannelSchema[]
  fields: ConfigFieldSchema[]
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update-field': [key: string, value: unknown]
}>()

const { t, te } = useI18n()
const authProviders = computed(() => props.app?.authProviders ?? [])

const schemaText = (value: string | undefined) => {
  return value && te(value) ? t(value) : (value ?? '')
}

const fieldID = (key: string, channel = '') => {
  return channel ? `config-app-${channel}-${key}` : `config-app-${key}`
}

const fieldValue = (key: string) => {
  if (!props.app) return undefined
  return (props.app as unknown as Record<string, unknown>)[key]
}

const isAuthProvider = (channel: string) => {
  return authProviders.value.includes(channel)
}

const toggleAuthProvider = (channel: string, enabled: boolean) => {
  if (enabled) {
    if (!isAuthProvider(channel)) {
      emit('update-field', 'authProviders', [...authProviders.value, channel].sort())
    }
    return
  }
  emit(
    'update-field',
    'authProviders',
    authProviders.value.filter((item) => item !== channel),
  )
}
</script>

<template>
  <div class="space-y-4 border-t pt-5">
    <div>
      <h3 class="text-sm font-semibold text-foreground">{{ t('config.authTitle') }}</h3>
      <p class="text-sm text-muted-foreground">{{ t('config.authDescription') }}</p>
    </div>

    <div v-for="field in fields" :key="field.key" class="space-y-2">
      <div v-if="field.control === 'channelList'" class="space-y-3">
        <Label>{{ schemaText(field.label) }}</Label>
        <div v-if="enabledChannels.length > 0" class="grid gap-3 sm:grid-cols-3">
          <div
            v-for="channel in enabledChannels"
            :key="channel.key"
            class="flex items-center gap-2"
          >
            <Checkbox
              :id="fieldID('auth_provider', channel.key)"
              :model-value="isAuthProvider(channel.key)"
              :disabled="disabled"
              @update:model-value="toggleAuthProvider(channel.key, $event === true)"
            />
            <Label
              :for="fieldID('auth_provider', channel.key)"
              class="cursor-pointer text-sm font-normal"
            >
              {{ schemaText(channel.label) }}
            </Label>
          </div>
        </div>
        <p v-else class="text-xs text-muted-foreground">
          {{ t('config.noEnabledChannels') }}
        </p>
      </div>

      <ConfigField
        v-else
        :id="fieldID(field.key)"
        :field="field"
        :model-value="fieldValue(field.key)"
        :disabled="disabled"
        @update:model-value="emit('update-field', field.key, $event)"
      />
    </div>
  </div>
</template>
