<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import EsimTransferSourceItem from '@/components/esim/EsimTransferSourceItem.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Spinner } from '@/components/ui/spinner'
import type { EsimTransferSource } from '@/types/esim'

const props = defineProps<{
  loading: boolean
  ready: boolean
  hasSources: boolean
  sources: EsimTransferSource[]
  ccidError: string
  errorMessage: string
}>()

const emit = defineEmits<{
  (event: 'select', source: EsimTransferSource): void
}>()

const { t } = useI18n()

const sourceDetail = (source: EsimTransferSource) => {
  if (source.type === 'ccid') return t('modemDetail.esim.transferCcid')
  return source.detail ?? ''
}
</script>

<template>
  <div class="min-h-0 min-w-0 space-y-6 overflow-visible">
    <div v-if="props.loading" class="flex items-center gap-3 rounded-lg bg-muted/40 p-4">
      <Spinner class="size-5" />
      <div class="min-w-0">
        <p class="text-sm font-medium">{{ t('modemDetail.esim.transferSource') }}</p>
        <p class="text-xs text-muted-foreground">
          {{ t('modemDetail.esim.transferRunning') }}
        </p>
      </div>
    </div>

    <div v-if="props.ready" class="min-w-0 space-y-4">
      <section class="min-w-0 space-y-3">
        <p class="text-sm font-medium">{{ t('modemDetail.esim.transferSource') }}</p>
        <Alert v-if="!props.hasSources" class="border-dashed">
          <AlertDescription>
            {{ t('modemDetail.esim.transferNoSources') }}
          </AlertDescription>
        </Alert>
        <Alert v-if="props.ccidError">
          <AlertDescription>
            {{ t('modemDetail.esim.transferCcidUnavailable') }}: {{ props.ccidError }}
          </AlertDescription>
        </Alert>
        <div class="-mx-1 min-w-0 space-y-2 px-1 py-1">
          <EsimTransferSourceItem
            v-for="source in props.sources"
            :key="`${source.type}-${source.id}`"
            :source="source"
            :detail="sourceDetail(source)"
            @select="emit('select', $event)"
          />
        </div>
      </section>
    </div>

    <Alert v-if="props.errorMessage" variant="destructive">
      <AlertDescription>
        {{ props.errorMessage }}
      </AlertDescription>
    </Alert>
  </div>
</template>
