<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useForm } from '@tanstack/vue-form'
import { Bell, CalendarClock, Save, Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { z } from 'zod'

import ValidatedField from '@/components/ValidatedField.vue'
import { Button } from '@/components/ui/button'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@/components/ui/input-group'
import { Textarea } from '@/components/ui/textarea'
import { Spinner } from '@/components/ui/spinner'
import { dateTimeLocalToISOString, formatDateTimeLocal } from '@/lib/datetime'
import { validateOnInteraction } from '@/lib/formValidation'
import type { Reminder, ReminderPayload } from '@/types/reminder'

const props = withDefaults(
  defineProps<{
    profileName: string
    reminder?: Reminder | null
    saving?: boolean
    deleting?: boolean
  }>(),
  {
    reminder: null,
    saving: false,
    deleting: false,
  },
)

const emit = defineEmits<{
  (event: 'save', payload: ReminderPayload): void
  (event: 'clear'): void
}>()

const open = defineModel<boolean>('open', { required: true })

const { t } = useI18n()
const maxRepeatDays = 3650

const schema = z.object({
  scheduledAt: z.string().min(1, t('modemDetail.reminder.validation.timeRequired')),
  repeatDays: z.union([z.string(), z.number()]).refine((value) => {
    const text = String(value)
    return text === '' || (/^[1-9]\d*$/.test(text) && Number(text) <= maxRepeatDays)
  }, t('modemDetail.reminder.validation.repeat')),
  content: z.string().trim().min(1, t('modemDetail.reminder.validation.contentRequired')),
})

type FormValues = z.input<typeof schema>

const formValues = (reminder?: Reminder | null): FormValues => ({
  scheduledAt: reminder ? formatDateTimeLocal(reminder.nextAt) : '',
  repeatDays: reminder?.repeatDays ? String(reminder.repeatDays) : '',
  content: reminder?.content ?? '',
})

const form = useForm({
  validationLogic: validateOnInteraction,
  validators: { onDynamic: schema },
  defaultValues: formValues(props.reminder),
  onSubmit: ({ value }) => {
    const values = schema.parse(value)
    const scheduled = dateTimeLocalToISOString(values.scheduledAt)
    if (!scheduled) return
    const repeatText = String(values.repeatDays).trim()
    const repeat = repeatText === '' ? null : Number(repeatText)
    emit('save', {
      scheduledAt: scheduled,
      repeatDays: repeat,
      content: values.content,
    })
  },
})

const ReminderField = form.Field
const repeatDays = form.useSelector((state) => state.values.repeatDays)

const clearOpen = ref(false)
const focusTarget = useTemplateRef<HTMLElement>('focusTarget')
const busy = computed(() => props.saving || props.deleting)
const repeatDayUnit = computed(() => {
  const days = Number(String(repeatDays.value).trim())
  return t(days > 1 ? 'modemDetail.reminder.dayUnitPlural' : 'modemDetail.reminder.dayUnit')
})
const dialogOpen = computed({
  get: () => open.value,
  set: (value: boolean) => {
    if (busy.value) return
    open.value = value
  },
})

const confirmClear = () => {
  clearOpen.value = false
  emit('clear')
}

const handleOpenAutoFocus = (event: Event) => {
  event.preventDefault()
  focusTarget.value?.focus({ preventScroll: true })
}

watch(
  () => [open.value, props.reminder] as const,
  ([isOpen]) => {
    if (!isOpen) return
    form.reset(formValues(props.reminder))
  },
  { deep: true },
)
</script>

<template>
  <Dialog v-model:open="dialogOpen">
    <DialogContent
      :show-close-button="!busy"
      class="sm:max-w-md"
      @open-auto-focus="handleOpenAutoFocus"
    >
      <DialogHeader class="text-left">
        <div
          ref="focusTarget"
          tabindex="-1"
          class="space-y-2 outline-none"
        >
          <DialogTitle class="flex items-center gap-2">
            <Bell class="size-4 text-primary" />
            {{ t('modemDetail.reminder.title') }}
          </DialogTitle>
          <DialogDescription>
            {{ t('modemDetail.reminder.description', { profile: props.profileName }) }}
          </DialogDescription>
        </div>
      </DialogHeader>

      <form
        class="space-y-4 **:data-[slot=field-error]:text-xs"
        @submit.prevent="form.handleSubmit"
      >
        <ReminderField
          v-slot="{ field }"
          name="scheduledAt"
        >
          <ValidatedField
            id="reminder-time"
            v-slot="{ controlAttrs }"
            :label="t('modemDetail.reminder.time')"
            :meta="field.state.meta"
          >
            <InputGroup
              data-testid="reminder-time-group"
              class="overflow-hidden"
            >
              <InputGroupInput
                v-bind="controlAttrs"
                :name="field.name"
                :model-value="field.state.value"
                type="datetime-local"
                step="60"
                :placeholder="t('modemDetail.reminder.timePlaceholder')"
                class="appearance-none [&::-webkit-calendar-picker-indicator]:opacity-0"
                :disabled="busy"
                @update:model-value="(value: string | number) => field.handleChange(String(value))"
                @blur="field.handleBlur"
              />
              <InputGroupAddon
                align="inline-end"
                aria-hidden="true"
              >
                <CalendarClock class="size-4" />
              </InputGroupAddon>
            </InputGroup>
          </ValidatedField>
        </ReminderField>

        <ReminderField
          v-slot="{ field }"
          name="repeatDays"
        >
          <ValidatedField
            id="reminder-repeat"
            v-slot="{ controlAttrs }"
            :label="t('modemDetail.reminder.repeat')"
            :meta="field.state.meta"
          >
            <InputGroup>
              <InputGroupInput
                v-bind="controlAttrs"
                :name="field.name"
                :model-value="field.state.value"
                type="number"
                min="1"
                :max="maxRepeatDays"
                step="1"
                inputmode="numeric"
                :placeholder="t('modemDetail.reminder.repeatPlaceholder')"
                :disabled="busy"
                @update:model-value="field.handleChange"
                @blur="field.handleBlur"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText data-testid="reminder-repeat-unit">
                  {{ repeatDayUnit }}
                </InputGroupText>
              </InputGroupAddon>
            </InputGroup>
          </ValidatedField>
        </ReminderField>

        <ReminderField
          v-slot="{ field }"
          name="content"
        >
          <ValidatedField
            id="reminder-content"
            v-slot="{ controlAttrs }"
            :label="t('modemDetail.reminder.content')"
            :meta="field.state.meta"
          >
            <Textarea
              v-bind="controlAttrs"
              :name="field.name"
              :model-value="field.state.value"
              :placeholder="t('modemDetail.reminder.contentPlaceholder')"
              :disabled="busy"
              @update:model-value="(value) => field.handleChange(String(value))"
              @blur="field.handleBlur"
            />
          </ValidatedField>
        </ReminderField>

        <DialogFooter class="gap-2 sm:justify-between">
          <Button
            v-if="props.reminder"
            type="button"
            variant="ghost"
            class="text-destructive hover:text-destructive"
            :disabled="busy"
            @click="clearOpen = true"
          >
            <Trash2 class="size-4" />
            {{ t('modemDetail.reminder.clear') }}
          </Button>
          <span v-else />
          <div class="flex gap-2">
            <Button
              type="button"
              variant="outline"
              :disabled="busy"
              @click="open = false"
            >
              {{ t('modemDetail.actions.cancel') }}
            </Button>
            <Button
              type="submit"
              :disabled="busy"
            >
              <Spinner
                v-if="props.saving"
                class="size-4"
              />
              <Save
                v-else
                class="size-4"
              />
              {{ t('modemDetail.reminder.save') }}
            </Button>
          </div>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>

  <AlertDialog v-model:open="clearOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ t('modemDetail.reminder.clearTitle') }}</AlertDialogTitle>
        <AlertDialogDescription>
          {{ t('modemDetail.reminder.clearDescription') }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>{{ t('modemDetail.actions.cancel') }}</AlertDialogCancel>
        <AlertDialogAction
          class="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          @click="confirmClear"
        >
          {{ t('modemDetail.reminder.clear') }}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
