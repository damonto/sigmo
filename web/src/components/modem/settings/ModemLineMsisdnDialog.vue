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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'

const open = defineModel<boolean>('open', { required: true })
const msisdn = defineModel<string>('msisdn', { required: true })

const props = defineProps<{
  isUpdating: boolean
  isValid: boolean
}>()

const emit = defineEmits<{
  (event: 'save'): void
}>()

const { t } = useI18n()

const dialogOpen = computed({
  get: () => open.value,
  set: (value: boolean) => {
    if (props.isUpdating && !value) return
    open.value = value
  },
})

const closeDialog = () => {
  if (props.isUpdating) return
  open.value = false
}
</script>

<template>
  <Dialog v-model:open="dialogOpen">
    <DialogContent :show-close-button="!props.isUpdating">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.settings.msisdnEdit') }}</DialogTitle>
        <DialogDescription>
          {{ t('modemDetail.settings.msisdnEditDescription') }}
        </DialogDescription>
      </DialogHeader>

      <form class="space-y-4" @submit.prevent="emit('save')">
        <div class="space-y-2">
          <Label for="modem-line-msisdn">{{ t('modemDetail.settings.msisdnTitle') }}</Label>
          <Input
            id="modem-line-msisdn"
            v-model="msisdn"
            type="tel"
            inputmode="tel"
            autocomplete="tel"
            :disabled="props.isUpdating"
            :placeholder="t('modemDetail.settings.msisdnPlaceholder')"
          />
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" :disabled="props.isUpdating" @click="closeDialog">
            {{ t('modemDetail.actions.cancel') }}
          </Button>
          <Button type="submit" :disabled="!props.isValid || props.isUpdating">
            <Spinner v-if="props.isUpdating" class="size-4" />
            {{ t('modemDetail.actions.update') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
