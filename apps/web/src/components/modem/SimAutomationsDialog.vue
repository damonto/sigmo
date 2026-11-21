<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import { CalendarClock, Save } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import ClearScheduleButton from '@/components/ClearScheduleButton.vue'
import { Button } from '@/components/ui/button'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useSimAutomations } from '@/composables/useSimAutomations'
import { formatReminderTimestamp } from '@/lib/datetime'
import type { AutomationLine } from '@/types/automation'

const emit = defineEmits<{ changed: [] }>()
const line = defineModel<AutomationLine | undefined>({ required: true })
const { t, n } = useI18n()
const { task, form, loaded, loading, locked, busy, error, schedule, save, remove, reload } =
  useSimAutomations(
    line,
    () => emit('changed'),
    () => {
      line.value = undefined
    },
  )
const open = computed({
  get: () => line.value !== undefined,
  set: (value) => {
    if (!value && !busy.value) line.value = undefined
  },
})
const focusTarget = useTemplateRef<HTMLElement>('focusTarget')
const fieldsDisabled = computed(
  () => busy.value || locked.value || loading.value || error.value === 'conflict',
)
const repeatDayUnit = computed(() =>
  t(
    Number(form.value.repeatDays) > 1
      ? 'modemDetail.reminder.dayUnitPlural'
      : 'modemDetail.reminder.dayUnit',
  ),
)

const handleOpenAutoFocus = (event: Event) => {
  event.preventDefault()
  focusTarget.value?.focus({ preventScroll: true })
}

const result = (code: string | undefined, fallback: string) => {
  if (!code) return t(`automation.${fallback}`)
  const key = `automation.result.${code}`
  const value = t(key)
  return value === key ? t(`automation.${fallback}`) : value
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent
      class="max-h-[85dvh] overflow-y-auto sm:max-w-md"
      :show-close-button="!busy"
      @open-auto-focus="handleOpenAutoFocus"
    >
      <DialogHeader class="text-left">
        <div
          ref="focusTarget"
          tabindex="-1"
          class="space-y-2 outline-none"
        >
          <DialogTitle class="flex items-center gap-2">
            <CalendarClock
              class="size-4 text-primary"
              aria-hidden="true"
            />
            {{ t('automation.title') }}
          </DialogTitle>
          <DialogDescription>{{
            t('automation.description', { profile: line?.label })
          }}</DialogDescription>
        </div>
      </DialogHeader>
      <p
        v-if="error"
        class="text-sm text-destructive"
        role="alert"
      >
        {{ t(`automation.${error}`) }}
      </p>

      <p
        v-if="loading"
        class="flex items-center gap-2 text-sm text-muted-foreground"
        role="status"
      >
        <Spinner aria-hidden="true" />
        {{ t('automation.loading') }}
      </p>
      <form
        v-if="loaded"
        class="space-y-4"
        @submit.prevent="save"
      >
        <fieldset
          class="min-w-0 space-y-4"
          :disabled="fieldsDisabled"
        >
          <div class="space-y-2">
            <Label for="automation-type">{{ t('automation.action') }}</Label>
            <select
              id="automation-type"
              v-model="form.type"
              class="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
            >
              <option value="data">{{ t('automation.data') }}</option>
              <option value="sms">{{ t('automation.sms') }}</option>
              <option value="call">{{ t('automation.call') }}</option>
            </select>
          </div>
          <div class="space-y-2">
            <Label for="automation-time">{{ t('automation.time') }}</Label>
            <InputGroup class="overflow-hidden">
              <InputGroupInput
                id="automation-time"
                v-model="form.scheduledAt"
                type="datetime-local"
                step="60"
                :placeholder="t('modemDetail.reminder.timePlaceholder')"
                class="appearance-none dark:scheme-dark"
                required
              />
            </InputGroup>
          </div>
          <div class="space-y-2">
            <Label for="automation-repeat">{{ t('automation.repeat') }}</Label>
            <InputGroup>
              <InputGroupInput
                id="automation-repeat"
                v-model="form.repeatDays"
                type="number"
                min="1"
                max="3650"
                step="1"
                inputmode="numeric"
                :placeholder="t('modemDetail.reminder.repeatPlaceholder')"
                aria-describedby="automation-repeat-hint"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText>{{ repeatDayUnit }}</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
            <p
              id="automation-repeat-hint"
              class="text-xs text-muted-foreground"
            >
              {{ t('automation.repeatHint') }}
            </p>
          </div>
          <template v-if="form.type === 'data'">
            <div class="space-y-2">
              <Label for="automation-amount">{{ t('automation.amount') }}</Label>
              <div class="flex gap-2">
                <Input
                  id="automation-amount"
                  v-model="form.amount"
                  type="number"
                  min="0"
                  step="any"
                  required
                />
                <select
                  v-model="form.unit"
                  :aria-label="t('automation.unit')"
                  class="h-9 rounded-md border border-input bg-background px-3 text-sm"
                >
                  <option>KiB</option>
                  <option>MiB</option>
                </select>
              </div>
              <p class="text-xs text-muted-foreground">{{ t('automation.dataHint') }}</p>
            </div>
          </template>
          <template v-else>
            <div class="space-y-2">
              <Label for="automation-to">{{ t('automation.number') }}</Label>
              <Input
                id="automation-to"
                v-model="form.to"
                type="tel"
                required
              />
            </div>
            <div
              v-if="form.type === 'sms'"
              class="space-y-2"
            >
              <Label for="automation-text">{{ t('automation.message') }}</Label>
              <Textarea
                id="automation-text"
                v-model="form.text"
                required
              />
            </div>
            <div
              v-else
              class="space-y-2"
            >
              <Label for="automation-duration">{{ t('automation.duration') }}</Label>
              <Input
                id="automation-duration"
                v-model="form.duration"
                type="number"
                min="1"
                max="300"
                step="1"
                required
              />
              <p class="text-xs text-muted-foreground">{{ t('automation.callHint') }}</p>
            </div>
            <div class="space-y-2">
              <div class="flex items-center justify-between gap-4">
                <Label for="automation-wifi-calling-only">{{
                  t('automation.wifiCallingOnly')
                }}</Label>
                <Switch
                  id="automation-wifi-calling-only"
                  v-model="form.wifiCallingOnly"
                  aria-describedby="automation-wifi-calling-only-hint"
                />
              </div>
              <div
                id="automation-wifi-calling-only-hint"
                class="space-y-1 text-xs text-muted-foreground"
              >
                <p>{{ t('automation.wifiCallingOnlyHint') }}</p>
                <p v-if="form.type === 'sms'">{{ t('automation.wifiCallingOnlySmsHint') }}</p>
              </div>
            </div>
          </template>
        </fieldset>
        <div
          v-if="task?.lastRun"
          class="space-y-1 text-xs text-muted-foreground"
        >
          <p>
            {{
              t('automation.last', {
                time: formatReminderTimestamp(task.lastRun.finishedAt),
                result: result(task.lastRun.code, task.lastRun.status),
              })
            }}
          </p>
          <p v-if="task.lastRun.bytesReceived !== undefined">
            {{ t('automation.received', { bytes: n(task.lastRun.bytesReceived) }) }}
          </p>
          <p v-if="task.lastRun.restoreCode">{{ result(task.lastRun.restoreCode, 'failed') }}</p>
        </div>
        <p
          v-if="locked"
          class="text-sm text-muted-foreground"
          role="status"
        >
          {{ t(task?.restoring ? 'automation.restoring' : 'automation.running') }}
        </p>
        <DialogFooter class="gap-2 sm:justify-between">
          <ClearScheduleButton
            v-if="task"
            :label="t('automation.clear')"
            :title="t('automation.deletePrompt')"
            :description="t('automation.clearDescription', { profile: line?.label })"
            :cancel-label="t('automation.cancel')"
            :disabled="fieldsDisabled"
            @clear="remove"
          />
          <span v-else />
          <div class="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              :disabled="busy"
              @click="open = false"
            >
              {{ t('automation.cancel') }}
            </Button>
            <Button
              v-if="locked || error === 'conflict'"
              type="button"
              variant="outline"
              :disabled="loading"
              @click="reload"
            >
              {{ t('automation.refresh') }}
            </Button>
            <Button
              type="submit"
              :disabled="fieldsDisabled || !schedule"
            >
              <Save
                class="size-4"
                aria-hidden="true"
              />
              {{ t('automation.save') }}
            </Button>
          </div>
        </DialogFooter>
      </form>
      <Button
        v-else-if="!loading"
        variant="outline"
        class="justify-self-end"
        @click="reload"
      >
        {{ t('automation.refresh') }}
      </Button>
    </DialogContent>
  </Dialog>
</template>
