<script setup lang="ts">
import { computed } from 'vue'
import { SignalHigh } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Badge } from '@/components/ui/badge'
import { useModemDisplay } from '@/composables/useModemDisplay'

const props = withDefaults(
  defineProps<{
    signalQuality: number
    registrationState: string
    accessTechnology?: string | null
    registeredOperatorName?: string | null
    showSignalValue?: boolean
    size?: 'sm' | 'md'
    variant?: 'inline' | 'pill'
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
const showRegistrationIcon = computed(() =>
  shouldShowRegistrationIcon(props.registrationState),
)
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
const rootClass = computed(() =>
  props.variant === 'pill'
    ? 'inline-flex shrink-0 items-center gap-1 rounded-full border border-border/70 bg-muted/40 px-2 py-1'
    : 'flex shrink-0 items-center gap-1',
)
</script>

<template>
  <div :class="rootClass" data-testid="modem-signal-status">
    <component
      :is="signalIconComponent"
      class="shrink-0"
      :class="[iconSizeClass, signalToneClass, isSearching && 'animate-pulse']"
      :title="signalTitle"
    />
    <span v-if="props.showSignalValue" :class="numberClass">
      {{ signalValue }}
    </span>
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
      v-if="accessTechnologyLabel"
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
  </div>
</template>
