<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import type { ModeResponse } from '@/types/network'

const selectedMode = defineModel<string>('selectedMode', { required: true })

const props = defineProps<{
  modeOptions: ModeResponse[]
  isSettingsLoading: boolean
  isModeUpdating: boolean
  canUpdateMode: boolean
}>()

const emit = defineEmits<{
  (event: 'updateMode'): void
}>()

const { t } = useI18n()

const hasModeOptions = computed(() => props.modeOptions.length > 0)

const modeLabel = (mode: ModeResponse) => {
  if (mode.preferred === 0 || mode.preferredLabel === 'None') {
    return mode.allowedLabel
  }
  return `${mode.allowedLabel} / ${mode.preferredLabel}`
}
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.networkModeTitle') }}
      </CardTitle>
      <CardAction>
        <Button
          size="sm"
          type="button"
          :disabled="!props.canUpdateMode"
          @click="emit('updateMode')"
        >
          <span v-if="props.isModeUpdating" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.settings.networkApply') }}
          </span>
          <span v-else>{{ t('modemDetail.settings.networkApply') }}</span>
        </Button>
      </CardAction>
    </CardHeader>

    <CardContent class="space-y-3 px-4">
      <Select
        v-model="selectedMode"
        :disabled="props.isSettingsLoading || props.isModeUpdating || !hasModeOptions"
      >
        <SelectTrigger class="w-full">
          <SelectValue :placeholder="t('modemDetail.settings.networkModePlaceholder')" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="mode in props.modeOptions"
            :key="`${mode.allowed}:${mode.preferred}`"
            :value="`${mode.allowed}:${mode.preferred}`"
          >
            {{ modeLabel(mode) }}
          </SelectItem>
        </SelectContent>
      </Select>

      <p v-if="!hasModeOptions && !props.isSettingsLoading" class="text-xs text-muted-foreground">
        {{ t('modemDetail.settings.networkModeEmpty') }}
      </p>
    </CardContent>
  </Card>
</template>
