<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { RefreshCw } from 'lucide-vue-next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { CellInfoResponse } from '@/types/network'

const props = defineProps<{
  cellInfo: CellInfoResponse[]
  isCellInfoLoading: boolean
  hasCells: boolean
}>()

const emit = defineEmits<{
  (event: 'refreshCells'): void
}>()

const { t } = useI18n()

const cellDetails = (cell: CellInfoResponse) => {
  const fields = [
    { label: t('modemDetail.settings.cellOperator'), value: cell.operatorId },
    { label: 'LAC', value: cell.lac },
    { label: 'TAC', value: cell.tac },
    { label: t('modemDetail.settings.cellId'), value: cell.cellId },
    { label: 'PCI', value: cell.physicalCellId },
    { label: 'ARFCN', value: cell.arfcn },
    { label: 'UARFCN', value: cell.uarfcn },
    { label: 'EARFCN', value: cell.earfcn },
    { label: 'NRARFCN', value: cell.nrarfcn },
    { label: 'RSRP', value: cell.rsrp },
    { label: 'RSRQ', value: cell.rsrq },
    { label: 'SINR', value: cell.sinr },
    { label: t('modemDetail.settings.cellTimingAdvance'), value: cell.timingAdvance },
    { label: t('modemDetail.settings.cellBandwidth'), value: cell.bandwidth },
  ]
  return fields.filter((field) => field.value !== undefined && field.value !== '')
}
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.cellInfoTitle') }}
      </CardTitle>
      <CardAction>
        <Button
          size="icon"
          variant="outline"
          type="button"
          :disabled="props.isCellInfoLoading"
          :aria-label="t('modemDetail.settings.cellInfoRefresh')"
          @click="emit('refreshCells')"
        >
          <RefreshCw class="size-4" :class="{ 'animate-spin': props.isCellInfoLoading }" />
        </Button>
      </CardAction>
    </CardHeader>

    <CardContent class="space-y-3 px-4">
      <div v-if="props.hasCells" class="space-y-2">
        <div
          v-for="(cell, index) in props.cellInfo"
          :key="`${cell.typeValue}-${cell.cellId ?? index}`"
          class="space-y-2 rounded-lg border p-3"
        >
          <div class="flex items-center justify-between gap-3">
            <span class="font-medium text-foreground">{{ cell.type }}</span>
            <Badge :variant="cell.serving ? 'default' : 'secondary'">
              {{
                cell.serving
                  ? t('modemDetail.settings.cellServing')
                  : t('modemDetail.settings.cellNeighbor')
              }}
            </Badge>
          </div>
          <div class="grid gap-x-4 gap-y-1 text-xs sm:grid-cols-2">
            <div
              v-for="field in cellDetails(cell)"
              :key="field.label"
              class="flex items-center justify-between gap-2"
            >
              <span class="text-muted-foreground">{{ field.label }}</span>
              <span class="font-medium text-foreground">{{ field.value }}</span>
            </div>
          </div>
        </div>
      </div>
      <p v-else class="text-xs text-muted-foreground">
        {{ t('modemDetail.settings.cellInfoEmpty') }}
      </p>
    </CardContent>
  </Card>
</template>
