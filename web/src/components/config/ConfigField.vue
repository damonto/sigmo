<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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

const { t, te } = useI18n()
const tagsInputDelimiter = /[,\n\r\t]+/
const inputType = computed(() => (props.field.control === 'password' ? 'password' : 'text'))
const schemaText = (value: string | undefined) => {
  return value && te(value) ? t(value) : (value ?? '')
}
const fieldLabel = computed(() => schemaText(props.field.label))
const fieldDescription = computed(() => schemaText(props.field.description))
const fieldPlaceholder = computed(() => schemaText(props.field.placeholder))

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

const updateSelection = (value: unknown) => {
  if (typeof value !== 'string') return
  emit('update:modelValue', value)
}
</script>

<template>
  <div v-if="field.control === 'switch'" class="flex items-center justify-between gap-4">
    <div class="min-w-0 space-y-1">
      <Label :for="id">{{ fieldLabel }}</Label>
      <p v-if="fieldDescription" class="text-xs leading-5 text-muted-foreground">
        {{ fieldDescription }}
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
    <Label :for="id">{{ fieldLabel }}</Label>
    <Select
      :model-value="stringValue(modelValue)"
      :disabled="disabled"
      @update:model-value="updateSelection"
    >
      <SelectTrigger :id="id" class="w-full">
        <SelectValue :placeholder="fieldPlaceholder || fieldLabel" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem v-for="option in field.options" :key="option.value" :value="option.value">
          {{ schemaText(option.label) }}
        </SelectItem>
      </SelectContent>
    </Select>
    <p v-if="fieldDescription" class="text-xs text-muted-foreground">
      {{ fieldDescription }}
    </p>
  </div>

  <div v-else class="space-y-2">
    <Label :for="id">{{ fieldLabel }}</Label>
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
      <TagsInputInput :placeholder="fieldPlaceholder" class="min-w-24" />
    </TagsInput>
    <Input
      v-else-if="field.control === 'text' || field.control === 'password'"
      :id="id"
      :type="inputType"
      :placeholder="fieldPlaceholder"
      :model-value="stringValue(modelValue)"
      :disabled="disabled"
      @update:model-value="emit('update:modelValue', String($event))"
    />
    <p v-else class="text-xs text-destructive">Unsupported control: {{ field.control }}</p>
    <p v-if="fieldDescription" class="text-xs text-muted-foreground">
      {{ fieldDescription }}
    </p>
  </div>
</template>
