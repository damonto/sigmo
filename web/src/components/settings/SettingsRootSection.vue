<script setup lang="ts">
import SettingsField from '@/components/settings/SettingsField.vue'
import { Card, CardContent } from '@/components/ui/card'
import type { SettingsRootSection } from '@/composables/useSettingsForm'
import type { SettingsField as SettingsFieldSchema } from '@/types/settings'

const props = defineProps<{
  section: SettingsRootSection
  fields: SettingsFieldSchema[]
  values: object | null
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update-field': [key: string, value: unknown]
}>()

const fieldID = (key: string) => {
  return `settings-${props.section}-${key}`
}

const fieldValue = (key: string) => {
  return (props.values as Record<string, unknown> | null)?.[key]
}
</script>

<template>
  <Card class="gap-4 border-0 py-4 shadow-sm">
    <CardContent class="grid gap-4 px-4 sm:grid-cols-2">
      <SettingsField
        v-for="field in fields"
        :id="fieldID(field.key)"
        :key="field.key"
        :field="field"
        :model-value="fieldValue(field.key)"
        :disabled="disabled"
        class="space-y-2"
        @update:model-value="emit('update-field', field.key, $event)"
      />
    </CardContent>
  </Card>
</template>
