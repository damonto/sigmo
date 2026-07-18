<script setup lang="ts">
import { LoaderCircle } from 'lucide-vue-next'
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

const props = withDefaults(
  defineProps<{
    disabled?: boolean
    originalLabel: string
    saving?: boolean
  }>(),
  {
    disabled: false,
    saving: false,
  },
)

const emit = defineEmits<{
  close: []
  save: [label: string]
}>()

const open = defineModel<boolean>('open', { required: true })
const label = defineModel<string>('label', { required: true })
const { t } = useI18n()
const controlsDisabled = computed(() => props.disabled || props.saving)
const normalizedLabel = computed(() => label.value.trim())
const canSave = computed(
  () =>
    !controlsDisabled.value &&
    normalizedLabel.value.length > 0 &&
    normalizedLabel.value !== props.originalLabel,
)

const handleOpen = (value: boolean) => {
  if (!value && props.saving) return
  open.value = value
  if (!value) emit('close')
}

const save = () => {
  if (canSave.value) emit('save', normalizedLabel.value)
}
</script>

<template>
  <Dialog :open="open" @update:open="handleOpen">
    <DialogContent :show-close-button="!props.saving" class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t('settings.webPush.rename') }}</DialogTitle>
        <DialogDescription>{{ t('settings.webPush.renameDescription') }}</DialogDescription>
      </DialogHeader>

      <form class="space-y-4" @submit.prevent="save">
        <div class="space-y-2">
          <Label for="web-push-device-label">{{ t('settings.webPush.deviceLabel') }}</Label>
          <Input
            id="web-push-device-label"
            v-model="label"
            autofocus
            :aria-label="t('settings.webPush.deviceLabel')"
            :disabled="controlsDisabled"
          />
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            :disabled="props.saving"
            @click="handleOpen(false)"
          >
            {{ t('settings.cancel') }}
          </Button>
          <Button type="submit" :aria-label="t('settings.webPush.saveRename')" :disabled="!canSave">
            <LoaderCircle v-if="props.saving" class="size-4 animate-spin" />
            {{ t('settings.webPush.saveRename') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
