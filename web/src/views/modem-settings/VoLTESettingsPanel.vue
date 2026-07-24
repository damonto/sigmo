<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import type { VoLTEDataPath, VoLTEQMIDataPath } from '@/types/volte'

const props = defineProps<{
  enabled: boolean
  dataPath: VoLTEDataPath
  modemRegistered: boolean
  isLoading: boolean
  isUpdating: boolean
}>()

const emit = defineEmits<{
  (event: 'update', enabled: boolean): void
  (event: 'update-data-path', dataPath: VoLTEQMIDataPath): void
}>()

const { t } = useI18n()

const description = computed(() => {
  if (props.enabled) return t('modemDetail.settings.volteManagedDescription')
  if (props.modemRegistered) return t('modemDetail.settings.volteModemRegisteredDescription')
  return t('modemDetail.settings.volteDescription')
})

const dataPathDisabled = computed(() => props.enabled || props.isLoading || props.isUpdating)

const updateDataPath = (dataPath: unknown) => {
  if (dataPath !== 'qmap' && dataPath !== 'legacy_bam_dmux') return
  emit('update-data-path', dataPath)
}
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">{{ t('modemDetail.settings.volteTitle') }}</CardTitle>
    </CardHeader>
    <CardContent class="space-y-5 px-4">
      <div v-if="props.dataPath !== 'mbim'" class="space-y-2">
        <Label for="volte-data-path">
          {{ t('modemDetail.settings.volteDataPathLabel') }}
        </Label>
        <RadioGroup
          class="gap-2"
          :model-value="props.dataPath"
          :disabled="dataPathDisabled"
          @update:model-value="updateDataPath"
        >
          <label
            class="flex items-start gap-3 rounded-lg border px-3 py-3 shadow-sm transition"
            :class="[
              props.dataPath === 'qmap'
                ? 'border-primary/40 bg-primary/5'
                : 'border-transparent bg-muted/30',
              dataPathDisabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer',
            ]"
          >
            <RadioGroupItem id="volte-data-path-qmap" value="qmap" class="mt-1" />
            <span class="min-w-0 space-y-1">
              <span class="flex items-center gap-2">
                <span class="text-sm font-semibold text-foreground">
                  {{ t('modemDetail.settings.volteDataPathQMAP') }}
                </span>
                <span
                  class="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary"
                >
                  {{ t('modemDetail.settings.volteDataPathDefault') }}
                </span>
              </span>
              <span class="block text-xs leading-5 text-muted-foreground">
                {{ t('modemDetail.settings.volteDataPathQMAPDescription') }}
              </span>
            </span>
          </label>

          <label
            class="flex items-start gap-3 rounded-lg border px-3 py-3 shadow-sm transition"
            :class="[
              props.dataPath === 'legacy_bam_dmux'
                ? 'border-primary/40 bg-primary/5'
                : 'border-transparent bg-muted/30',
              dataPathDisabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer',
            ]"
          >
            <RadioGroupItem id="volte-data-path-legacy" value="legacy_bam_dmux" class="mt-1" />
            <span class="min-w-0 space-y-1">
              <span class="block text-sm font-semibold text-foreground">
                {{ t('modemDetail.settings.volteDataPathLegacy') }}
              </span>
              <span class="block text-xs leading-5 text-muted-foreground">
                {{ t('modemDetail.settings.volteDataPathLegacyDescription') }}
              </span>
            </span>
          </label>
        </RadioGroup>
      </div>

      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1 space-y-1">
          <Label for="volte-enabled">{{ t('modemDetail.settings.volteLabel') }}</Label>
          <p class="text-xs leading-5 text-muted-foreground">{{ description }}</p>
        </div>
        <div class="inline-flex shrink-0 items-center gap-2">
          <Spinner v-if="props.isUpdating" class="size-4 text-muted-foreground" />
          <Switch
            id="volte-enabled"
            :model-value="props.enabled"
            :disabled="props.isLoading || props.isUpdating"
            @update:model-value="emit('update', $event === true)"
          />
        </div>
      </div>
    </CardContent>
  </Card>
</template>
