<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import type { CarrierWebsheetInfo } from '@/types/websheet'

const enabled = defineModel<boolean>('enabled', { required: true })
const preferred = defineModel<boolean>('preferred', { required: true })

const props = defineProps<{
  isLoading: boolean
  isUpdating: boolean
  isWebsheetStarting: boolean
  state: string
  websheet: CarrierWebsheetInfo | null
}>()

const emit = defineEmits<{
  (event: 'update'): void
  (event: 'start-websheet'): void
}>()

const { t } = useI18n()

const isInputDisabled = computed(() => props.isLoading || props.isUpdating)
const isActionDisabled = computed(() => props.isLoading || props.isUpdating)
const requiresWebsheet = computed(() => props.state === 'websheet_required' || props.websheet !== null)
</script>

<template>
  <Card class="gap-4 rounded-2xl border-0 py-4 shadow-sm">
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
          :model-value="enabled"
          :disabled="isInputDisabled"
          @update:model-value="(value: boolean) => (enabled = value)"
        />
      </div>

      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1 space-y-1">
          <Label for="modem-wifi-calling-preferred">
            {{ t('modemDetail.settings.wifiCallingPreferredLabel') }}
          </Label>
          <p class="text-xs leading-5 text-muted-foreground">
            {{ t('modemDetail.settings.wifiCallingPreferredDescription') }}
          </p>
        </div>
        <Switch
          id="modem-wifi-calling-preferred"
          :model-value="preferred"
          :disabled="isInputDisabled || !enabled"
          @update:model-value="(value: boolean) => (preferred = value)"
        />
      </div>

      <div class="flex justify-end">
        <Button
          size="sm"
          type="button"
          class="w-full"
          :disabled="isActionDisabled"
          @click="emit('update')"
        >
          <span v-if="props.isUpdating" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.actions.update') }}
          </span>
          <span v-else>{{ t('modemDetail.actions.update') }}</span>
        </Button>
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
