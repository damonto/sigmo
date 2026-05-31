<script setup lang="ts">
import { computed, ref } from 'vue'
import { CheckCircle2, ChevronDown, CircleX, Globe2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { InternetConnectionResponse } from '@/types/internet'

const apn = defineModel<string>('apn', { required: true })
const ipType = defineModel<string>('ipType', { required: true })
const apnUsername = defineModel<string>('apnUsername', { required: true })
const apnPassword = defineModel<string>('apnPassword', { required: true })
const apnAuth = defineModel<string>('apnAuth', { required: true })
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

const { t } = useI18n()
const advancedOpen = ref(false)

const isInputDisabled = computed(() => props.isLoading || props.isConnecting || props.isConnected)
const statusLabel = computed(() => {
  if (props.isConnected) return t('modemDetail.settings.internetConnected')
  return t('modemDetail.settings.internetDisconnected')
})
const statusDescription = computed(() => {
  if (props.isConnected) return t('modemDetail.settings.internetConnectedDescription')
  return t('modemDetail.settings.internetDisconnectedDescription')
})
const interfaceLabel = computed(
  () => props.connection?.interfaceName || t('modemDetail.settings.internetNone'),
)
const ipv4Label = computed(() => formatList(props.connection?.ipv4Addresses))
const ipv6Label = computed(() => formatList(props.connection?.ipv6Addresses))
const dnsLabel = computed(() => formatList(props.connection?.dns))
const durationLabel = computed(() => formatDuration(props.connection?.durationSeconds ?? 0))
const txLabel = computed(() => formatBytes(props.connection?.txBytes ?? 0))
const rxLabel = computed(() => formatBytes(props.connection?.rxBytes ?? 0))
const bandwidthLabel = computed(() => {
  const tx = props.connection?.txBytes ?? 0
  const rx = props.connection?.rxBytes ?? 0
  return formatBytes(tx + rx)
})
const ipTypeOptions = computed(() => [
  { value: 'ipv4v6', label: 'IPv4v6' },
  { value: 'ipv4', label: 'IPv4' },
  { value: 'ipv6', label: 'IPv6' },
])
const authOptions = computed(() => [
  { value: 'default', label: t('modemDetail.settings.internetAuthDefault') },
  { value: 'none', label: t('modemDetail.settings.internetAuthNone') },
  { value: 'pap', label: 'PAP' },
  { value: 'chap', label: 'CHAP' },
  { value: 'pap|chap', label: 'PAP / CHAP' },
  { value: 'mschap', label: 'MS-CHAP' },
  { value: 'mschapv2', label: 'MS-CHAP v2' },
  { value: 'eap', label: 'EAP' },
])
const routeMetricLabel = computed(() => {
  const metric = props.connection?.routeMetric ?? 0
  if (metric === 0) return t('modemDetail.settings.internetNone')
  return String(metric)
})

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
  <div class="space-y-3">
    <Card
      class="overflow-hidden py-0 shadow-sm"
      :class="
        props.isConnected
          ? 'border-emerald-500/20 bg-emerald-50/40 dark:bg-emerald-950/10'
          : 'border-primary/10 bg-card'
      "
    >
      <CardContent class="p-0">
        <div class="flex items-start gap-3 border-b border-border/70 p-4">
          <div
            class="relative flex size-11 shrink-0 items-center justify-center rounded-full"
            :class="props.isConnected ? 'bg-emerald-500/10 text-emerald-600' : 'bg-primary/10 text-primary'"
          >
            <Globe2 class="size-6" />
            <span
              class="absolute -bottom-0.5 -right-0.5 flex size-4 items-center justify-center rounded-full border-2 border-card"
              :class="props.isConnected ? 'bg-emerald-500 text-white' : 'bg-primary text-primary-foreground'"
            >
              <CheckCircle2 v-if="props.isConnected" class="size-3" />
              <CircleX v-else class="size-3" />
            </span>
          </div>
          <div class="min-w-0 flex-1">
            <p
              class="text-base font-semibold"
              :class="props.isConnected ? 'text-emerald-600' : 'text-primary'"
            >
              {{ statusLabel }}
            </p>
            <p class="truncate text-sm text-muted-foreground">
              {{ statusDescription }}
            </p>
          </div>
        </div>

        <div class="grid grid-cols-3 divide-x divide-border/70 px-2 py-3 text-center text-sm">
          <div class="min-w-0 px-2">
            <p class="truncate text-xs font-medium text-muted-foreground">
              {{ t('modemDetail.settings.internetInterface') }}
            </p>
            <p class="mt-1 truncate font-semibold text-foreground">
              {{ interfaceLabel }}
            </p>
          </div>
          <div class="min-w-0 px-2">
            <p class="truncate text-xs font-medium text-muted-foreground">
              {{ t('modemDetail.settings.internetDuration') }}
            </p>
            <p class="mt-1 truncate font-semibold text-foreground">
              {{ durationLabel }}
            </p>
          </div>
          <div class="min-w-0 px-2">
            <p class="truncate text-xs font-medium text-muted-foreground">
              {{ t('modemDetail.settings.internetBandwidth') }}
            </p>
            <p class="mt-1 truncate font-semibold text-foreground" :title="bandwidthLabel">
              {{ bandwidthLabel }}
            </p>
          </div>
        </div>
      </CardContent>
    </Card>

    <Card class="gap-4 border-0 py-4 shadow-sm">
      <CardHeader class="px-4">
        <CardTitle class="text-base">
          {{ t('modemDetail.settings.internetAPNProfileTitle') }}
        </CardTitle>
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
      </CardContent>
    </Card>

    <Card class="gap-4 border-0 py-4 shadow-sm">
      <CardContent class="space-y-4 px-4">
        <Collapsible v-model:open="advancedOpen" class="space-y-3">
          <CollapsibleTrigger
            class="flex w-full items-center justify-between gap-3 rounded-md text-left outline-none transition-colors hover:text-primary focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            <span class="min-w-0 space-y-1">
              <span class="block text-base font-semibold text-foreground">
                {{ t('modemDetail.settings.internetAdvancedTitle') }}
              </span>
              <span class="block text-xs leading-5 text-muted-foreground">
                {{ t('modemDetail.settings.internetAdvancedDescription') }}
              </span>
            </span>
            <ChevronDown
              class="size-4 shrink-0 transition-transform"
              :class="advancedOpen ? 'rotate-180' : ''"
            />
          </CollapsibleTrigger>
          <CollapsibleContent class="space-y-3">
            <div class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-2">
                <Label for="modem-internet-ip-type">
                  {{ t('modemDetail.settings.internetIPTypeLabel') }}
                </Label>
                <Select v-model="ipType" :disabled="isInputDisabled">
                  <SelectTrigger id="modem-internet-ip-type" class="w-full">
                    <SelectValue :placeholder="t('modemDetail.settings.internetIPTypePlaceholder')" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="option in ipTypeOptions"
                      :key="option.value"
                      :value="option.value"
                    >
                      {{ option.label }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div class="space-y-2">
                <Label for="modem-internet-apn-auth">
                  {{ t('modemDetail.settings.internetAPNAuthLabel') }}
                </Label>
                <Select v-model="apnAuth" :disabled="isInputDisabled">
                  <SelectTrigger id="modem-internet-apn-auth" class="w-full">
                    <SelectValue :placeholder="t('modemDetail.settings.internetAPNAuthPlaceholder')" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="option in authOptions"
                      :key="option.value"
                      :value="option.value"
                    >
                      {{ option.label }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-2">
                <Label for="modem-internet-apn-username">
                  {{ t('modemDetail.settings.internetAPNUsernameLabel') }}
                </Label>
                <Input
                  id="modem-internet-apn-username"
                  v-model="apnUsername"
                  :disabled="isInputDisabled"
                  :placeholder="t('modemDetail.settings.internetAPNUsernamePlaceholder')"
                />
              </div>

              <div class="space-y-2">
                <Label for="modem-internet-apn-password">
                  {{ t('modemDetail.settings.internetAPNPasswordLabel') }}
                </Label>
                <Input
                  id="modem-internet-apn-password"
                  v-model="apnPassword"
                  type="password"
                  :disabled="isInputDisabled"
                  :placeholder="t('modemDetail.settings.internetAPNPasswordPlaceholder')"
                />
              </div>
            </div>
          </CollapsibleContent>
        </Collapsible>
      </CardContent>
    </Card>

    <Card class="gap-4 border-0 py-4 shadow-sm">
      <CardHeader class="px-4">
        <CardTitle class="text-base">
          {{ t('modemDetail.settings.internetPreferencesTitle') }}
        </CardTitle>
      </CardHeader>
      <CardContent class="space-y-4 px-4">
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
      </CardContent>
    </Card>

    <Card class="gap-4 border-0 py-4 shadow-sm">
      <CardHeader class="px-4">
        <CardTitle class="text-base">
          {{ t('modemDetail.settings.internetIPInfoTitle') }}
        </CardTitle>
      </CardHeader>
      <CardContent class="space-y-2 px-4">
        <div class="space-y-2 text-sm">
          <div class="flex items-center justify-between gap-4">
            <span class="text-muted-foreground">{{
              t('modemDetail.settings.internetInterface')
            }}</span>
            <span class="break-all text-right font-medium text-foreground">
              {{ interfaceLabel }}
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
  </div>
</template>
