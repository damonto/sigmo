<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { computed, useId } from 'vue'

import { Field, FieldError, FieldLabel } from '@/components/ui/field'

interface ValidationMeta {
  isTouched: boolean
  isValid: boolean
  errors: Array<string | { message: string | undefined } | undefined>
}

// The slot owns value bindings; this component only connects labels and errors.
const props = defineProps<{
  id?: string
  label: string
  labelClass?: HTMLAttributes['class']
  meta: ValidationMeta
}>()

const generatedId = useId()
const fieldId = computed(() => props.id ?? generatedId)
const labelId = computed(() => `${fieldId.value}-label`)
const errorId = computed(() => `${fieldId.value}-error`)
const invalid = computed(() => props.meta.isTouched && !props.meta.isValid)
const controlAttrs = computed(() => ({
  id: fieldId.value,
  'aria-labelledby': labelId.value,
  'aria-invalid': invalid.value,
  'aria-describedby': invalid.value ? errorId.value : undefined,
}))
</script>

<template>
  <Field
    class="gap-2"
    :data-invalid="invalid"
  >
    <FieldLabel
      :id="labelId"
      :for="fieldId"
      :class="labelClass"
      >{{ label }}</FieldLabel
    >
    <slot :control-attrs="controlAttrs" />
    <FieldError
      v-if="invalid"
      :id="errorId"
      :errors="meta.errors"
    />
  </Field>
</template>
