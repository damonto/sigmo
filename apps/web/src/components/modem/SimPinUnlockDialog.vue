<script setup lang="ts">
import { Loader2 } from 'lucide-vue-next'
import { computed, watch } from 'vue'
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
import { Input } from '@/components/ui/input'

const props = defineProps<{
  isSubmitting: boolean
  error: string
  lockType: string
}>()

const emit = defineEmits<{
  (event: 'submit'): void
  (event: 'cancel'): void
}>()

const { t } = useI18n()
const open = defineModel<boolean>('open', { required: true })
const pin = defineModel<string>('pin', { required: true })

const trimmedPin = computed(() => pin.value.trim())
const canSubmit = computed(() => trimmedPin.value.length > 0 && !props.isSubmitting)

const handleOpenChange = (nextOpen: boolean) => {
  open.value = nextOpen
  if (!nextOpen) emit('cancel')
}

const handleSubmit = () => {
  if (!canSubmit.value) return
  pin.value = trimmedPin.value
  emit('submit')
}

watch(open, (value) => {
  if (!value) pin.value = ''
})
</script>

<template>
  <Dialog :open="open" @update:open="handleOpenChange">
    <DialogContent class="sm:max-w-sm">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.unlock.title') }}</DialogTitle>
        <DialogDescription>
          {{ t('modemDetail.unlock.description', { lock: lockType }) }}
        </DialogDescription>
      </DialogHeader>

      <form class="space-y-4" @submit.prevent="handleSubmit">
        <div class="space-y-2">
          <label class="sr-only" for="sim-pin">{{ t('modemDetail.unlock.pinLabel') }}</label>
          <Input
            id="sim-pin"
            v-model="pin"
            type="password"
            inputmode="numeric"
            autocomplete="one-time-code"
            :disabled="isSubmitting"
            :placeholder="t('modemDetail.unlock.pinPlaceholder')"
            :aria-invalid="error ? 'true' : undefined"
          />
          <p v-if="error" class="text-sm text-destructive">
            {{ error }}
          </p>
        </div>

        <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Button type="submit" class="order-1 w-full sm:order-2" :disabled="!canSubmit">
            <Loader2 v-if="isSubmitting" class="size-4 animate-spin" aria-hidden="true" />
            <span>{{ t('modemDetail.unlock.submit') }}</span>
          </Button>
          <Button
            type="button"
            variant="ghost"
            class="order-2 w-full sm:order-1"
            :disabled="isSubmitting"
            @click="handleOpenChange(false)"
          >
            {{ t('modemDetail.actions.cancel') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
