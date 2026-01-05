<script setup lang="ts">
import { toTypedSchema } from '@vee-validate/zod'
import { RefreshCw, ScanQrCode } from 'lucide-vue-next'
import { useForm } from 'vee-validate'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  QrcodeStream,
  type BarcodeFormat,
  type DetectedBarcode,
  type EmittedError,
} from 'vue-qrcode-reader'

type InstallPayload = {
  smdp: string
  activationCode: string
  confirmationCode: string
}

type InstallFormValues = {
  smdp: string
  activationCode: string
  confirmationCode?: string
}

const props = withDefaults(
  defineProps<{
    isDiscovering?: boolean
  }>(),
  {
    isDiscovering: false,
  },
)

const emit = defineEmits<{
  (event: 'confirm', payload: InstallPayload): void
  (event: 'discover'): void
}>()

const open = defineModel<boolean>('open', { required: true })

const { t } = useI18n()

const smdpPlaceholder = computed(() => t('modemDetail.esim.smdp'))
const activationPlaceholder = computed(() => t('modemDetail.esim.activationCode'))
const confirmationPlaceholder = computed(() => t('modemDetail.esim.confirmationCode'))

const confirmationRequired = ref(false)

  const buildInstallSchemaDefinition = (requiresConfirmation: boolean) =>
    z.object({
      smdp: z
        .string({ error: t('modemDetail.esim.validation.smdpRequired') })
        .trim()
        .min(1, t('modemDetail.esim.validation.smdpRequired'))
        .transform((value) => value.trim()),
      activationCode: z
        .string()
        .optional()
        .transform((value) => value?.trim() ?? ''),
      confirmationCode: requiresConfirmation
        ? z
            .string({ error: t('modemDetail.validation.required') })
            .trim()
            .min(1, t('modemDetail.validation.required'))
        : z
            .string()
            .optional()
            .transform((value) => value?.trim() ?? ''),
    })

const installSchema = computed(() =>
  toTypedSchema(buildInstallSchemaDefinition(confirmationRequired.value)),
)

const { handleSubmit, resetForm, isSubmitting } = useForm<InstallFormValues>({
  validationSchema: installSchema,
  initialValues: {
    smdp: '',
    activationCode: '',
    confirmationCode: '',
  },
  validateOnMount: false,
})

const resetValues = () => {
  confirmationRequired.value = false
  resetForm({
    values: {
      smdp: '',
      activationCode: '',
      confirmationCode: '',
    },
    errors: {},
    touched: {},
  })
}

const closeDialog = () => {
  open.value = false
  // Reset form after dialog is closed to avoid visual flicker
  void nextTick(() => {
    resetValues()
  })
}

const scanOpen = ref(false)
const scanPaused = ref(false)
const scanError = ref('')
const scanConstraints = { facingMode: 'environment' } satisfies MediaTrackConstraints
const scanFormats: BarcodeFormat[] = ['qr_code']

const parseLpaCode = (raw: string) => {
  const trimmed = raw.trim()
  const parts = trimmed.split('$')
  const prefix = parts?.[0]?.toUpperCase() ?? ''
  if (parts.length < 3 || !prefix.startsWith('LPA:')) {
    return null
  }
  const smdp = parts[1] ?? ''
  const matchingId = parts[2] ?? ''
  const oid = parts[3] ?? ''
  const confirmationFlag = parts[4] ?? ''
  const activationCode = matchingId || oid
  return {
    smdp,
    activationCode,
    confirmationRequired: confirmationFlag === '1',
  }
}

const applyLpaPayload = (payload: {
  smdp: string
  activationCode: string
  confirmationRequired: boolean
}) => {
  confirmationRequired.value = payload.confirmationRequired
  resetForm({
    values: {
      smdp: payload.smdp,
      activationCode: payload.activationCode,
      confirmationCode: '',
    },
  })
}

const handleSmdpInput = (event: Event) => {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) return
  const value = target.value.trim()
  if (!value.toUpperCase().startsWith('LPA:1')) return
  const parsed = parseLpaCode(value)
  if (!parsed) return
  applyLpaPayload(parsed)
}

const handleScanResult = (value: string) => {
  const parsed = parseLpaCode(value)
  if (!parsed) {
    scanError.value = t('modemDetail.esim.scanInvalid')
    return
  }
  scanPaused.value = true
  applyLpaPayload(parsed)
  scanOpen.value = false
}

const handleDetect = (codes: DetectedBarcode[]) => {
  if (!codes.length) return
  const value = codes[0]?.rawValue ?? ''
  if (!value) return
  handleScanResult(value)
}

const handleScanError = (error: EmittedError) => {
  console.error('[EsimInstallDialog] Failed to scan QR:', error)
  if (error.name === 'NotFoundError') {
    scanError.value = t('modemDetail.esim.scanNoCamera')
    return
  }
  scanError.value = t('modemDetail.esim.scanFailed')
}

const openScanDialog = () => {
  scanOpen.value = true
  scanPaused.value = false
}

const onSubmit = handleSubmit((values) => {
  emit('confirm', {
    smdp: values.smdp,
    activationCode: values.activationCode,
    confirmationCode: values.confirmationCode ?? '',
  })
  open.value = false
  // Reset form after dialog is closed
  void nextTick(() => {
    resetValues()
  })
})

const applyDiscoverAddress = (address: string) => {
  const trimmed = address.trim()
  if (!trimmed || isSubmitting.value) return
  confirmationRequired.value = false
  resetForm({
    values: {
      smdp: trimmed,
      activationCode: '',
      confirmationCode: '',
    },
  })
  void onSubmit()
}

defineExpose({ applyDiscoverAddress })

watch(open, (value) => {
  if (value) {
    // Reset form in next tick when dialog opens to avoid validation flicker
    void nextTick(() => {
      resetValues()
    })
  } else {
    scanOpen.value = false
  }
})

watch(scanOpen, (value) => {
  if (!value) {
    scanError.value = ''
    scanPaused.value = false
    return
  }
  scanError.value = ''
  scanPaused.value = false
})
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <div class="flex items-center gap-2 pr-8">
          <DialogTitle>{{ t('modemDetail.esim.installTitle') }}</DialogTitle>
          <Button
            variant="outline"
            size="icon"
            type="button"
            class="shrink-0"
            :aria-label="t('modemDetail.esim.scan')"
            :title="t('modemDetail.esim.scan')"
            @click="openScanDialog"
          >
            <ScanQrCode class="size-4" />
          </Button>
          <Button
            variant="outline"
            size="icon"
            type="button"
            class="shrink-0"
            :aria-label="t('modemDetail.esim.discover')"
            :title="t('modemDetail.esim.discover')"
            :disabled="props.isDiscovering"
            @click="emit('discover')"
          >
            <RefreshCw class="size-4" />
          </Button>
        </div>
      </DialogHeader>

      <form class="space-y-4" @submit="onSubmit">
        <FormField v-slot="{ componentField }" name="smdp">
          <FormItem>
            <FormLabel>{{ t('modemDetail.esim.smdp') }}</FormLabel>
            <FormControl>
              <Input
                type="text"
                :placeholder="smdpPlaceholder"
                v-bind="componentField"
                @input="handleSmdpInput"
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <FormField v-slot="{ componentField }" name="activationCode">
          <FormItem>
            <FormLabel>{{ t('modemDetail.esim.activationCode') }}</FormLabel>
            <FormControl>
              <Input type="text" :placeholder="activationPlaceholder" v-bind="componentField" />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <FormField v-slot="{ componentField }" name="confirmationCode">
          <FormItem>
            <FormLabel>{{ t('modemDetail.esim.confirmationCode') }}</FormLabel>
            <FormControl>
              <Input
                type="text"
                :placeholder="confirmationPlaceholder"
                :required="confirmationRequired"
                v-bind="componentField"
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Button type="submit" class="order-1 w-full sm:order-2" :disabled="isSubmitting">
            {{ t('modemDetail.esim.installConfirm') }}
          </Button>
          <Button
            variant="ghost"
            type="button"
            class="order-2 w-full sm:order-1"
            @click="closeDialog"
          >
            {{ t('modemDetail.actions.cancel') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>

  <Dialog v-model:open="scanOpen">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.esim.scanTitle') }}</DialogTitle>
      </DialogHeader>
      <div class="space-y-3">
        <div class="mx-auto aspect-square w-full max-w-sm overflow-hidden rounded-lg bg-muted/40">
          <QrcodeStream
            v-if="scanOpen"
            class="h-full w-full"
            :constraints="scanConstraints"
            :formats="scanFormats"
            :paused="scanPaused"
            @detect="handleDetect"
            @error="handleScanError"
          />
        </div>
        <p v-if="scanError" class="text-sm text-destructive">
          {{ scanError }}
        </p>
        <p v-else class="text-sm text-muted-foreground">
          {{ t('modemDetail.esim.scanDescription') }}
        </p>
      </div>
      <DialogFooter>
        <Button variant="ghost" type="button" @click="scanOpen = false">
          {{ t('modemDetail.actions.cancel') }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
