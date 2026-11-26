<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { localizeErrorMessage } from '@/lib/errorHandler'
import { INTERNET_SPEED_TEST_SIZES } from '@/types/internet'
import type { InternetSpeedTestResult, InternetSpeedTestSize } from '@/types/internet'

const props = defineProps<{
  result: InternetSpeedTestResult | null
  canRun: boolean
  running: boolean
  failure: unknown
  readFailed: boolean
}>()
const emit = defineEmits<{ test: [sizeMB: InternetSpeedTestSize] }>()
const { locale, t } = useI18n()
const base = 'modemDetail.settings.internetSpeedTest'
const sizeMB = ref<InternetSpeedTestSize>(INTERNET_SPEED_TEST_SIZES[0])
const formatter = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }))
const timeFormatter = computed(
  () => new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }),
)
const formatSpeed = (bps: number) => {
  const mbps = bps / 1_000_000
  return mbps < 0.01 ? `<${formatter.value.format(0.01)}` : formatter.value.format(mbps)
}
const errorMessage = computed(() =>
  props.failure ? localizeErrorMessage(props.failure, t(`${base}.failed`)) : '',
)
</script>

<template>
  <Card class="gap-3 border-0 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">{{ t(`${base}.title`) }}</CardTitle>
    </CardHeader>
    <CardContent class="space-y-3 px-4">
      <div
        aria-live="polite"
        class="space-y-2"
      >
        <dl
          v-if="result"
          class="grid grid-cols-2 gap-3"
        >
          <div>
            <dt class="text-sm text-muted-foreground">
              {{ t('modemDetail.overview.traffic.download') }}
            </dt>
            <dd class="font-medium tabular-nums">{{ formatSpeed(result.downloadBps) }} Mbps</dd>
          </div>
          <div>
            <dt class="text-sm text-muted-foreground">
              {{ t('modemDetail.overview.traffic.upload') }}
            </dt>
            <dd class="font-medium tabular-nums">{{ formatSpeed(result.uploadBps) }} Mbps</dd>
          </div>
        </dl>
        <p
          v-if="result"
          class="text-xs text-muted-foreground"
        >
          {{ t(`${base}.lastTest`, { time: timeFormatter.format(new Date(result.testedAt)) }) }}
        </p>
      </div>
      <p
        v-if="errorMessage || readFailed"
        role="status"
        class="text-sm text-destructive"
      >
        {{ errorMessage || t(`${base}.readFailed`) }}
      </p>
      <div class="flex flex-wrap items-end gap-3">
        <div class="space-y-2">
          <Label for="internet-speed-test-size">{{ t(`${base}.size`) }}</Label>
          <Select
            v-model="sizeMB"
            :disabled="running"
          >
            <SelectTrigger
              id="internet-speed-test-size"
              size="sm"
            >
              <SelectValue>{{ sizeMB }} MB</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="size in INTERNET_SPEED_TEST_SIZES"
                :key="size"
                :value="size"
              >
                {{ size }} MB
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          :disabled="!canRun"
          @click="emit('test', sizeMB)"
        >
          <Spinner
            v-if="running"
            class="size-4"
          />
          {{ t(`${base}.${running ? 'running' : result ? 'retest' : 'start'}`) }}
        </Button>
      </div>
      <p class="text-xs text-muted-foreground">{{ t(`${base}.dataUsage`, { size: sizeMB }) }}</p>
    </CardContent>
  </Card>
</template>
