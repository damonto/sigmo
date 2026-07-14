<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import type { VoLTENetworkDriver, VoLTEQMINetworkDriver } from '@/types/volte'

const props = defineProps<{
  enabled: boolean
  networkDriver: VoLTENetworkDriver
  setImsApnAsDefault: boolean
  enablePcscfViaPco: boolean
  modemRegistered: boolean
  isLoading: boolean
  isUpdating: boolean
}>()

const emit = defineEmits<{
  (event: 'update', enabled: boolean): void
  (event: 'update-driver', networkDriver: VoLTEQMINetworkDriver): void
  (
    event: 'update-profile-options',
    options: { setIMSAPNAsDefault: boolean; enablePCSCFViaPCO: boolean },
  ): void
}>()

const { t } = useI18n()

const description = computed(() => {
  if (props.enabled) return t('modemDetail.settings.volteManagedDescription')
  if (props.modemRegistered) return t('modemDetail.settings.volteModemRegisteredDescription')
  return t('modemDetail.settings.volteDescription')
})

const networkDriverDisabled = computed(() => props.enabled || props.isLoading || props.isUpdating)

const profileOptionsDisabled = computed(() => props.enabled || props.isLoading || props.isUpdating)

const updateNetworkDriver = (networkDriver: unknown) => {
  if (networkDriver !== 'qmap' && networkDriver !== 'legacy_bam_dmux') return
  emit('update-driver', networkDriver)
}

const updateProfileOption = (name: 'setIMSAPNAsDefault' | 'enablePCSCFViaPCO', value: boolean) => {
  emit('update-profile-options', {
    setIMSAPNAsDefault:
      name === 'setIMSAPNAsDefault' ? value : props.setImsApnAsDefault,
    enablePCSCFViaPCO: name === 'enablePCSCFViaPCO' ? value : props.enablePcscfViaPco,
  })
}
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">{{ t('modemDetail.settings.volteTitle') }}</CardTitle>
    </CardHeader>
    <CardContent class="space-y-5 px-4">
      <div v-if="props.networkDriver !== 'mbim'" class="space-y-2">
        <Label for="volte-network-driver">
          {{ t('modemDetail.settings.volteNetworkDriverLabel') }}
        </Label>
        <RadioGroup
          class="gap-2"
          :model-value="props.networkDriver"
          :disabled="networkDriverDisabled"
          @update:model-value="updateNetworkDriver"
        >
          <label
            class="flex items-start gap-3 rounded-lg border px-3 py-3 shadow-sm transition"
            :class="[
              props.networkDriver === 'qmap'
                ? 'border-primary/40 bg-primary/5'
                : 'border-transparent bg-muted/30',
              networkDriverDisabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer',
            ]"
          >
            <RadioGroupItem id="volte-network-driver-qmap" value="qmap" class="mt-1" />
            <span class="min-w-0 space-y-1">
              <span class="flex items-center gap-2">
                <span class="text-sm font-semibold text-foreground">
                  {{ t('modemDetail.settings.volteNetworkDriverQMAP') }}
                </span>
                <span
                  class="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary"
                >
                  {{ t('modemDetail.settings.volteNetworkDriverDefault') }}
                </span>
              </span>
              <span class="block text-xs leading-5 text-muted-foreground">
                {{ t('modemDetail.settings.volteNetworkDriverQMAPDescription') }}
              </span>
            </span>
          </label>

          <label
            class="flex items-start gap-3 rounded-lg border px-3 py-3 shadow-sm transition"
            :class="[
              props.networkDriver === 'legacy_bam_dmux'
                ? 'border-primary/40 bg-primary/5'
                : 'border-transparent bg-muted/30',
              networkDriverDisabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer',
            ]"
          >
            <RadioGroupItem id="volte-network-driver-legacy" value="legacy_bam_dmux" class="mt-1" />
            <span class="min-w-0 space-y-1">
              <span class="block text-sm font-semibold text-foreground">
                {{ t('modemDetail.settings.volteNetworkDriverLegacy') }}
              </span>
              <span class="block text-xs leading-5 text-muted-foreground">
                {{ t('modemDetail.settings.volteNetworkDriverLegacyDescription') }}
              </span>
              <span class="block text-xs leading-5 text-amber-700 dark:text-amber-400">
                {{ t('modemDetail.settings.volteNetworkDriverLegacyWarning') }}
              </span>
            </span>
          </label>
        </RadioGroup>
      </div>

      <div v-if="props.networkDriver !== 'mbim'" class="space-y-4">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0 flex-1 space-y-1">
            <Label for="volte-ims-apn-default">
              {{ t('modemDetail.settings.volteIMSAPNDefaultLabel') }}
            </Label>
            <p class="text-xs leading-5 text-muted-foreground">
              {{ t('modemDetail.settings.volteIMSAPNDefaultDescription') }}
            </p>
          </div>
          <Switch
            id="volte-ims-apn-default"
            :model-value="props.setImsApnAsDefault"
            :disabled="profileOptionsDisabled"
            @update:model-value="updateProfileOption('setIMSAPNAsDefault', $event === true)"
          />
        </div>

        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0 flex-1 space-y-1">
            <Label for="volte-ims-apn-pco">
              {{ t('modemDetail.settings.volteIMSAPNPCOLabel') }}
            </Label>
            <p class="text-xs leading-5 text-muted-foreground">
              {{ t('modemDetail.settings.volteIMSAPNPCODescription') }}
            </p>
          </div>
          <Switch
            id="volte-ims-apn-pco"
            :model-value="props.enablePcscfViaPco"
            :disabled="profileOptionsDisabled"
            @update:model-value="updateProfileOption('enablePCSCFViaPCO', $event === true)"
          />
        </div>
      </div>

      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1 space-y-1">
          <Label for="volte-enabled">{{ t('modemDetail.settings.volteLabel') }}</Label>
          <p class="text-xs leading-5 text-muted-foreground">{{ description }}</p>
        </div>
        <div class="inline-flex shrink-0 items-center gap-2">
          <Spinner v-if="props.isUpdating" class="size-4 text-muted-foreground" />
          <Switch
            id="volte-enabled"
            :model-value="props.enabled"
            :disabled="props.isLoading || props.isUpdating"
            @update:model-value="emit('update', $event === true)"
          />
        </div>
      </div>
    </CardContent>
  </Card>
</template>
