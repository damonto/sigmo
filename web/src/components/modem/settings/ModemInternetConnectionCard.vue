<script setup lang="ts">
import { computed } from 'vue'
import { Plug, Unplug } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import type { InternetConnectionResponse } from '@/types/internet'

const apn = defineModel<string>('apn', { required: true })
const defaultRoute = defineModel<boolean>('defaultRoute', { required: true })
const proxyEnabled = defineModel<boolean>('proxyEnabled', { required: true })
const alwaysOn = defineModel<boolean>('alwaysOn', { required: true })

const props = defineProps<{
  connection: InternetConnectionResponse | null
  isLoading: boolean
  isConnecting: boolean
  isDisconnecting: boolean
  isConnected: boolean
  canConnect: boolean
}>()

const emit = defineEmits<{
  (event: 'connect'): void
  (event: 'disconnect'): void
}>()

const { t } = useI18n()

const isInputDisabled = computed(() => props.isLoading || props.isConnecting || props.isConnected)
const isActionLoading = computed(
  () => props.isLoading || props.isConnecting || props.isDisconnecting,
)
const isActionDisabled = computed(() => {
  if (props.isLoading) return true
  if (props.isConnected) return props.isDisconnecting
  return !props.canConnect || props.isConnecting || props.isDisconnecting
})
const actionLabel = computed(() => {
  if (props.isConnected) return t('modemDetail.settings.internetDisconnect')
  return t('modemDetail.settings.internetConnect')
})
const statusLabel = computed(() => {
  if (props.isConnected) return t('modemDetail.settings.internetConnected')
  return t('modemDetail.settings.internetDisconnected')
})
const ipv4Label = computed(() => formatList(props.connection?.ipv4Addresses))
const ipv6Label = computed(() => formatList(props.connection?.ipv6Addresses))
const dnsLabel = computed(() => formatList(props.connection?.dns))
const durationLabel = computed(() => formatDuration(props.connection?.durationSeconds ?? 0))
const txLabel = computed(() => formatBytes(props.connection?.txBytes ?? 0))
const rxLabel = computed(() => formatBytes(props.connection?.rxBytes ?? 0))
const routeMetricLabel = computed(() => {
  const metric = props.connection?.routeMetric ?? 0
  if (metric === 0) return t('modemDetail.settings.internetNone')
  return String(metric)
})

const handleAction = () => {
  if (props.isConnected) {
    emit('disconnect')
    return
  }
  emit('connect')
}

const formatList = (values?: string[]) => {
  if (!values || values.length === 0) return t('modemDetail.settings.internetNone')
  return values.join(', ')
}

const formatDuration = (seconds: number) => {
  const normalized = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(normalized / 3600)
  const minutes = Math.floor((normalized % 3600) / 60)
  const remainingSeconds = normalized % 60
  if (hours > 0) {
    return `${hours}h ${minutes}m ${remainingSeconds}s`
  }
  if (minutes > 0) {
    return `${minutes}m ${remainingSeconds}s`
  }
  return `${remainingSeconds}s`
}

const formatBytes = (bytes: number) => {
  const normalized = Math.max(0, bytes)
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = normalized
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  if (unitIndex === 0) return `${value} ${units[unitIndex]}`
  return `${value.toFixed(1)} ${units[unitIndex]}`
}
</script>

<template>
  <Card class="gap-4 rounded-2xl border-0 py-4 shadow-sm">
    <CardHeader class="flex grid-cols-none flex-row items-center justify-between gap-4 px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.internetTitle') }}
      </CardTitle>
      <span
        class="relative flex size-3 items-center justify-center"
        role="status"
        :aria-label="statusLabel"
        :title="statusLabel"
      >
        <span
          v-if="props.isConnected"
          class="absolute inline-flex size-full animate-ping rounded-full bg-emerald-500 opacity-70"
        />
        <span
          class="relative inline-flex size-2.5 rounded-full"
          :class="
            props.isConnected
              ? 'bg-emerald-500 shadow-[0_0_0_3px_rgba(16,185,129,0.16)]'
              : 'bg-muted-foreground/40'
          "
        />
        <span class="sr-only">{{ statusLabel }}</span>
      </span>
    </CardHeader>

    <CardContent class="space-y-4 px-4">
      <div class="space-y-2">
        <Label for="modem-internet-apn">{{ t('modemDetail.settings.internetAPNLabel') }}</Label>
        <Input
          id="modem-internet-apn"
          v-model="apn"
          :disabled="isInputDisabled"
          :placeholder="t('modemDetail.settings.internetAPNPlaceholder')"
        />
      </div>

      <div class="space-y-2">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0 flex-1 space-y-1">
            <Label for="modem-internet-default-route">
              {{ t('modemDetail.settings.internetDefaultRouteLabel') }}
            </Label>
            <p class="text-xs leading-5 text-muted-foreground">
              {{ t('modemDetail.settings.internetDefaultRouteDescription') }}
            </p>
          </div>
          <Switch
            id="modem-internet-default-route"
            :model-value="defaultRoute"
            :disabled="isInputDisabled"
            @update:model-value="(value: boolean) => (defaultRoute = value)"
          />
        </div>
      </div>

      <div class="space-y-2">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0 flex-1 space-y-1">
            <Label for="modem-internet-proxy">
              {{ t('modemDetail.settings.internetProxyLabel') }}
            </Label>
            <p class="text-xs leading-5 text-muted-foreground">
              {{ t('modemDetail.settings.internetProxyDescription') }}
            </p>
          </div>
          <Switch
            id="modem-internet-proxy"
            :model-value="proxyEnabled"
            :disabled="isInputDisabled"
            @update:model-value="(value: boolean) => (proxyEnabled = value)"
          />
        </div>
      </div>

      <div class="space-y-2">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0 flex-1 space-y-1">
            <Label for="modem-internet-always-on">
              {{ t('modemDetail.settings.internetAlwaysOnLabel') }}
            </Label>
            <p class="text-xs leading-5 text-muted-foreground">
              {{ t('modemDetail.settings.internetAlwaysOnDescription') }}
            </p>
          </div>
          <Switch
            id="modem-internet-always-on"
            :model-value="alwaysOn"
            :disabled="isInputDisabled"
            @update:model-value="(value: boolean) => (alwaysOn = value)"
          />
        </div>
      </div>

      <Button
        size="sm"
        type="button"
        class="w-full"
        :variant="props.isConnected ? 'outline' : 'default'"
        :disabled="isActionDisabled"
        @click="handleAction"
      >
        <Spinner v-if="isActionLoading" class="size-4" />
        <Unplug v-else-if="props.isConnected" class="size-4" />
        <Plug v-else class="size-4" />
        {{ actionLabel }}
      </Button>

      <div class="space-y-2 text-sm">
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">{{
            t('modemDetail.settings.internetInterface')
          }}</span>
          <span class="break-all text-right font-medium text-foreground">
            {{ props.connection?.interfaceName || t('modemDetail.settings.internetNone') }}
          </span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">{{ t('modemDetail.settings.internetIPv4') }}</span>
          <span class="break-all text-right font-medium text-foreground">{{ ipv4Label }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">{{ t('modemDetail.settings.internetIPv6') }}</span>
          <span class="break-all text-right font-medium text-foreground">{{ ipv6Label }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">{{ t('modemDetail.settings.internetDNS') }}</span>
          <span class="break-all text-right font-medium text-foreground">{{ dnsLabel }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">{{
            t('modemDetail.settings.internetDuration')
          }}</span>
          <span class="font-medium text-foreground">{{ durationLabel }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">{{ t('modemDetail.settings.internetTX') }}</span>
          <span class="font-medium text-foreground">{{ txLabel }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">{{ t('modemDetail.settings.internetRX') }}</span>
          <span class="font-medium text-foreground">{{ rxLabel }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">{{
            t('modemDetail.settings.internetRouteMetric')
          }}</span>
          <span class="font-medium text-foreground">{{ routeMetricLabel }}</span>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
