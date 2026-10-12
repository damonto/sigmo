<script setup lang="ts">
import type { AcceptableValue } from 'reka-ui'
import { computed } from 'vue'
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
import { Switch } from '@/components/ui/switch'
import type { Modem, WiFiCallingSettings, WiFiCallingUnderlay } from '@/types/modem'
import type { CarrierWebsheetInfo } from '@/types/websheet'

const props = defineProps<{
  enabled: boolean
  underlay: WiFiCallingUnderlay
  modems: Modem[]
  modemId: string
  isLoading: boolean
  isUpdating: boolean
  isWebsheetStarting: boolean
  state: string
  websheet: CarrierWebsheetInfo | null
}>()

const emit = defineEmits<{
  (event: 'update', settings: WiFiCallingSettings): void
  (event: 'start-websheet'): void
}>()

const { t } = useI18n()

const isInputDisabled = computed(() => props.isLoading || props.isUpdating)
const requiresWebsheet = computed(
  () => props.state === 'websheet_required' || props.websheet !== null,
)

const underlayValue = computed(() => {
  if (props.underlay.mode === 'modem' && props.underlay.modemId) {
    return `modem:${props.underlay.modemId}`
  }
  return props.underlay.mode
})

const currentModemConnected = computed(
  () => props.modems.find((modem) => modem.id === props.modemId)?.internetConnected === true,
)
const otherModems = computed(() =>
  props.modems.filter(
    (modem) => modem.id !== props.modemId && modem.internetConnected === true,
  ),
)

const missingSelectedModem = computed(() => {
  if (props.underlay.mode !== 'modem' || !props.underlay.modemId) return null
  return otherModems.value.some((modem) => modem.id === props.underlay.modemId)
    ? null
    : props.underlay.modemId
})

const emitSettings = (enabled: boolean, underlay: WiFiCallingUnderlay) => {
  emit('update', { enabled, underlay })
}

const updateEnabled = (enabled: boolean) => {
  emitSettings(enabled, props.underlay)
}

const updateUnderlay = (value: AcceptableValue) => {
  if (typeof value !== 'string') return
  if (value === 'system') {
    emitSettings(props.enabled, { mode: 'system' })
    return
  }
  if (value === 'self') {
    if (currentModemConnected.value) {
      emitSettings(props.enabled, { mode: 'self' })
    }
    return
  }
  const modemId = value.startsWith('modem:') ? value.slice('modem:'.length) : ''
  if (otherModems.value.some((modem) => modem.id === modemId)) {
    emitSettings(props.enabled, { mode: 'modem', modemId })
  }
}
</script>

<template>
  <Card class="gap-4 border-0 py-4 shadow-sm">
    <CardHeader class="flex grid-cols-none flex-row items-center justify-between gap-4 px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.wifiCallingTitle') }}
      </CardTitle>
    </CardHeader>

    <CardContent class="space-y-4 px-4">
      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1 space-y-1">
          <Label for="modem-wifi-calling">
            {{ t('modemDetail.settings.wifiCallingLabel') }}
          </Label>
          <p class="text-xs leading-5 text-muted-foreground">
            {{ t('modemDetail.settings.wifiCallingDescription') }}
          </p>
        </div>
        <Switch
          id="modem-wifi-calling"
          :model-value="props.enabled"
          :disabled="isInputDisabled"
          @update:model-value="updateEnabled"
        />
      </div>

      <div class="space-y-2">
        <div class="space-y-1">
          <Label for="modem-wifi-calling-underlay">
            {{ t('modemDetail.settings.wifiCallingUnderlayLabel') }}
          </Label>
          <p class="text-xs leading-5 text-muted-foreground">
            {{ t('modemDetail.settings.wifiCallingUnderlayDescription') }}
          </p>
        </div>
        <Select
          :model-value="underlayValue"
          :disabled="isInputDisabled"
          @update:model-value="updateUnderlay"
        >
          <SelectTrigger id="modem-wifi-calling-underlay" class="w-full">
            <SelectValue :placeholder="t('modemDetail.settings.wifiCallingUnderlayPlaceholder')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="system">
              {{ t('modemDetail.settings.wifiCallingUnderlaySystem') }}
            </SelectItem>
            <SelectItem
              v-if="currentModemConnected || props.underlay.mode === 'self'"
              value="self"
              :disabled="!currentModemConnected"
            >
              {{ t('modemDetail.settings.wifiCallingUnderlaySelf') }}
            </SelectItem>
            <SelectItem
              v-if="missingSelectedModem"
              :value="`modem:${missingSelectedModem}`"
              disabled
            >
              {{
                t('modemDetail.settings.wifiCallingUnderlayMissingModem', {
                  id: missingSelectedModem,
                })
              }}
            </SelectItem>
            <SelectItem v-for="modem in otherModems" :key="modem.id" :value="`modem:${modem.id}`">
              {{
                t('modemDetail.settings.wifiCallingUnderlayModem', {
                  name: modem.name,
                  id: modem.id,
                })
              }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div v-if="requiresWebsheet" class="rounded-md border border-dashed p-3 text-sm">
        <p class="text-muted-foreground">
          {{ t('modemDetail.settings.wifiCallingWebsheetRequired') }}
        </p>
        <Button
          size="sm"
          type="button"
          variant="outline"
          class="mt-3 w-full"
          :disabled="props.isWebsheetStarting"
          @click="emit('start-websheet')"
        >
          <span v-if="props.isWebsheetStarting" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.settings.wifiCallingWebsheetAction') }}
          </span>
          <span v-else>{{ t('modemDetail.settings.wifiCallingWebsheetAction') }}</span>
        </Button>
      </div>
    </CardContent>
  </Card>
</template>
