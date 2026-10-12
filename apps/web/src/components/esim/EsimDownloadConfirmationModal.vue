<script setup lang="ts">
import { watch } from 'vue'
import { useForm } from '@tanstack/vue-form'
import { useI18n } from 'vue-i18n'
import { z } from 'zod'

import ValidatedField from '@/components/ValidatedField.vue'
import EsimPersistentDialogContent from '@/components/esim/EsimPersistentDialogContent.vue'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { validateOnInteraction } from '@/lib/formValidation'

const props = defineProps<{
  open: boolean
  title: string
  hint: string
  placeholder: string
  confirmLabel: string
  cancelLabel: string
}>()

const emit = defineEmits<{
  (event: 'submit'): void
  (event: 'cancel'): void
}>()

const { t } = useI18n()
const code = defineModel<string>('code', { required: true })

const confirmationSchema = z.object({
  code: z
    .string({ error: t('modemDetail.validation.required') })
    .trim()
    .min(1, t('modemDetail.validation.required')),
})

const form = useForm({
  validationLogic: validateOnInteraction,
  validators: { onDynamic: confirmationSchema },
  defaultValues: {
    code: '',
  },
  onSubmit: ({ value }) => {
    code.value = confirmationSchema.parse(value).code
    emit('submit')
  },
})

const ConfirmationField = form.Field
const isSubmitting = form.useSelector((state) => state.isSubmitting)

const resetValues = () => {
  form.reset({ code: code.value })
}

const handleOpenChange = (nextOpen: boolean) => {
  if (!nextOpen) {
    code.value = ''
    emit('cancel')
  }
}

watch(
  () => props.open,
  (value) => {
    if (!value) {
      form.reset({ code: '' })
      return
    }
    resetValues()
  },
)
</script>

<template>
  <Dialog
    :open="props.open"
    @update:open="handleOpenChange"
  >
    <EsimPersistentDialogContent class="sm:max-w-sm">
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
        <DialogDescription>{{ hint }}</DialogDescription>
      </DialogHeader>
      <form
        class="space-y-4"
        @submit.prevent="form.handleSubmit"
      >
        <ConfirmationField
          v-slot="{ field }"
          name="code"
        >
          <ValidatedField
            v-slot="{ controlAttrs }"
            :label="placeholder"
            label-class="sr-only"
            :meta="field.state.meta"
          >
            <Input
              v-bind="controlAttrs"
              :name="field.name"
              type="text"
              :placeholder="placeholder"
              :model-value="field.state.value"
              @update:model-value="(value) => field.handleChange(String(value))"
              @blur="field.handleBlur"
            />
          </ValidatedField>
        </ConfirmationField>

        <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Button
            type="submit"
            class="order-1 w-full sm:order-2"
            :disabled="isSubmitting"
          >
            {{ confirmLabel }}
          </Button>
          <Button
            variant="ghost"
            type="button"
            class="order-2 w-full sm:order-1"
            @click="emit('cancel')"
          >
            {{ cancelLabel }}
          </Button>
        </DialogFooter>
      </form>
    </EsimPersistentDialogContent>
  </Dialog>
</template>
