<script setup lang="ts">
import { Bell, Save, Trash2 } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useForm } from 'vee-validate'
import { toTypedSchema } from '@vee-validate/zod'
import { useI18n } from 'vue-i18n'
import * as z from 'zod'

import { dateTimeLocalToISOString, formatDateTimeLocal } from '@/lib/datetime'
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
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@/components/ui/input-group'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Spinner } from '@/components/ui/spinner'
import type { Reminder, ReminderPayload } from '@/types/reminder'

const open = defineModel<boolean>('open', { required: true })

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

const { t } = useI18n()
const maxRepeatDays = 3650

const schema = toTypedSchema(
  z.object({
    scheduledAt: z.string().min(1, t('modemDetail.reminder.validation.timeRequired')),
    repeatDays: z.union([z.string(), z.number()]).refine((value) => {
      const text = String(value)
      return text === '' || (/^[1-9]\d*$/.test(text) && Number(text) <= maxRepeatDays)
    }, t('modemDetail.reminder.validation.repeat')),
    content: z.string().trim().min(1, t('modemDetail.reminder.validation.contentRequired')),
  }),
)

type FormValues = {
  scheduledAt: string
  repeatDays: string | number
  content: string
}

const formValues = (reminder?: Reminder | null): FormValues => ({
  scheduledAt: reminder ? formatDateTimeLocal(reminder.nextAt) : '',
  repeatDays: reminder?.repeatDays ? String(reminder.repeatDays) : '',
  content: reminder?.content ?? '',
})

const { defineField, errors, handleSubmit, resetForm } = useForm<FormValues>({
  validationSchema: schema,
  initialValues: formValues(props.reminder),
})

const [scheduledAt] = defineField('scheduledAt')
const [repeatDays] = defineField('repeatDays')
const [content] = defineField('content')

const clearOpen = ref(false)
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

const save = handleSubmit((values) => {
  const scheduled = dateTimeLocalToISOString(values.scheduledAt)
  if (!scheduled) return
  const repeatText = String(values.repeatDays).trim()
  const repeat = repeatText === '' ? null : Number(repeatText)
  emit('save', {
    scheduledAt: scheduled,
    repeatDays: repeat,
    content: values.content.trim(),
  })
})

const confirmClear = () => {
  clearOpen.value = false
  emit('clear')
}

watch(
  () => [open.value, props.reminder] as const,
  ([isOpen]) => {
    if (!isOpen) return
    resetForm({ values: formValues(props.reminder) })
  },
  { deep: true },
)
</script>

<template>
  <Dialog v-model:open="dialogOpen">
    <DialogContent :show-close-button="!busy" class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle class="flex items-center gap-2">
          <Bell class="size-4 text-primary" />
          {{ t('modemDetail.reminder.title') }}
        </DialogTitle>
        <DialogDescription>
          {{ t('modemDetail.reminder.description', { profile: props.profileName }) }}
        </DialogDescription>
      </DialogHeader>

      <form class="space-y-4" @submit.prevent="save">
        <div class="space-y-2">
          <Label for="reminder-time">{{ t('modemDetail.reminder.time') }}</Label>
          <Input
            id="reminder-time"
            v-model="scheduledAt"
            type="datetime-local"
            step="60"
            :disabled="busy"
            :aria-invalid="Boolean(errors.scheduledAt)"
          />
          <p v-if="errors.scheduledAt" class="text-xs text-destructive">
            {{ errors.scheduledAt }}
          </p>
        </div>

        <div class="space-y-2">
          <Label for="reminder-repeat">{{ t('modemDetail.reminder.repeat') }}</Label>
          <InputGroup>
            <InputGroupInput
              id="reminder-repeat"
              v-model="repeatDays"
              type="number"
              min="1"
              :max="maxRepeatDays"
              step="1"
              inputmode="numeric"
              :placeholder="t('modemDetail.reminder.repeatPlaceholder')"
              :disabled="busy"
              :aria-invalid="Boolean(errors.repeatDays)"
            />
            <InputGroupAddon align="inline-end">
              <InputGroupText data-testid="reminder-repeat-unit">
                {{ repeatDayUnit }}
              </InputGroupText>
            </InputGroupAddon>
          </InputGroup>
          <p v-if="errors.repeatDays" class="text-xs text-destructive">
            {{ errors.repeatDays }}
          </p>
        </div>

        <div class="space-y-2">
          <Label for="reminder-content">{{ t('modemDetail.reminder.content') }}</Label>
          <Textarea
            id="reminder-content"
            v-model="content"
            :placeholder="t('modemDetail.reminder.contentPlaceholder')"
            :disabled="busy"
            :aria-invalid="Boolean(errors.content)"
          />
          <p v-if="errors.content" class="text-xs text-destructive">
            {{ errors.content }}
          </p>
        </div>

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
            <Button type="button" variant="outline" :disabled="busy" @click="open = false">
              {{ t('modemDetail.actions.cancel') }}
            </Button>
            <Button type="submit" :disabled="busy">
              <Spinner v-if="props.saving" class="size-4" />
              <Save v-else class="size-4" />
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
