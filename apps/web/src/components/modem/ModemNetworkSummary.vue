<script setup lang="ts">
import { computed } from 'vue'
import { Activity, Plane } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { useModemDisplay } from '@/composables/useModemDisplay'
import type { Modem } from '@/types/modem'

const props = defineProps<{ modem: Modem }>()
defineSlots<{ default?(): unknown }>()
const { t } = useI18n()
const { formatSignal, signalIcon } = useModemDisplay()
const ringSizes = ['size-11', 'size-24', 'size-36', 'size-48', 'size-60']

const hasSignal = computed(() => !props.modem.airplaneMode && props.modem.signalQuality > 0)
const signal = computed(() => (hasSignal.value ? formatSignal(props.modem.signalQuality) : '—'))
const signalSymbol = computed(() =>
  props.modem.airplaneMode ? Plane : signalIcon(props.modem.signalQuality),
)
const technology = computed(() =>
  props.modem.airplaneMode
    ? t('modemDetail.settings.networkAirplaneModeStatus')
    : props.modem.accessTechnology?.trim() || '—',
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
    class="contents lg:relative lg:block lg:overflow-hidden lg:rounded-xl lg:border lg:border-primary/15 lg:bg-linear-to-br lg:from-accent/50 lg:to-accent lg:px-5 lg:py-3.5"
  >
    <div
      :key="props.modem.id"
      class="pointer-events-none absolute -end-5 -top-10 hidden size-64 place-items-center text-primary/15 lg:grid"
      aria-hidden="true"
    >
      <span
        v-for="(size, index) in ringSizes"
        :key="size"
        :class="size"
        :style="{ animationDelay: `${index * 180}ms` }"
        class="col-start-1 row-start-1 rounded-full border border-current motion-safe:animate-network-ring"
      />
    </div>
    <div class="relative hidden space-y-2 lg:block">
      <div class="flex items-center gap-2">
        <Activity
          class="size-4 text-primary"
          aria-hidden="true"
        />
        <h2 class="text-sm font-semibold">{{ t('modemDetail.overview.network') }}</h2>
      </div>
      <div class="flex items-center gap-3">
        <component
          :is="signalSymbol"
          class="size-7 shrink-0"
          :class="hasSignal ? 'text-primary' : 'text-muted-foreground'"
          aria-hidden="true"
        />
        <div>
          <p class="text-3xl leading-none font-bold tracking-tight tabular-nums">{{ signal }}</p>
          <p class="mt-1 text-xs text-muted-foreground">{{ t('labels.signal') }}</p>
        </div>
      </div>
    </div>
    <div
      class="contents lg:relative lg:mt-3 lg:grid lg:grid-cols-3 lg:gap-3 lg:border-t lg:border-primary/15 lg:pt-2.5 lg:text-xs"
    >
      <dl class="hidden lg:col-span-2 lg:grid lg:grid-cols-2 lg:gap-3">
        <div>
          <dt class="text-muted-foreground">{{ t('labels.accessTech') }}</dt>
          <dd class="mt-1 font-medium wrap-anywhere">{{ technology }}</dd>
        </div>
        <div>
          <dt class="text-muted-foreground">{{ t('modemDetail.overview.voice') }}</dt>
          <dd class="mt-1 font-medium wrap-anywhere">{{ voiceServices }}</dd>
        </div>
      </dl>
      <div class="contents lg:block lg:min-w-0">
        <p class="hidden text-muted-foreground lg:block">{{ t('modemDetail.overview.simSlot') }}</p>
        <slot><p class="mt-1 hidden font-medium lg:block">—</p></slot>
      </div>
    </div>
  </div>
</template>
