<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import SettingsChannelPicker from '@/components/settings/SettingsChannelPicker.vue'
import SettingsField from '@/components/settings/SettingsField.vue'
import { Card, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import type {
  SettingsAuth,
  SettingsChannelSchema,
  SettingsField as SettingsFieldSchema,
} from '@/types/settings'

const props = defineProps<{
  auth: SettingsAuth | null
  configuredChannels: SettingsChannelSchema[]
  fields: SettingsFieldSchema[]
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update-field': [key: string, value: unknown]
}>()

const { t, te } = useI18n()
const otpChannels = computed(() => props.auth?.otpChannels ?? [])

const schemaText = (value: string | undefined) => {
  return value && te(value) ? t(value) : (value ?? '')
}

const fieldID = (key: string) => `settings-auth-${key}`

const fieldValue = (key: string) => {
  if (!props.auth) return undefined
  return key in props.auth ? props.auth[key as keyof SettingsAuth] : undefined
}

const visibleFields = computed(() =>
  props.fields.filter((field) => {
    if (field.key === 'mode') return true
    if (props.auth?.mode === 'cloudflare_access') return field.key.startsWith('access')
    if (props.auth?.mode === 'otp') return ['sessionMaxDays', 'otpChannels'].includes(field.key)
    if (props.auth?.mode === 'oidc') return field.key.startsWith('oidc')
    return false
  }),
)
</script>

<template>
  <Card class="gap-4 border-0 py-4 shadow-sm">
    <CardContent class="space-y-4 px-4">
      <div
        v-for="field in visibleFields"
        :key="field.key"
        class="space-y-2"
      >
        <div
          v-if="field.control === 'channelList'"
          class="space-y-3"
        >
          <Label>{{ schemaText(field.label) }}</Label>
          <p
            v-if="field.description"
            class="text-xs text-muted-foreground"
          >
            {{ schemaText(field.description) }}
          </p>
          <SettingsChannelPicker
            :id="fieldID(field.key)"
            :model-value="otpChannels"
            :channels="configuredChannels"
            :disabled="disabled"
            @update:model-value="emit('update-field', 'otpChannels', $event)"
          />
        </div>

        <SettingsField
          v-else
          :id="fieldID(field.key)"
          :field="field"
          :model-value="fieldValue(field.key)"
          :disabled="disabled"
          @update:model-value="emit('update-field', field.key, $event)"
        />
      </div>
    </CardContent>
  </Card>
</template>
