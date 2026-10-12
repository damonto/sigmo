<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import type { RegistrationMode } from '@/types/network'

const props = defineProps<{
  operatorLabel: string
  registrationState: string
  accessTechnology: string
  registrationMode: RegistrationMode
  registeredOperatorCode: string
  isScanning: boolean
  isRegistrationUpdating: boolean
  canScan: boolean
  canUpdateRegistration: boolean
}>()

const emit = defineEmits<{
  (event: 'scan'): void
  (event: 'updateRegistrationMode', mode: RegistrationMode): void
}>()

const { t } = useI18n()

const isScanDisabled = computed(() => props.isScanning || !props.canScan)
const isRegistrationDisabled = computed(
  () => props.isRegistrationUpdating || !props.canUpdateRegistration,
)

const manualLabel = computed(() =>
  props.registeredOperatorCode
    ? t('modemDetail.settings.networkRegistrationManualOperator', {
        operatorCode: props.registeredOperatorCode,
      })
    : t('modemDetail.settings.networkRegistrationManual'),
)

const isRegistrationMode = (value: unknown): value is RegistrationMode =>
  value === 'automatic' || value === 'manual'

// The select is controlled by the mode the modem reports. Emitting without
// mutating local state keeps the trigger on the real mode when a manual
// switch is abandoned before an operator is chosen.
const handleModeUpdate = (value: unknown) => {
  if (!isRegistrationMode(value) || value === props.registrationMode) return
  emit('updateRegistrationMode', value)
}
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.networkTitle') }}
      </CardTitle>
      <CardAction class="inline-flex items-center gap-2">
        <Spinner
          v-if="props.isRegistrationUpdating"
          class="size-4 text-muted-foreground"
        />
        <Select
          :model-value="props.registrationMode"
          :disabled="isRegistrationDisabled"
          @update:model-value="handleModeUpdate"
        >
          <SelectTrigger
            id="network-registration-mode"
            size="sm"
            :aria-label="t('modemDetail.settings.networkRegistrationModeLabel')"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="automatic">
              {{ t('modemDetail.settings.networkRegistrationAutomatic') }}
            </SelectItem>
            <SelectItem value="manual">
              {{ manualLabel }}
            </SelectItem>
          </SelectContent>
        </Select>
        <Button
          size="sm"
          type="button"
          :disabled="isScanDisabled"
          @click="emit('scan')"
        >
          <span
            v-if="props.isScanning"
            class="inline-flex items-center gap-2"
          >
            <Spinner class="size-4" />
            {{ t('modemDetail.settings.networkSearch') }}
          </span>
          <span v-else>{{ t('modemDetail.settings.networkSearch') }}</span>
        </Button>
      </CardAction>
    </CardHeader>

    <CardContent class="space-y-2 px-4 text-sm">
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.networkOperator') }}</span>
        <span class="font-medium text-foreground">
          {{ props.operatorLabel }}
        </span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.networkStatus') }}</span>
        <span class="font-medium text-foreground">
          {{ props.registrationState }}
        </span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.networkAccess') }}</span>
        <span class="font-medium text-foreground">
          {{ props.accessTechnology }}
        </span>
      </div>
    </CardContent>
  </Card>
</template>
