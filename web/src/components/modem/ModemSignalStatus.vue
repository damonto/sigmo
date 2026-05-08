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
    size?: 'sm' | 'md'
    variant?: 'inline' | 'pill'
  }>(),
  {
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
const iconSizeClass = computed(() => (props.size === 'md' ? 'size-5' : 'size-4'))
const numberClass = computed(() =>
  props.size === 'md'
    ? 'font-mono text-xs text-muted-foreground tabular-nums'
    : 'inline-flex h-4 min-w-6 items-center justify-end text-right font-mono text-xs text-muted-foreground tabular-nums',
)
const badgeClass = computed(() =>
  props.size === 'md' ? 'h-5 px-1.5 text-xs font-semibold' : 'h-4 px-1.5 text-xs font-semibold',
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
      :class="[badgeClass, registrationToneClass]"
      :aria-label="props.registrationState"
      :title="props.registrationState"
    >
      {{ registrationLabel }}
    </Badge>
    <span :class="numberClass">
      {{ signalValue }}
    </span>
  </div>
</template>
