<script setup lang="ts">
import { computed } from 'vue'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  TagsInput,
  TagsInputInput,
  TagsInputItem,
  TagsInputItemDelete,
  TagsInputItemText,
} from '@/components/ui/tags-input'
import type { ConfigField as ConfigFieldSchema } from '@/types/config'

const props = defineProps<{
  id?: string
  field: ConfigFieldSchema
  modelValue: unknown
  disabled?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: unknown]
}>()

const tagsInputDelimiter = /[,\n\r\t]+/
const inputType = computed(() => (props.field.control === 'password' ? 'password' : 'text'))

const stringValue = (value: unknown) => {
  if (value === null || value === undefined) return ''
  return String(value)
}

const boolValue = (value: unknown) => {
  return value === true
}

const numberValue = (value: unknown) => {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  const parsed = Number.parseInt(String(value ?? ''), 10)
  return Number.isFinite(parsed) ? parsed : 0
}

const listValue = (value: unknown) => {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item))
}

const cleanList = (value: unknown[]) => {
  return value.map((line) => String(line).trim()).filter((line) => line.length > 0)
}

const eventValue = (event: Event) => {
  return (event.target as HTMLSelectElement).value
}
</script>

<template>
  <div v-if="field.control === 'switch'" class="flex items-center justify-between gap-4">
    <div class="min-w-0 space-y-1">
      <Label :for="id">{{ field.label }}</Label>
      <p v-if="field.description" class="text-xs leading-5 text-muted-foreground">
        {{ field.description }}
      </p>
    </div>
    <Switch
      :id="id"
      :model-value="boolValue(modelValue)"
      :disabled="disabled"
      @update:model-value="emit('update:modelValue', $event === true)"
    />
  </div>

  <div v-else-if="field.control === 'select'" class="space-y-2">
    <Label :for="id">{{ field.label }}</Label>
    <select
      :id="id"
      class="border-input bg-background focus-visible:border-ring focus-visible:ring-ring/50 h-9 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-[3px]"
      :value="stringValue(modelValue)"
      :disabled="disabled"
      @change="emit('update:modelValue', eventValue($event))"
    >
      <option v-for="option in field.options" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
    <p v-if="field.description" class="text-xs text-muted-foreground">
      {{ field.description }}
    </p>
  </div>

  <div v-else class="space-y-2">
    <Label :for="id">{{ field.label }}</Label>
    <Input
      v-if="field.control === 'number'"
      :id="id"
      type="number"
      :min="field.min"
      :max="field.max"
      :model-value="numberValue(modelValue)"
      :disabled="disabled"
      @update:model-value="emit('update:modelValue', numberValue($event))"
    />
    <TagsInput
      v-else-if="field.control === 'list'"
      :id="id"
      :model-value="listValue(modelValue)"
      :disabled="disabled"
      :delimiter="tagsInputDelimiter"
      add-on-blur
      add-on-paste
      add-on-tab
      @update:model-value="emit('update:modelValue', cleanList($event))"
    >
      <TagsInputItem v-for="item in listValue(modelValue)" :key="item" :value="item">
        <TagsInputItemText />
        <TagsInputItemDelete />
      </TagsInputItem>
      <TagsInputInput :placeholder="field.placeholder" class="min-w-24" />
    </TagsInput>
    <Input
      v-else-if="field.control === 'text' || field.control === 'password'"
      :id="id"
      :type="inputType"
      :placeholder="field.placeholder"
      :model-value="stringValue(modelValue)"
      :disabled="disabled"
      @update:model-value="emit('update:modelValue', String($event))"
    />
    <p v-else class="text-xs text-destructive">Unsupported control: {{ field.control }}</p>
    <p v-if="field.description" class="text-xs text-muted-foreground">
      {{ field.description }}
    </p>
  </div>
</template>
