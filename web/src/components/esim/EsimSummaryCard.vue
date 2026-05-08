<script setup lang="ts">
import { Check, Copy, Database, FileText, Smartphone } from 'lucide-vue-next'
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { writeClipboardText } from '@/lib/clipboard'
import type { EuiccApiResponse } from '@/types/euicc'
import type { Modem } from '@/types/modem'

const props = defineProps<{
  modem: Modem
  euicc: EuiccApiResponse | null
}>()

const { t } = useI18n()

// Format bytes to human-readable size
const formatBytes = (bytes: number) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${Math.round((bytes / Math.pow(k, i)) * 100) / 100} ${sizes[i]}`
}

const storageRemaining = computed(() => {
  return props.euicc ? formatBytes(props.euicc.freeSpace) : 'N/A'
})

const eid = computed(() => {
  return props.euicc?.eid || 'N/A'
})

type CopyField = 'imei' | 'eid'

const copiedField = ref<CopyField | ''>('')
const copyTimer = ref<number>()

const canCopy = (value: string) => value.trim() !== '' && value !== 'N/A'

const markCopied = (field: CopyField) => {
  copiedField.value = field
  if (copyTimer.value !== undefined) {
    window.clearTimeout(copyTimer.value)
  }
  copyTimer.value = window.setTimeout(() => {
    copiedField.value = ''
    copyTimer.value = undefined
  }, 1200)
}

const copyValue = async (field: CopyField, value: string) => {
  if (!canCopy(value)) return
  try {
    await writeClipboardText(value)
    markCopied(field)
  } catch (err) {
    console.error('[EsimSummaryCard] Failed to copy value:', err)
  }
}

onUnmounted(() => {
  if (copyTimer.value !== undefined) {
    window.clearTimeout(copyTimer.value)
  }
})
</script>

<template>
  <Card class="gap-0 rounded-xl border-0 bg-card/90 py-0 shadow-sm backdrop-blur-xl dark:bg-card/70">
    <CardContent class="space-y-3 px-4 py-3 text-sm">
      <div class="flex items-center justify-between gap-3">
        <div class="flex min-w-0 shrink-0 items-center gap-2">
          <Smartphone class="size-3 shrink-0 text-primary/70" />
          <span class="text-xs font-medium text-muted-foreground">
            {{ t('modemDetail.fields.imei') }}
          </span>
        </div>
        <div class="flex min-w-0 items-center justify-end gap-1">
          <span class="min-w-0 truncate font-mono text-xs font-medium text-foreground">
            {{ props.modem.id }}
          </span>
          <Button
            size="icon-sm"
            variant="ghost"
            type="button"
            class="size-5 rounded-md text-muted-foreground hover:text-foreground"
            :disabled="!canCopy(props.modem.id)"
            :title="t('modemDetail.actions.copy')"
            @click="copyValue('imei', props.modem.id)"
          >
            <Check v-if="copiedField === 'imei'" class="size-3 text-emerald-500" />
            <Copy v-else class="size-3" />
            <span class="sr-only">{{ t('modemDetail.actions.copy') }}</span>
          </Button>
        </div>
      </div>

      <div class="flex items-start justify-between gap-3">
        <div class="flex min-w-0 shrink-0 items-center gap-2 pt-0.5">
          <FileText class="size-3 shrink-0 text-primary/70" />
          <span class="text-xs font-medium text-muted-foreground">
            {{ t('modemDetail.fields.eid') }}
          </span>
        </div>
        <div class="flex min-w-0 flex-1 items-start justify-end gap-1">
          <span class="min-w-0 flex-1 break-all text-right font-mono text-xs font-medium leading-5 text-foreground">
            {{ eid }}
          </span>
          <Button
            size="icon-sm"
            variant="ghost"
            type="button"
            class="size-5 rounded-md text-muted-foreground hover:text-foreground"
            :disabled="!canCopy(eid)"
            :title="t('modemDetail.actions.copy')"
            @click="copyValue('eid', eid)"
          >
            <Check v-if="copiedField === 'eid'" class="size-3 text-emerald-500" />
            <Copy v-else class="size-3" />
            <span class="sr-only">{{ t('modemDetail.actions.copy') }}</span>
          </Button>
        </div>
      </div>

      <div class="flex items-center justify-between gap-3">
        <div class="flex min-w-0 items-center gap-2">
          <Database class="size-3 shrink-0 text-primary/70" />
          <span class="text-xs font-medium text-muted-foreground">
            {{ t('modemDetail.fields.storageRemaining') }}
          </span>
        </div>
        <span class="shrink-0 text-xs font-semibold text-foreground">
          {{ storageRemaining }}
        </span>
      </div>
    </CardContent>
  </Card>
</template>
