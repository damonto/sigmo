<script setup lang="ts">
import { Plus, Trash2 } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { ConfigField } from '@/types/config'

const props = defineProps<{
  field: ConfigField
  modelValue: unknown
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: Record<string, string>]
}>()

const { t } = useI18n()

const entries = computed(() =>
  Object.entries(keyValueMap()).map(([key, value]) => ({
    key,
    value,
  })),
)

const keyValueMap = () => {
  if (
    !props.modelValue ||
    typeof props.modelValue !== 'object' ||
    Array.isArray(props.modelValue)
  ) {
    return {}
  }
  return { ...(props.modelValue as Record<string, string>) }
}

const setKey = (oldKey: string, nextKey: string | number) => {
  const current = keyValueMap()
  const value = current[oldKey] ?? ''
  delete current[oldKey]
  const trimmed = String(nextKey).trim()
  if (trimmed) current[trimmed] = value
  emit('update:modelValue', current)
}

const setValue = (key: string, value: string | number) => {
  const current = keyValueMap()
  current[key] = String(value)
  emit('update:modelValue', current)
}

const addEntry = () => {
  const current = keyValueMap()
  let index = 1
  let key = 'Header'
  while (current[key] !== undefined) {
    index += 1
    key = `Header-${index}`
  }
  current[key] = ''
  emit('update:modelValue', current)
}

const removeEntry = (key: string) => {
  const current = keyValueMap()
  delete current[key]
  emit('update:modelValue', current)
}
</script>

<template>
  <div class="space-y-2">
    <div class="flex items-center justify-between gap-3">
      <Label>{{ field.label }}</Label>
      <Button type="button" variant="outline" size="sm" :disabled="disabled" @click="addEntry">
        <Plus class="size-4" />
        {{ t('config.addHeader') }}
      </Button>
    </div>
    <div class="space-y-2">
      <div v-for="entry in entries" :key="entry.key" class="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <Input
          :model-value="entry.key"
          :disabled="disabled"
          @update:model-value="setKey(entry.key, $event)"
        />
        <Input
          :model-value="entry.value"
          :disabled="disabled"
          @update:model-value="setValue(entry.key, $event)"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon"
          class="justify-self-end sm:justify-self-auto"
          :disabled="disabled"
          :title="t('config.removeHeader')"
          @click="removeEntry(entry.key)"
        >
          <Trash2 class="size-4" />
        </Button>
      </div>
    </div>
    <p v-if="field.description" class="text-xs text-muted-foreground">
      {{ field.description }}
    </p>
  </div>
</template>
