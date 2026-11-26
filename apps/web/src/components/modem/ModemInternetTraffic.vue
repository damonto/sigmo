<script setup lang="ts">
import { computed, onUnmounted, useTemplateRef, watch } from 'vue'
import { usePreferredReducedMotion } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import RegionFlag from '@/components/RegionFlag.vue'
import {
  TRAFFIC_WINDOW_MS,
  type TrafficPoint,
  type TrafficRates,
} from '@/composables/useModemTraffic'
import type { InternetPublicResponse } from '@/types/internet'

const props = defineProps<{
  rates: TrafficRates | null
  usedBytes: number | null
  history: TrafficPoint[]
  publicInfo: InternetPublicResponse | null
}>()

const { locale, t } = useI18n()
const reducedMotion = usePreferredReducedMotion()
const chart = useTemplateRef<SVGSVGElement>('chart')
const formatter = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }))

const format = (value: number | null, units: string[]) => {
  if (value === null) return { value: '—', unit: '' }
  let index = 0
  while (value >= 1000 && index < units.length - 1) {
    value /= 1000
    index++
  }
  return { value: formatter.value.format(value), unit: units[index] }
}

const speedUnits = ['b/s', 'kb/s', 'Mb/s', 'Gb/s']
const download = computed(() => format(props.rates?.download ?? null, speedUnits))
const upload = computed(() => format(props.rates?.upload ?? null, speedUnits))
const usage = computed(() => format(props.usedBytes, ['B', 'kB', 'MB', 'GB', 'TB']))
const idle = computed(() => props.rates?.download === 0 && props.rates.upload === 0)
const latestPoint = computed(() => props.history[props.history.length - 1])
const publicIP = computed(() => props.publicInfo?.ip?.trim() ?? '')
const publicISP = computed(
  () => props.publicInfo?.isp?.trim() || props.publicInfo?.organization?.trim(),
)
const publicRegion = computed(() => {
  const code = props.publicInfo?.country?.trim().toUpperCase() ?? ''
  return /^[A-Z]{2}$/.test(code) ? code : ''
})

const paths = computed(() => {
  const first = props.history[0]
  const latest = latestPoint.value
  if (!first || !latest) return { line: 'M 0 92 H 1000', area: '' }
  const peak = Math.max(1000, ...props.history.map((point) => point.bitsPerSecond))
  const xFor = (elapsed: number) =>
    1000 * (1 - (latest.elapsedMilliseconds - elapsed) / TRAFFIC_WINDOW_MS)
  const yFor = (rate: number) => 92 - (rate / peak) * 74
  const firstX = xFor(first.elapsedMilliseconds)
  let previousX = firstX
  let previousY = yFor(first.bitsPerSecond)
  let line = `M ${firstX} ${previousY}`
  for (const point of props.history.slice(1)) {
    const x = xFor(point.elapsedMilliseconds)
    const y = yFor(point.bitsPerSecond)
    const middle = (previousX + x) / 2
    line += ` C ${middle} ${previousY} ${middle} ${y} ${x} ${y}`
    previousX = x
    previousY = y
  }
  return { line, area: `${line} V 100 H ${firstX} Z` }
})

let animation: Animation | undefined
watch(
  [latestPoint, reducedMotion],
  ([latest, motion], [previous]) => {
    animation?.cancel()
    if (
      !latest ||
      !previous ||
      motion === 'reduce' ||
      latest.bitsPerSecond === 0 ||
      latest.bitsPerSecond === previous.bitsPerSecond
    )
      return

    const shift =
      ((latest.elapsedMilliseconds - previous.elapsedMilliseconds) / TRAFFIC_WINDOW_MS) * 100
    animation = chart.value?.animate?.(
      [{ transform: `translateX(${shift}%)` }, { transform: 'translateX(0)' }],
      { duration: 450, easing: 'ease-out' },
    )
  },
  { flush: 'post' },
)
onUnmounted(() => animation?.cancel())
</script>

<template>
  <section
    :aria-label="t('modemDetail.overview.traffic.title')"
    class="relative isolate flex min-w-0 flex-col gap-2.5 overflow-hidden rounded-lg border border-primary/15 bg-accent/70 px-4 py-3 lg:rounded-none lg:border-0 lg:border-s lg:bg-transparent"
  >
    <svg
      ref="chart"
      viewBox="0 0 1000 100"
      preserveAspectRatio="none"
      class="pointer-events-none absolute inset-x-0 bottom-0 -z-10 h-3/5 w-full text-primary mask-[linear-gradient(90deg,transparent,black_65%)]"
      aria-hidden="true"
    >
      <path
        :d="paths.area"
        fill="currentColor"
        class="opacity-[0.09]"
      />
      <path
        :d="paths.line"
        fill="none"
        stroke="currentColor"
        stroke-width="1.25"
        vector-effect="non-scaling-stroke"
        class="opacity-40"
      />
    </svg>
    <div class="flex flex-wrap items-center justify-between gap-x-2 text-xs">
      <span class="flex items-center gap-1.5 font-medium text-emerald-800 dark:text-emerald-300">
        <span
          class="size-1.5 shrink-0 rounded-full bg-current"
          aria-hidden="true"
        />
        {{ t('modemDetail.overview.traffic.connected') }}
      </span>
      <span class="text-muted-foreground">
        {{ t(idle ? 'modemDetail.overview.traffic.idle' : 'modemDetail.overview.traffic.window') }}
      </span>
    </div>
    <dl class="grid grid-cols-2 gap-2">
      <div>
        <dt class="text-xs text-muted-foreground">
          <span
            class="text-primary"
            aria-hidden="true"
            >↓</span
          >
          {{ t('modemDetail.overview.traffic.download') }}
        </dt>
        <dd class="text-xl leading-tight font-semibold tracking-tight tabular-nums">
          {{ download.value }}
          <small class="text-xs font-normal text-muted-foreground">{{ download.unit }}</small>
        </dd>
      </div>
      <div>
        <dt class="text-xs text-muted-foreground">
          <span
            class="text-primary"
            aria-hidden="true"
            >↑</span
          >
          {{ t('modemDetail.overview.traffic.upload') }}
        </dt>
        <dd class="text-xl leading-tight font-semibold tracking-tight tabular-nums">
          {{ upload.value }}
          <small class="text-xs font-normal text-muted-foreground">{{ upload.unit }}</small>
        </dd>
      </div>
    </dl>
    <dl class="mt-auto self-end rounded-md bg-accent/85 px-2 py-1 text-xs">
      <div class="flex flex-wrap items-baseline gap-x-2">
        <dt class="text-muted-foreground">{{ t('modemDetail.overview.traffic.usage') }}</dt>
        <dd class="font-semibold tabular-nums">{{ usage.value }} {{ usage.unit }}</dd>
      </div>
    </dl>
    <dl
      v-if="publicIP"
      class="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-primary/15 pt-2 text-xs"
    >
      <div
        v-if="publicRegion || publicISP"
        class="flex min-w-0 items-center gap-1.5"
      >
        <dt class="sr-only">{{ t('modemDetail.settings.internetEgressProvider') }}</dt>
        <dd class="flex min-w-0 items-center gap-1.5 font-medium">
          <RegionFlag
            v-if="publicRegion"
            :region-code="publicRegion"
            class="shrink-0 rounded-sm"
            role="img"
          />
          <span
            v-if="publicISP"
            class="wrap-anywhere"
            >{{ publicISP }}</span
          >
        </dd>
      </div>
      <div class="ms-auto min-w-0">
        <dt class="sr-only">{{ t('modemDetail.settings.internetEgressIP') }}</dt>
        <dd class="font-mono wrap-anywhere select-text">{{ publicIP }}</dd>
      </div>
    </dl>
  </section>
</template>
