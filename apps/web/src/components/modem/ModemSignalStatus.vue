<script setup lang="ts">
import { computed } from 'vue'
import { Plane, SignalHigh } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import VoLTEIcon from '@/components/modem/VoLTEIcon.vue'
import VoWiFiIcon from '@/components/modem/VoWiFiIcon.vue'
import { Badge } from '@/components/ui/badge'
import { useModemDisplay } from '@/composables/useModemDisplay'

const props = withDefaults(
  defineProps<{
    signalQuality: number
    registrationState: string
    accessTechnology?: string | null
    registeredOperatorName?: string | null
    wifiCallingConnected?: boolean
    volteConnected?: boolean
    airplaneMode?: boolean
    showSignalValue?: boolean
    size?: 'sm' | 'md'
    variant?: 'inline' | 'pill' | 'stacked'
  }>(),
  {
    showSignalValue: true,
    size: 'sm',
    variant: 'inline',
  },
)

const { t } = useI18n()
const {
  formatSignal,
  signalIcon,
  signalTone,
  registrationStateIcon,
  registrationStateLabel,
  registrationStateTone,
  shouldShowRegistrationIcon,
  getSignalColorOverride,
} = useModemDisplay()

const isSearching = computed(() => props.registrationState.trim() === 'Searching')
const signalValue = computed(() => formatSignal(props.signalQuality))
const signalIconComponent = computed(() =>
  isSearching.value ? SignalHigh : signalIcon(props.signalQuality),
)
const signalToneClass = computed(() => {
  if (isSearching.value) return 'text-amber-500'
  const override = getSignalColorOverride(props.registrationState)
  return override ?? signalTone(props.signalQuality)
})
const signalTitle = computed(() => `${t('labels.signal')}: ${signalValue.value}`)
const showRegistrationIcon = computed(() => shouldShowRegistrationIcon(props.registrationState))
const registrationIcon = computed(() => registrationStateIcon(props.registrationState))
const registrationLabel = computed(() => registrationStateLabel(props.registrationState))
const registrationToneClass = computed(() => registrationStateTone(props.registrationState))
const accessTechnologyLabel = computed(() => props.accessTechnology?.trim() ?? '')
const registeredOperatorLabel = computed(() => props.registeredOperatorName?.trim() ?? '')
const accessTechnologyTitle = computed(
  () => `${t('labels.accessTech')}: ${accessTechnologyLabel.value}`,
)
const registeredOperatorTitle = computed(
  () => `${t('labels.registeredOperator')}: ${registeredOperatorLabel.value}`,
)
const airplaneModeTitle = computed(() => t('modemDetail.settings.networkAirplaneModeStatus'))
const volteTitle = computed(
  () => `${t('modemDetail.settings.volteTitle')}: ${t('modemDetail.settings.volteConnected')}`,
)
const stacked = computed(() => props.variant === 'stacked')
const radioGeneration = computed(() => {
  if (!stacked.value || props.airplaneMode || !accessTechnologyLabel.value) return ''

  // Match the radio generations displayed in the iOS device list.
  switch (accessTechnologyLabel.value.toUpperCase()) {
    case '2G':
    case 'GSM':
    case 'GPRS':
    case 'EDGE':
      return '2G'
    case '3G':
    case 'UMTS':
    case 'HSDPA':
    case 'HSUPA':
    case 'HSPA':
    case 'HSPA+':
      return '3G'
    case '4G':
    case 'LTE':
    case 'LTE-M':
    case 'LTE CAT-M':
    case 'LTE NB-IOT':
      return '4G'
    case '5G':
    case '5GNR':
    case '5G NR':
    case 'NR':
      return '5G'
    default:
      return '?'
  }
})
const showVoLTE = computed(() => stacked.value && !props.airplaneMode && props.volteConnected)
const showCellularDetails = computed(
  () =>
    !props.airplaneMode &&
    (showRegistrationIcon.value ||
      (!stacked.value && accessTechnologyLabel.value) ||
      registeredOperatorLabel.value),
)
const iconSizeClass = computed(() => (props.size === 'md' ? 'size-5' : 'size-4'))
const numberClass = computed(() =>
  props.size === 'md'
    ? 'font-mono text-xs text-muted-foreground tabular-nums'
    : 'inline-flex h-4 min-w-6 items-center justify-end text-right font-mono text-xs text-muted-foreground tabular-nums',
)
const registrationBadgeClass = computed(() =>
  props.size === 'md'
    ? 'size-5 rounded-full p-0 text-xs font-semibold'
    : 'size-4 rounded-full p-0 text-[10px] font-semibold',
)
const accessTechnologyClass = computed(() =>
  props.size === 'md'
    ? 'h-5 rounded-full border-border/60 bg-muted/30 px-1.5 font-mono text-xs font-medium text-muted-foreground'
    : 'h-4 rounded-full border-border/60 bg-muted/30 px-1.5 font-mono text-[10px] font-medium text-muted-foreground',
)
const operatorClass = computed(() =>
  props.size === 'md'
    ? 'inline-block max-w-36 truncate text-xs font-medium text-muted-foreground'
    : 'inline-block max-w-24 truncate text-xs font-medium text-muted-foreground',
)
const rootClass = computed(() => {
  if (stacked.value) return 'flex shrink-0 flex-col items-end gap-0.5'
  return props.variant === 'pill'
    ? 'inline-flex shrink-0 items-center gap-1 rounded-full border border-border/70 bg-muted/40 px-2 py-1'
    : 'flex shrink-0 items-center gap-1'
})
const groupClass = computed(() => (stacked.value ? 'flex items-center gap-1' : 'contents'))
</script>

<template>
  <div
    :class="rootClass"
    data-testid="modem-signal-status"
  >
    <div :class="groupClass">
      <Plane
        v-if="props.airplaneMode"
        class="shrink-0"
        :class="[iconSizeClass, 'text-muted-foreground']"
        :title="airplaneModeTitle"
        :aria-label="airplaneModeTitle"
      />
      <template v-else>
        <component
          :is="signalIconComponent"
          class="shrink-0"
          :class="[iconSizeClass, signalToneClass, isSearching && 'animate-pulse']"
          :title="signalTitle"
        />
        <span
          v-if="props.showSignalValue"
          :class="numberClass"
        >
          {{ signalValue }}
        </span>
      </template>
      <span
        v-if="radioGeneration"
        class="font-mono text-xs font-medium text-muted-foreground"
        :title="accessTechnologyTitle"
      >
        {{ radioGeneration }}
      </span>
    </div>
    <div
      v-if="showCellularDetails || showVoLTE || props.wifiCallingConnected"
      :class="groupClass"
    >
      <VoWiFiIcon
        v-if="props.wifiCallingConnected"
        class="shrink-0"
        :class="stacked ? 'order-last size-4' : iconSizeClass"
        title="Wi-Fi Calling"
        aria-label="Wi-Fi Calling"
      />
      <template v-if="showCellularDetails">
        <component
          v-if="showRegistrationIcon && registrationIcon"
          :is="registrationIcon"
          class="shrink-0"
          :class="[iconSizeClass, registrationToneClass]"
          :aria-label="props.registrationState"
          :title="props.registrationState"
        />
        <Badge
          v-else-if="showRegistrationIcon && registrationLabel"
          variant="secondary"
          :class="[registrationBadgeClass, registrationToneClass]"
          :aria-label="props.registrationState"
          :title="props.registrationState"
        >
          {{ registrationLabel }}
        </Badge>
        <Badge
          v-if="!stacked && accessTechnologyLabel"
          variant="outline"
          :class="accessTechnologyClass"
          :aria-label="accessTechnologyTitle"
          :title="accessTechnologyTitle"
        >
          {{ accessTechnologyLabel }}
        </Badge>
        <span
          v-if="registeredOperatorLabel"
          :class="operatorClass"
          :aria-label="registeredOperatorTitle"
          :title="registeredOperatorTitle"
        >
          {{ registeredOperatorLabel }}
        </span>
      </template>
      <VoLTEIcon
        v-if="showVoLTE"
        class="size-4 shrink-0"
        :title="volteTitle"
        :aria-label="volteTitle"
      />
    </div>
  </div>
</template>
