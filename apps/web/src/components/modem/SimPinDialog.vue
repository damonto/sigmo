<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import type { useSimPin } from '@/composables/useSimPin'

const props = defineProps<{ controller: ReturnType<typeof useSimPin> }>()
const emit = defineEmits<{ updated: [] }>()
const { t, te } = useI18n()
const {
  status,
  open,
  busy,
  loading,
  action,
  pin,
  newPin,
  confirmation,
  errorCode,
  needsNewPin,
  canSubmit,
} = props.controller
const errorMessage = computed(() => {
  const key = `simPin.errors.${errorCode.value}`
  return errorCode.value ? t(te(key) ? key : 'simPin.errors.sim_pin_result_unknown') : ''
})
const actionLabel = computed(() => `simPin.actions.${action.value ?? 'pin'}`)
const showProtection = computed(
  () => status.value?.supported && status.value.lock === 'none' && status.value.enabled !== null,
)
const statusMessage = computed(() => {
  if (status.value && !status.value.supported) return 'simPin.errors.sim_pin_unsupported'
  const lock = status.value?.lock
  return `simPin.locks.${!lock || lock === 'none' ? 'unknown' : lock}`
})
const attemptMessage = computed(() => {
  const puk = action.value === 'puk'
  const count = puk ? status.value?.pukRetries : status.value?.pinRetries
  return count == null ? '' : t(puk ? 'simPin.pukAttempts' : 'simPin.pinAttempts', { count })
})
const credentialDescription = computed(() => {
  const ids: string[] = []
  if (attemptMessage.value) ids.push('sim-pin-attempts')
  if (errorMessage.value) ids.push('sim-pin-error')
  return ids.length ? ids.join(' ') : undefined
})
const incorrectCredential = computed(
  () => errorCode.value === 'sim_pin_incorrect' || errorCode.value === 'sim_puk_incorrect',
)
function preventWhileBusy(event: Event) {
  if (busy.value) event.preventDefault()
}
async function submit() {
  const unlocking = action.value === 'pin' || action.value === 'puk'
  if (await props.controller.submit()) {
    emit('updated')
    if (unlocking) props.controller.close()
  }
}
</script>

<template>
  <Dialog
    :open="open"
    @update:open="(value) => !value && controller.close()"
  >
    <DialogContent
      class="sm:max-w-sm outline-none"
      :show-close-button="!busy"
      @escape-key-down="preventWhileBusy"
      @interact-outside="preventWhileBusy"
    >
      <DialogHeader class="text-left">
        <DialogTitle>{{ t('simPin.title') }}</DialogTitle>
        <DialogDescription class="sr-only">{{ t('simPin.description') }}</DialogDescription>
      </DialogHeader>

      <FieldGroup
        v-if="showProtection"
        class="gap-4"
      >
        <Field
          orientation="horizontal"
          class="justify-between"
        >
          <FieldLabel for="sim-pin-protection">{{ t('simPin.protection') }}</FieldLabel>
          <Switch
            id="sim-pin-protection"
            :model-value="status?.enabled ?? false"
            :disabled="busy || loading || action !== null"
            @update:model-value="controller.choose($event ? 'enable' : 'disable')"
          />
        </Field>
        <Button
          v-if="status?.enabled && action === null"
          class="w-full"
          :disabled="busy || loading"
          @click="controller.choose('change')"
          >{{ t('simPin.actions.change') }}</Button
        >
      </FieldGroup>

      <div
        v-if="loading && !status"
        class="flex items-center gap-2 text-sm text-muted-foreground"
        role="status"
      >
        <Spinner class="size-4" />
        {{ t('simPin.loading') }}
      </div>
      <form
        v-if="action && status?.supported"
        class="space-y-4"
        @submit.prevent="submit"
      >
        <FieldGroup class="gap-4">
          <Field class="gap-2">
            <FieldLabel for="sim-pin-current">{{
              t(action === 'puk' ? 'simPin.puk' : 'simPin.currentPin')
            }}</FieldLabel>
            <Input
              id="sim-pin-current"
              v-model="pin"
              type="password"
              inputmode="numeric"
              autocomplete="off"
              :maxlength="8"
              :placeholder="t(action === 'puk' ? 'simPin.puk' : 'simPin.currentPin')"
              :disabled="busy || loading"
              :aria-describedby="credentialDescription"
              :aria-invalid="incorrectCredential || undefined"
              autofocus
            />
            <FieldDescription
              v-if="attemptMessage"
              id="sim-pin-attempts"
              >{{ attemptMessage }}</FieldDescription
            >
          </Field>
          <template v-if="needsNewPin">
            <Field class="gap-2">
              <FieldLabel for="sim-pin-new">{{ t('simPin.newPin') }}</FieldLabel>
              <Input
                id="sim-pin-new"
                v-model="newPin"
                type="password"
                inputmode="numeric"
                autocomplete="off"
                :maxlength="8"
                :placeholder="t('simPin.newPin')"
                :disabled="busy || loading"
              />
            </Field>
            <Field class="gap-2">
              <FieldLabel for="sim-pin-confirm">{{ t('simPin.confirmPin') }}</FieldLabel>
              <Input
                id="sim-pin-confirm"
                v-model="confirmation"
                type="password"
                inputmode="numeric"
                autocomplete="off"
                :maxlength="8"
                :placeholder="t('simPin.confirmPin')"
                :disabled="busy || loading"
              />
            </Field>
          </template>
        </FieldGroup>
        <p
          v-if="errorMessage"
          id="sim-pin-error"
          class="text-sm text-destructive"
          role="alert"
        >
          {{ errorMessage }}
        </p>
        <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Button
            type="submit"
            class="order-1 w-full sm:order-2"
            :disabled="!canSubmit"
          >
            <Spinner
              v-if="busy"
              class="size-4"
            />
            {{ t(actionLabel) }}
          </Button>
          <Button
            type="button"
            variant="ghost"
            class="order-2 w-full sm:order-1"
            :disabled="busy"
            @click="controller.close()"
          >
            {{ t('simPin.cancel') }}
          </Button>
        </DialogFooter>
      </form>
      <p
        v-if="!action && errorMessage"
        class="text-sm text-destructive"
        role="alert"
      >
        {{ errorMessage }}
      </p>
      <p
        v-else-if="!action && !loading && !showProtection"
        class="text-sm text-muted-foreground"
        role="status"
      >
        {{ t(statusMessage) }}
      </p>
      <DialogFooter v-if="!action">
        <Button
          variant="ghost"
          class="w-full"
          :disabled="busy"
          @click="controller.close()"
          >{{ t('simPin.cancel') }}</Button
        >
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
