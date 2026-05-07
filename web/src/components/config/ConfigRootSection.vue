<script setup lang="ts">
import ConfigField from '@/components/config/ConfigField.vue'
import type { ConfigRootSection } from '@/composables/useConfigForm'
import type { ConfigField as ConfigFieldSchema } from '@/types/config'

const props = defineProps<{
  id: string
  section: ConfigRootSection
  title: string
  description: string
  fields: ConfigFieldSchema[]
  values: object | null
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update-field': [key: string, value: unknown]
}>()

const fieldID = (key: string) => {
  return `config-${props.section}-${key}`
}

const fieldValue = (key: string) => {
  return (props.values as Record<string, unknown> | null)?.[key]
}
</script>

<template>
  <section :id="id" class="scroll-mt-8 space-y-4">
    <div>
      <h2 class="text-lg font-semibold text-foreground">{{ title }}</h2>
      <p class="text-sm text-muted-foreground">{{ description }}</p>
    </div>

    <div class="grid gap-4 sm:grid-cols-2">
      <ConfigField
        v-for="field in fields"
        :id="fieldID(field.key)"
        :key="field.key"
        :field="field"
        :model-value="fieldValue(field.key)"
        :disabled="disabled"
        class="space-y-2"
        @update:model-value="emit('update-field', field.key, $event)"
      />
    </div>
  </section>
</template>
