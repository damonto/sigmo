<script setup lang="ts">
import { computed } from 'vue'
import { Cpu, Plane } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import RegionFlag from '@/components/RegionFlag.vue'
import CopyIdentifierButton from '@/components/modem/CopyIdentifierButton.vue'
import { Badge } from '@/components/ui/badge'
import { formatByteSize } from '@/lib/formatByteSize'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'
import type { SEsResponse } from '@/types/se'
import type { Modem } from '@/types/modem'

const props = defineProps<{
  modem: Modem
  seInfo?: SEsResponse | null
  variant?: 'overview'
}>()

const { t } = useI18n()
const imeiLabel = computed(() => t('modemDetail.fields.imei'))
const eidLabel = computed(() => t('modemDetail.fields.eid'))
const secureElements = computed(() => {
  const elements = props.seInfo?.ses ?? []
  return elements.length ? elements : [undefined]
})
</script>

<template>
  <div
    class="space-y-6"
    :class="props.variant === 'overview' ? 'text-xs' : 'text-sm'"
  >
    <!-- Basic Info -->
    <section class="space-y-3">
      <div class="flex items-center justify-between gap-3">
        <h2 class="text-sm font-semibold">{{ t('modemDetail.overview.device') }}</h2>
        <Cpu class="size-4 text-muted-foreground" aria-hidden="true" />
      </div>
      <div class="grid gap-3">
        <div class="flex items-center justify-between gap-3">
          <span class="text-muted-foreground">{{ imeiLabel }}</span>
          <div class="flex min-w-0 items-center justify-end gap-1">
            <span class="min-w-0 text-right font-mono wrap-anywhere select-all">{{ modem.id }}</span>
            <CopyIdentifierButton :field="imeiLabel" :value="modem.id" />
          </div>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">
            {{ t('modemDetail.fields.controlDevice') }}
          </span>
          <span class="break-all text-right font-mono">{{ modem.primaryPort }}</span>
        </div>
        <div class="flex justify-between gap-3">
          <span class="text-muted-foreground">{{ t('modemDetail.fields.moduleName') }}</span>
          <span class="text-right wrap-anywhere">{{ modem.name }}</span>
        </div>
        <div class="flex justify-between gap-3">
          <span class="text-muted-foreground">{{ t('modemDetail.fields.manufacturer') }}</span>
          <span class="text-right wrap-anywhere">{{ modem.manufacturer }}</span>
        </div>
        <div class="flex flex-wrap justify-between gap-x-3 gap-y-1">
          <span class="text-muted-foreground">{{ t('modemDetail.fields.firmwareRevision') }}</span>
          <span class="ms-auto min-w-0 text-right font-mono wrap-anywhere">
            {{ modem.firmwareRevision }}
          </span>
        </div>
        <div class="flex justify-between gap-3">
          <span class="text-muted-foreground">{{ t('modemDetail.fields.hardwareRevision') }}</span>
          <span class="text-right font-mono wrap-anywhere">{{ modem.hardwareRevision }}</span>
        </div>
        <div v-if="modem.number" class="flex justify-between">
          <span class="text-muted-foreground">Number</span>
          <span class="font-mono">{{ formatPhoneDisplay(modem.number) }}</span>
        </div>
      </div>
    </section>

    <section v-if="modem.simKind === 'euicc'" class="space-y-3">
      <h2 class="text-base font-semibold text-foreground">
        {{ t('modemDetail.overview.euicc') }}
      </h2>
      <div class="divide-y">
        <div
          v-for="element in secureElements"
          :key="element?.id ?? ''"
          class="space-y-2 py-3 first:pt-0 last:pb-0"
        >
          <h3 v-if="secureElements.length > 1" class="font-semibold wrap-anywhere">
            {{ element?.label || element?.id }}
          </h3>
          <dl class="grid gap-3">
            <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
              <dt class="text-muted-foreground">{{ eidLabel }}</dt>
              <dd class="ms-auto flex min-w-0 items-center justify-end gap-1">
                <span class="min-w-0 text-right font-mono wrap-anywhere select-all">
                  {{ element?.eid || 'N/A' }}
                </span>
                <CopyIdentifierButton
                  v-if="element?.eid"
                  :field="secureElements.length > 1 ? `${element.label || element.id} ${eidLabel}` : eidLabel"
                  :value="element.eid"
                />
              </dd>
            </div>
            <div class="flex justify-between gap-3">
              <dt class="text-muted-foreground">{{ t('modemDetail.fields.storageRemaining') }}</dt>
              <dd class="shrink-0 font-semibold">{{ formatByteSize(element?.freeSpace) }}</dd>
            </div>
            <div class="flex items-center justify-between gap-4">
              <dt class="text-muted-foreground">{{ t('modemDetail.fields.sasAccreditation') }}</dt>
              <dd class="flex items-center justify-end gap-2 text-right">
                <span class="wrap-anywhere">{{ element?.sasUp?.name || 'N/A' }}</span>
                <RegionFlag
                  :region-code="element?.sasUp?.region ?? ''"
                  class="rounded-sm text-base"
                />
              </dd>
            </div>
            <div class="flex flex-col gap-2">
              <dt class="text-muted-foreground">{{ t('modemDetail.fields.certificates') }}</dt>
              <dd v-if="element?.certificates?.length" class="flex flex-col gap-1">
                <span
                  v-for="(cert, index) in element.certificates"
                  :key="index"
                  class="rounded-md bg-muted px-2 py-1 text-xs wrap-anywhere text-foreground"
                >
                  {{ cert }}
                </span>
              </dd>
              <dd v-else class="text-xs text-muted-foreground">N/A</dd>
            </div>
          </dl>
        </div>
      </div>
    </section>

    <!-- SIM Info -->
    <section class="space-y-3">
      <h2 class="text-base font-semibold text-foreground">SIM Information</h2>
      <div class="grid gap-3">
        <div class="flex justify-between gap-3">
          <span class="text-muted-foreground">Operator</span>
          <span class="text-right wrap-anywhere">{{ modem.sim.operatorName }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Operator ID</span>
          <span class="font-mono">{{ modem.sim.operatorIdentifier }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Region</span>
          <span class="flex items-center gap-2 font-mono">
            <RegionFlag :region-code="modem.sim.regionCode" class="rounded-sm text-base" />
          </span>
        </div>
        <div class="flex justify-between gap-3">
          <span class="text-muted-foreground">ICCID</span>
          <span class="text-right font-mono wrap-anywhere">{{ modem.sim.identifier }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Active</span>
          <span>{{ modem.sim.active ? 'Yes' : 'No' }}</span>
        </div>
      </div>
    </section>

    <!-- Network Info -->
    <section class="space-y-3">
      <h2 class="text-sm font-semibold">{{ t('modemDetail.overview.networkDetails') }}</h2>
      <div v-if="modem.airplaneMode" class="grid gap-3">
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">Radio State</span>
          <span class="inline-flex items-center gap-2 font-medium text-muted-foreground">
            <Plane class="size-4" />
            {{ t('modemDetail.settings.networkAirplaneModeStatus') }}
          </span>
        </div>
      </div>
      <div v-else class="grid gap-3">
        <div class="flex justify-between">
          <span class="text-muted-foreground">{{ t('labels.accessTech') }}</span>
          <span>{{ modem.accessTechnology || 'N/A' }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">{{ t('modemDetail.settings.networkStatus') }}</span>
          <span>{{ modem.registrationState }}</span>
        </div>
        <div class="flex justify-between gap-3">
          <span class="text-muted-foreground">{{ t('labels.registeredOperator') }}</span>
          <span class="text-right wrap-anywhere">{{ modem.registeredOperator.name }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Operator Code</span>
          <span class="font-mono">{{ modem.registeredOperator.code }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">{{ t('labels.signal') }}</span>
          <span class="font-mono">{{ modem.signalQuality }}%</span>
        </div>
      </div>
    </section>

    <!-- Features -->
    <section class="space-y-3">
      <h2 class="text-base font-semibold text-foreground">Features</h2>
      <div class="flex flex-wrap gap-2">
        <Badge v-if="modem.simKind === 'euicc'">eSIM Support</Badge>
        <Badge v-else-if="modem.simKind === 'physical'" variant="secondary">
          Physical SIM Only
        </Badge>
        <Badge v-else variant="secondary">{{ t('labels.simUnknown') }}</Badge>
      </div>
    </section>
  </div>
</template>
