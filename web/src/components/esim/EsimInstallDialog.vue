<script setup lang="ts">
import { toTypedSchema } from '@vee-validate/zod'
import { ArrowRightLeft, CloudDownload, ScanQrCode } from 'lucide-vue-next'
import { useForm } from 'vee-validate'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as z from 'zod'

import EsimSESelector from '@/components/esim/EsimSESelector.vue'
import EsimPersistentDialogContent from '@/components/esim/EsimPersistentDialogContent.vue'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import type { SEItem } from '@/types/se'
import {
  QrcodeStream,
  type BarcodeFormat,
  type DetectedBarcode,
  type EmittedError,
} from 'vue-qrcode-reader'

type InstallFormValues = {
  seId: string
  smdp: string
  activationCode: string
  confirmationCode?: string
}

const props = withDefaults(
  defineProps<{
    isDiscovering?: boolean
    allowTransfer?: boolean
    ses?: SEItem[]
  }>(),
  {
    isDiscovering: false,
    allowTransfer: false,
    ses: () => [],
  },
)

const emit = defineEmits<{
  (
    event: 'confirm',
    payload: {
      smdp: string
      activationCode: string
      confirmationCode: string
      seId: string
    },
  ): void
  (event: 'discover', seId: string): void
  (event: 'transfer', seId: string): void
}>()

const open = defineModel<boolean>('open', { required: true })

const { t } = useI18n()

const smdpPlaceholder = computed(() => t('modemDetail.esim.smdp'))
const activationPlaceholder = computed(() => t('modemDetail.esim.activationCode'))
const confirmationPlaceholder = computed(() => t('modemDetail.esim.confirmationCode'))

const confirmationRequired = ref(false)
const seIDs = computed(() => new Set(props.ses.map((se) => se.id)))
const implicitSEID = computed(() => (props.ses.length === 1 ? (props.ses[0]?.id ?? '') : ''))
const resolveSEID = (seId?: string) => {
  const id = seId?.trim() ?? ''
  if (id && seIDs.value.has(id)) return id
  return implicitSEID.value
}
const compactEsimValue = (value: string) => value.replace(/\s+/g, '')

const buildInstallSchemaDefinition = (requiresConfirmation: boolean) =>
  z.object({
    smdp: z
      .string({ message: t('modemDetail.esim.validation.smdpRequired') })
      .trim()
      .min(1, t('modemDetail.esim.validation.smdpRequired'))
      .transform((value) => compactEsimValue(value)),
    activationCode: z
      .string()
      .optional()
      .transform((value) => compactEsimValue(value ?? '')),
    confirmationCode: requiresConfirmation
      ? z
          .string({ message: t('modemDetail.validation.required') })
          .trim()
          .min(1, t('modemDetail.validation.required'))
      : z
          .string()
          .optional()
          .transform((value) => value?.trim() ?? ''),
    seId: z.string().trim().min(1, t('modemDetail.validation.required')),
  })

const installSchema = computed(() =>
  toTypedSchema(buildInstallSchemaDefinition(confirmationRequired.value)),
)

const { handleSubmit, resetForm, isSubmitting, values, setFieldValue } = useForm<InstallFormValues>({
  validationSchema: installSchema,
  initialValues: {
    seId: '',
    smdp: '',
    activationCode: '',
    confirmationCode: '',
  },
  validateOnMount: false,
})

const hasSelectedSE = computed(() => {
  const id = values.seId?.trim() ?? ''
  return id.length > 0 && seIDs.value.has(id)
})

const resetValues = () => {
  confirmationRequired.value = false
  resetForm({
    values: {
      smdp: '',
      activationCode: '',
      confirmationCode: '',
      seId: implicitSEID.value,
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

const selectSE = (seId: string) => {
  if (!seIDs.value.has(seId)) return
  setFieldValue('seId', seId)
}

const scanOpen = ref(false)
const scanPaused = ref(false)
const scanError = ref('')
const scanConstraints = { facingMode: 'environment' } satisfies MediaTrackConstraints
const scanFormats: BarcodeFormat[] = ['qr_code']

const parseLpaCode = (raw: string) => {
  const normalized = compactEsimValue(raw)
  const parts = normalized.split('$')
  const prefix = parts?.[0]?.toUpperCase() ?? ''
  if (parts.length < 3 || !prefix.startsWith('LPA:')) {
    return null
  }
  const smdp = compactEsimValue(parts[1] ?? '')
  const matchingId = compactEsimValue(parts[2] ?? '')
  const oid = compactEsimValue(parts[3] ?? '')
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
      seId: resolveSEID(values.seId),
    },
  })
}

const handleSmdpInput = (event: Event) => {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) return
  const value = compactEsimValue(target.value)
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
  const seId = resolveSEID(values.seId)
  if (!seId) return

  emit('confirm', {
    smdp: compactEsimValue(values.smdp),
    activationCode: compactEsimValue(values.activationCode),
    confirmationCode: values.confirmationCode?.trim() ?? '',
    seId,
  })
  open.value = false
  // Reset form after dialog is closed
  void nextTick(() => {
    resetValues()
  })
})

const applyDiscoverAddress = (address: string, seId = implicitSEID.value) => {
  const normalized = compactEsimValue(address)
  if (!normalized || isSubmitting.value) return
  const resolvedSEID = resolveSEID(seId)
  if (!resolvedSEID) return

  confirmationRequired.value = false
  resetForm({
    values: {
      smdp: normalized,
      activationCode: '',
      confirmationCode: '',
      seId: resolvedSEID,
    },
  })
  void onSubmit()
}

defineExpose({ applyDiscoverAddress })

watch(
  open,
  (value) => {
    if (value) {
      // Reset form in next tick when dialog opens to avoid validation flicker
      void nextTick(() => {
        resetValues()
      })
    } else {
      scanOpen.value = false
    }
  },
  { immediate: true },
)

watch(
  () => props.ses.map((se) => se.id).join('\0'),
  () => {
    if (!open.value) return
    const seId = resolveSEID(values.seId)
    if (seId === values.seId) return
    resetForm({
      values: {
        smdp: values.smdp,
        activationCode: values.activationCode,
        confirmationCode: values.confirmationCode,
        seId,
      },
    })
  },
)

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
    <EsimPersistentDialogContent class="sm:max-w-md">
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
            :disabled="props.isDiscovering || !hasSelectedSE"
            @click="emit('discover', resolveSEID(values.seId))"
          >
            <CloudDownload class="size-4" />
          </Button>
        </div>
        <DialogDescription class="sr-only">
          {{ t('modemDetail.esim.installTitle') }}
        </DialogDescription>
      </DialogHeader>

      <form class="space-y-4" @submit="onSubmit">
        <FormField v-if="props.ses.length > 1" name="seId">
          <FormItem>
            <FormLabel>eUICC</FormLabel>
            <FormControl>
              <EsimSESelector
                :ses="props.ses"
                :selected-se-id="values.seId ?? ''"
                @update:selected-se-id="selectSE"
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

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

        <div class="space-y-4">
          <Button type="submit" class="w-full" :disabled="isSubmitting || !hasSelectedSE">
            {{ t('modemDetail.esim.installConfirm') }}
          </Button>

          <div
            v-if="props.allowTransfer"
            class="flex items-center gap-4 text-sm text-muted-foreground"
          >
            <span class="h-px flex-1 bg-border" />
            <span>{{ t('modemDetail.esim.installOr') }}</span>
            <span class="h-px flex-1 bg-border" />
          </div>

          <Button
            v-if="props.allowTransfer"
            variant="outline"
            type="button"
            class="w-full border-primary text-primary hover:text-primary"
            :disabled="!hasSelectedSE"
            @click="emit('transfer', resolveSEID(values.seId))"
          >
            <ArrowRightLeft class="size-3.5" />
            {{ t('modemDetail.esim.transferButton') }}
          </Button>

          <Button variant="ghost" type="button" class="w-full" @click="closeDialog">
            {{ t('modemDetail.actions.cancel') }}
          </Button>
        </div>
      </form>
    </EsimPersistentDialogContent>
  </Dialog>

  <Dialog v-model:open="scanOpen">
    <EsimPersistentDialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.esim.scanTitle') }}</DialogTitle>
        <DialogDescription class="sr-only">
          {{ t('modemDetail.esim.scanDescription') }}
        </DialogDescription>
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
    </EsimPersistentDialogContent>
  </Dialog>
</template>
