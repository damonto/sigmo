<script setup lang="ts">
import { Activity, Plane } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ModemInternetTraffic from '@/components/modem/ModemInternetTraffic.vue'
import { useModemDisplay } from '@/composables/useModemDisplay'
import { useModemTraffic } from '@/composables/useModemTraffic'
import type { Modem } from '@/types/modem'

const props = defineProps<{ modem: Modem }>()
defineSlots<{ default?(): unknown }>()
const { t } = useI18n()
const { formatSignal, signalIcon } = useModemDisplay()
const { connected, rates, usedBytes, history } = useModemTraffic(
  computed(() => props.modem.id),
  computed(() => props.modem.sim.identifier),
  computed(() => !props.modem.airplaneMode && props.modem.state !== 'locked'),
)

const NEUTRAL_STATUS = 'bg-muted-foreground/10 text-muted-foreground'
const AMBER_STATUS = 'bg-amber-100 text-amber-900 dark:bg-amber-500/15 dark:text-amber-200'

const formatOperator = (operatorName: string, operatorCode: string) => {
  const name = operatorName.trim()
  const code = operatorCode.trim()
  return { label: name || code || '—', code: name && name !== code ? code : '' }
}

const simOperator = computed(() => {
  const { operatorName, operatorIdentifier } = props.modem.sim
  return formatOperator(operatorName, operatorIdentifier)
})

const registrationState = computed(() => props.modem.registrationState.trim().toLowerCase())
const registeredNetwork = computed(() => {
  if (props.modem.airplaneMode || !['home', 'roaming'].includes(registrationState.value)) {
    return formatOperator('', '')
  }
  const { name, code } = props.modem.registeredOperator
  return formatOperator(name, code)
})

const registration = computed(() => {
  if (props.modem.airplaneMode) {
    return {
      label: t('modemDetail.settings.networkAirplaneModeStatus'),
      tone: NEUTRAL_STATUS,
    }
  }
  switch (registrationState.value) {
    case 'home':
      return {
        label: t('modemDetail.overview.registration.home'),
        tone: 'bg-emerald-500/10 text-emerald-800 dark:text-emerald-300',
      }
    case 'roaming':
      return { label: t('labels.roaming'), tone: AMBER_STATUS }
    case 'searching':
      return { label: t('modemDetail.overview.registration.searching'), tone: AMBER_STATUS }
    case 'denied':
      return {
        label: t('modemDetail.overview.registration.denied'),
        tone: 'bg-red-100 text-red-800 dark:bg-red-500/15 dark:text-red-200',
      }
    case 'idle':
      return { label: t('modemDetail.overview.registration.idle'), tone: NEUTRAL_STATUS }
    default:
      return { label: t('modemDetail.settings.networkUnknown'), tone: NEUTRAL_STATUS }
  }
})

const hasSignal = computed(() => !props.modem.airplaneMode && props.modem.signalQuality > 0)
const signal = computed(() => (hasSignal.value ? formatSignal(props.modem.signalQuality) : '—'))
const signalSymbol = computed(() =>
  props.modem.airplaneMode ? Plane : signalIcon(props.modem.signalQuality),
)
const technology = computed(() =>
  props.modem.airplaneMode ? '—' : props.modem.accessTechnology?.trim() || '—',
)
const voiceServices = computed(
  () =>
    [
      props.modem.wifiCallingConnected && t('modemDetail.calls.routes.wifiCalling'),
      !props.modem.airplaneMode &&
        props.modem.volteConnected &&
        t('modemDetail.calls.routes.volte'),
    ]
      .filter(Boolean)
      .join(' · ') || '—',
)
</script>

<template>
  <div
    class="contents lg:relative lg:isolate lg:grid lg:overflow-hidden lg:rounded-xl lg:border lg:border-primary/15 lg:bg-linear-to-br lg:from-accent/45 lg:to-accent"
    :class="connected ? 'lg:grid-cols-[minmax(0,1.85fr)_minmax(13rem,1fr)]' : 'lg:grid-cols-1'"
  >
    <div
      v-if="!connected"
      class="pointer-events-none absolute -inset-e-14 -top-18 -z-10 hidden size-48 place-items-center text-primary/12 lg:grid"
      aria-hidden="true"
    >
      <span class="col-start-1 row-start-1 size-16 rounded-full border border-current" />
      <span class="col-start-1 row-start-1 size-28 rounded-full border border-current" />
      <span class="col-start-1 row-start-1 size-40 rounded-full border border-current" />
    </div>
    <div class="contents lg:block lg:min-w-0 lg:px-5 lg:py-3">
      <div class="hidden lg:block">
        <div class="flex items-center gap-2">
          <Activity
            class="size-4 text-primary"
            aria-hidden="true"
          />
          <h2 class="text-sm font-semibold">{{ t('modemDetail.overview.network') }}</h2>
        </div>
        <dl class="mt-3 grid grid-cols-[1fr_1.2fr_0.8fr] divide-x divide-primary/15 text-xs">
          <div class="min-w-0 pe-4">
            <dt class="text-muted-foreground">{{ t('modemDetail.overview.simOperator') }}</dt>
            <dd class="mt-0.5 flex flex-wrap items-baseline gap-x-2 wrap-anywhere">
              <span class="text-base font-semibold tracking-tight">{{ simOperator.label }}</span>
              <span
                v-if="simOperator.code"
                class="text-muted-foreground tabular-nums"
              >
                {{ simOperator.code }}
              </span>
            </dd>
          </div>
          <div class="min-w-0 px-4">
            <dt class="text-muted-foreground">{{ t('modemDetail.overview.registeredNetwork') }}</dt>
            <dd class="mt-0.5 flex flex-wrap items-baseline gap-x-2 wrap-anywhere">
              <span class="text-base font-semibold tracking-tight">{{
                registeredNetwork.label
              }}</span>
              <span
                v-if="registeredNetwork.code"
                class="text-muted-foreground tabular-nums"
              >
                {{ registeredNetwork.code }}
              </span>
            </dd>
          </div>
          <div class="min-w-0 ps-4">
            <dt class="text-muted-foreground">
              {{ t('modemDetail.overview.registrationStatus') }}
            </dt>
            <dd class="mt-0.5">
              <span
                class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 font-medium"
                :class="registration.tone"
              >
                <span
                  class="size-1.5 shrink-0 rounded-full bg-current"
                  aria-hidden="true"
                />
                {{ registration.label }}
              </span>
            </dd>
          </div>
        </dl>
      </div>
      <dl
        class="contents lg:mt-3 lg:grid lg:grid-cols-[minmax(0,1fr)_minmax(0,0.85fr)_minmax(0,1.15fr)_max-content] lg:items-center lg:gap-x-3 lg:border-t lg:border-primary/15 lg:pt-2 lg:text-xs"
      >
        <div class="hidden min-w-0 flex-wrap items-center gap-x-2 lg:flex">
          <dt class="text-muted-foreground">{{ t('labels.signal') }}</dt>
          <dd class="flex items-center gap-1 font-medium tabular-nums">
            <component
              :is="signalSymbol"
              class="size-4 shrink-0"
              :class="hasSignal ? 'text-primary' : 'text-muted-foreground'"
              aria-hidden="true"
            />
            {{ signal }}
          </dd>
        </div>
        <div class="hidden min-w-0 flex-wrap items-center gap-x-2 lg:flex">
          <dt class="text-muted-foreground">{{ t('labels.accessTech') }}</dt>
          <dd class="font-medium wrap-anywhere">{{ technology }}</dd>
        </div>
        <div class="hidden min-w-0 flex-wrap items-center gap-x-2 lg:flex">
          <dt class="text-muted-foreground">{{ t('modemDetail.overview.voice') }}</dt>
          <dd class="font-medium wrap-anywhere">{{ voiceServices }}</dd>
        </div>
        <div class="contents lg:flex lg:max-w-52 lg:items-center lg:gap-x-1 lg:whitespace-nowrap">
          <dt class="hidden text-muted-foreground lg:block">
            {{ t('modemDetail.overview.simSlot') }}
          </dt>
          <dd class="contents lg:block lg:min-w-0">
            <slot><span class="hidden font-medium lg:inline">—</span></slot>
          </dd>
        </div>
      </dl>
    </div>
    <ModemInternetTraffic
      v-if="connected"
      :rates="rates"
      :used-bytes="usedBytes"
      :history="history"
    />
  </div>
</template>
