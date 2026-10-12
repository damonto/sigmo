<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useForm } from '@tanstack/vue-form'
import { ArrowRightLeft, CloudDownload, ScanQrCode } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { z } from 'zod'

import ValidatedField from '@/components/ValidatedField.vue'
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
import { Input } from '@/components/ui/input'
import { validateOnInteraction } from '@/lib/formValidation'
import type { SEItem } from '@/types/se'
import {
  QrcodeStream,
  type BarcodeFormat,
  type DetectedBarcode,
  type EmittedError,
} from 'vue-qrcode-reader'

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

const buildInstallSchema = (requiresConfirmation: boolean) =>
  z.object({
    smdp: z
      .string({ error: t('modemDetail.esim.validation.smdpRequired') })
      .trim()
      .min(1, t('modemDetail.esim.validation.smdpRequired'))
      .transform((value) => compactEsimValue(value)),
    activationCode: z
      .string()
      .optional()
      .transform((value) => compactEsimValue(value ?? '')),
    confirmationCode: requiresConfirmation
      ? z
          .string({ error: t('modemDetail.validation.required') })
          .trim()
          .min(1, t('modemDetail.validation.required'))
      : z
          .string()
          .optional()
          .transform((value) => value?.trim() ?? ''),
    seId: z.string().trim().min(1, t('modemDetail.validation.required')),
  })

const installSchema = computed(() => buildInstallSchema(confirmationRequired.value))
const initialValues: z.input<ReturnType<typeof buildInstallSchema>> = {
  seId: '',
  smdp: '',
  activationCode: '',
  confirmationCode: '',
}

const form = useForm({
  validationLogic: validateOnInteraction,
  validators: {
    // Read the current LPA requirement whenever validation runs.
    onDynamic: ({ formApi }) => formApi.parseValuesWithSchema(installSchema.value),
  },
  defaultValues: initialValues,
  onSubmit: ({ value }): void => {
    const values = installSchema.value.parse(value)
    const seId = resolveSEID(values.seId)
    if (!seId) return

    emit('confirm', { ...values, seId })
    open.value = false
    // Reset after the dialog closes to avoid displaying empty fields briefly.
    void nextTick(resetValues)
  },
})

const InstallField = form.Field
const isSubmitting = form.useSelector((state) => state.isSubmitting)
const selectedSEID = form.useSelector((state) => state.values.seId)

const hasSelectedSE = computed(() => {
  const id = selectedSEID.value.trim()
  return id.length > 0 && seIDs.value.has(id)
})

const resetValues = () => {
  confirmationRequired.value = false
  form.reset({
    smdp: '',
    activationCode: '',
    confirmationCode: '',
    seId: implicitSEID.value,
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
  form.setFieldValue('seId', seId)
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
  form.reset({
    smdp: payload.smdp,
    activationCode: payload.activationCode,
    confirmationCode: '',
    seId: resolveSEID(form.state.values.seId),
  })
}

const handleSmdpChange = (input: string | number) => {
  form.setFieldValue('smdp', String(input))
  const value = compactEsimValue(String(input))
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

const applyDiscoverAddress = (address: string, seId = implicitSEID.value) => {
  const normalized = compactEsimValue(address)
  if (!normalized || isSubmitting.value) return
  const resolvedSEID = resolveSEID(seId)
  if (!resolvedSEID) return

  confirmationRequired.value = false
  form.reset({
    smdp: normalized,
    activationCode: '',
    confirmationCode: '',
    seId: resolvedSEID,
  })
  void form.handleSubmit()
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
    const seId = resolveSEID(form.state.values.seId)
    if (seId === form.state.values.seId) return
    form.reset({ ...form.state.values, seId })
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
            @click="emit('discover', resolveSEID(selectedSEID))"
          >
            <CloudDownload class="size-4" />
          </Button>
        </div>
        <DialogDescription class="sr-only">
          {{ t('modemDetail.esim.installTitle') }}
        </DialogDescription>
      </DialogHeader>

      <form
        class="space-y-4"
        @submit.prevent="form.handleSubmit"
      >
        <InstallField
          v-if="props.ses.length > 1"
          v-slot="{ field }"
          name="seId"
        >
          <ValidatedField
            v-slot="{ controlAttrs }"
            label="eUICC"
            :meta="field.state.meta"
          >
            <EsimSESelector
              v-bind="controlAttrs"
              :ses="props.ses"
              :selected-se-id="field.state.value"
              @update:selected-se-id="selectSE"
              @focusout="field.handleBlur"
            />
          </ValidatedField>
        </InstallField>

        <InstallField
          v-slot="{ field }"
          name="smdp"
        >
          <ValidatedField
            v-slot="{ controlAttrs }"
            :label="t('modemDetail.esim.smdp')"
            :meta="field.state.meta"
          >
            <Input
              v-bind="controlAttrs"
              :name="field.name"
              type="text"
              :placeholder="smdpPlaceholder"
              :model-value="field.state.value"
              @update:model-value="handleSmdpChange"
              @blur="field.handleBlur"
            />
          </ValidatedField>
        </InstallField>

        <InstallField
          v-slot="{ field }"
          name="activationCode"
        >
          <ValidatedField
            v-slot="{ controlAttrs }"
            :label="t('modemDetail.esim.activationCode')"
            :meta="field.state.meta"
          >
            <Input
              v-bind="controlAttrs"
              :name="field.name"
              type="text"
              :placeholder="activationPlaceholder"
              :model-value="field.state.value"
              @update:model-value="(value) => field.handleChange(String(value))"
              @blur="field.handleBlur"
            />
          </ValidatedField>
        </InstallField>

        <InstallField
          v-slot="{ field }"
          name="confirmationCode"
        >
          <ValidatedField
            v-slot="{ controlAttrs }"
            :label="t('modemDetail.esim.confirmationCode')"
            :meta="field.state.meta"
          >
            <Input
              v-bind="controlAttrs"
              :name="field.name"
              type="text"
              :placeholder="confirmationPlaceholder"
              :required="confirmationRequired"
              :model-value="field.state.value"
              @update:model-value="(value) => field.handleChange(String(value))"
              @blur="field.handleBlur"
            />
          </ValidatedField>
        </InstallField>

        <div class="space-y-4">
          <Button
            type="submit"
            class="w-full"
            :disabled="isSubmitting || !hasSelectedSE"
          >
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
            @click="emit('transfer', resolveSEID(selectedSEID))"
          >
            <ArrowRightLeft class="size-3.5" />
            {{ t('modemDetail.esim.transferButton') }}
          </Button>

          <Button
            variant="ghost"
            type="button"
            class="w-full"
            @click="closeDialog"
          >
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
        <p
          v-if="scanError"
          class="text-sm text-destructive"
        >
          {{ scanError }}
        </p>
        <p
          v-else
          class="text-sm text-muted-foreground"
        >
          {{ t('modemDetail.esim.scanDescription') }}
        </p>
      </div>
      <DialogFooter>
        <Button
          variant="ghost"
          type="button"
          @click="scanOpen = false"
        >
          {{ t('modemDetail.actions.cancel') }}
        </Button>
      </DialogFooter>
    </EsimPersistentDialogContent>
  </Dialog>
</template>
