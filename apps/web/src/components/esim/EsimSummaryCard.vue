<script setup lang="ts">
import { computed } from 'vue'
import { Database, FileText, Smartphone } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import CopyIdentifierButton from '@/components/modem/CopyIdentifierButton.vue'
import { Card, CardContent } from '@/components/ui/card'
import { formatByteSize } from '@/lib/formatByteSize'
import type { Modem } from '@/types/modem'
import type { SEsResponse } from '@/types/se'

const props = defineProps<{
  modem: Modem
  seInfo: SEsResponse | null
}>()

const { t } = useI18n()

const ses = computed(() => props.seInfo?.ses ?? [])
const primarySE = computed(() => ses.value[0])
const hasMultipleSEs = computed(() => ses.value.length > 1)
const imeiLabel = computed(() => t('modemDetail.fields.imei'))
const eidRows = computed(() => {
  const label = t('modemDetail.fields.eid')
  if (!hasMultipleSEs.value) {
    return [{ key: 'eid', label, value: primarySE.value?.eid }]
  }
  return ses.value.map((item, index) => ({
    key: item.id,
    label: `${label}${index + 1}`,
    value: item.eid,
  }))
})
</script>

<template>
  <Card
    class="@container/esim-summary min-w-0 gap-0 rounded-lg border bg-muted/60 py-0 shadow-none"
  >
    <CardContent
      class="grid gap-2 px-3 py-2.5 text-xs @min-[28rem]/esim-summary:grid-cols-2 @min-[28rem]/esim-summary:gap-x-4"
    >
      <div class="flex min-w-0 items-center justify-between gap-2">
        <div class="flex min-w-0 shrink-0 items-center gap-2">
          <Smartphone class="size-3 shrink-0 text-primary/70" />
          <span class="text-xs font-medium text-muted-foreground">
            {{ imeiLabel }}
          </span>
        </div>
        <div class="flex min-w-0 items-center justify-end gap-1">
          <span class="min-w-0 text-right font-mono font-medium wrap-anywhere text-foreground">
            {{ props.modem.id }}
          </span>
          <CopyIdentifierButton
            :field="imeiLabel"
            :value="props.modem.id"
          />
        </div>
      </div>

      <div class="min-w-0 space-y-2 @min-[28rem]/esim-summary:col-span-2">
        <div
          v-for="row in eidRows"
          :key="row.key"
          class="flex items-start justify-between gap-2"
        >
          <div class="flex min-w-0 shrink-0 items-center gap-2 pt-0.5">
            <FileText class="size-3 shrink-0 text-primary/70" />
            <span class="font-medium text-muted-foreground">{{ row.label }}</span>
          </div>
          <div class="flex min-w-0 flex-1 items-start justify-end gap-1">
            <span
              class="min-w-0 flex-1 text-right font-mono leading-5 font-medium wrap-anywhere text-foreground"
            >
              {{ row.value || 'N/A' }}
            </span>
            <CopyIdentifierButton
              :field="row.label"
              :value="row.value"
            />
          </div>
        </div>
      </div>

      <div
        class="flex min-w-0 items-center justify-between gap-2 @min-[28rem]/esim-summary:col-start-2 @min-[28rem]/esim-summary:row-start-1"
      >
        <div class="flex min-w-0 items-center gap-2">
          <Database class="size-3 shrink-0 text-primary/70" />
          <span class="text-xs font-medium text-muted-foreground">
            {{ t('modemDetail.fields.storageRemaining') }}
          </span>
        </div>
        <span
          v-if="!hasMultipleSEs"
          class="shrink-0 font-semibold text-foreground"
        >
          {{ formatByteSize(primarySE?.freeSpace) }}
        </span>
        <div
          v-else
          class="flex min-w-0 flex-wrap justify-end gap-x-2 gap-y-1 text-right"
        >
          <span
            v-for="(item, index) in ses"
            :key="`storage-${item.id}`"
            class="whitespace-nowrap font-semibold text-foreground"
          >
            SE{{ index + 1 }}: {{ formatByteSize(item.freeSpace) }}
          </span>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
