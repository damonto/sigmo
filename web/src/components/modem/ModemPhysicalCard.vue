<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Card, CardContent } from '@/components/ui/card'
import ModemSignalStatus from '@/components/modem/ModemSignalStatus.vue'
import RegionFlag from '@/components/RegionFlag.vue'
import type { Modem } from '@/types/modem'

const props = defineProps<{
  modem: Modem
}>()

const { t } = useI18n()
</script>

<template>
  <Card
    class="gap-0 rounded-xl border-0 bg-white/80 py-0 shadow-sm backdrop-blur-xl dark:bg-slate-950/60"
  >
    <CardContent class="space-y-4 px-4 py-4 text-sm">
      <div class="flex items-center justify-between gap-4">
        <span class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
          {{ t('modemDetail.fields.moduleName') }}
        </span>
        <span class="font-semibold text-foreground">{{ props.modem.name }}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
          {{ t('modemDetail.fields.manufacturer') }}
        </span>
        <span class="text-foreground">{{ props.modem.manufacturer }}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
          {{ t('modemDetail.fields.carrier') }}
        </span>
        <span class="text-foreground">{{ props.modem.sim.operatorName }}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
          {{ t('modemDetail.fields.roamingCarrier') }}
        </span>
        <span class="text-muted-foreground">
          {{ props.modem.registeredOperator.name || '—' }}
        </span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
          {{ t('modemDetail.fields.signal') }}
        </span>
        <ModemSignalStatus
          :signal-quality="props.modem.signalQuality"
          :registration-state="props.modem.registrationState"
          :access-technology="props.modem.accessTechnology"
          :registered-operator-name="props.modem.registeredOperator.name"
          size="md"
        />
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
          {{ t('modemDetail.fields.flag') }}
        </span>
        <RegionFlag :region-code="props.modem.sim.regionCode" class="rounded-sm text-[18px]" />
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
          {{ t('modemDetail.fields.iccid') }}
        </span>
        <span class="font-mono text-xs text-foreground">
          {{ props.modem.id }}
        </span>
      </div>
    </CardContent>
  </Card>
</template>
