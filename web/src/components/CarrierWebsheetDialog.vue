<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import EsimPersistentDialogContent from '@/components/esim/EsimPersistentDialogContent.vue'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { getStoredToken } from '@/lib/auth-storage'
import { resolveAPIURL } from '@/lib/api-url'
import { useFetch } from '@/lib/fetch'
import type { CarrierWebsheetInfo } from '@/types/websheet'

const props = defineProps<{
  open: boolean
  websheet: CarrierWebsheetInfo | null
}>()

const emit = defineEmits<{
  (event: 'cancel'): void
  (event: 'done'): void
}>()

const { t } = useI18n()
const loaded = ref(false)
const completing = ref(false)
const iframeEl = ref<HTMLIFrameElement | null>(null)

const iframeSrc = computed(() => {
  if (!props.websheet?.embedUrl) return ''
  const u = new URL(resolveAPIURL(props.websheet.embedUrl))
  const token = getStoredToken()
  if (token) {
    u.searchParams.set('token', token)
  }
  return u.toString()
})

const title = computed(
  () => props.websheet?.title || t('modemDetail.esim.carrierWebsheetTitle'),
)

const complete = async () => {
  const id = props.websheet?.id
  if (!id || completing.value) return
  completing.value = true
  try {
    await useFetch<void>(`websheets/${id}/done`, { method: 'POST' }).json()
    emit('done')
  } finally {
    completing.value = false
  }
}

const isTerminalCallback = (callback: unknown) => {
  if (!callback || typeof callback !== 'object') return true
  const record = callback as { event?: unknown; method?: unknown; resultCode?: unknown }
  const value = String(record.event ?? record.method ?? record.resultCode ?? '').toLowerCase()
  if (!value) return true
  if (value.includes('phoneservicesaccountstatuschanged')) return false
  return true
}

const onMessage = (event: MessageEvent) => {
  if (event.source !== iframeEl.value?.contentWindow) return
  const data = event.data as { type?: unknown; callback?: unknown } | null
  if (!data || data.type !== 'sigmo-websheet-callback') return
  if (isTerminalCallback(data.callback)) {
    emit('done')
  }
}

onMounted(() => window.addEventListener('message', onMessage))
onUnmounted(() => window.removeEventListener('message', onMessage))

watch(
  () => props.websheet?.id,
  () => {
    loaded.value = false
    completing.value = false
  },
)
</script>

<template>
  <Dialog :open="props.open">
    <EsimPersistentDialogContent class="flex h-[85dvh] max-h-180 flex-col overflow-hidden sm:max-w-4xl">
      <DialogHeader class="shrink-0">
        <DialogTitle>{{ title }}</DialogTitle>
        <DialogDescription class="sr-only">{{ title }}</DialogDescription>
      </DialogHeader>

      <div class="relative min-h-0 flex-1 overflow-hidden rounded-md border bg-background">
        <div
          v-if="!loaded"
          class="absolute inset-0 z-10 flex items-center justify-center bg-background/80"
        >
          <Spinner class="size-5" />
        </div>
        <iframe
          v-if="iframeSrc"
          ref="iframeEl"
          :key="iframeSrc"
          :src="iframeSrc"
          class="size-full border-0"
          sandbox="allow-forms allow-scripts allow-popups"
          @load="loaded = true"
        />
      </div>

      <DialogFooter class="shrink-0 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Button variant="outline" type="button" class="order-2 w-full sm:order-1" @click="emit('cancel')">
          {{ t('modemDetail.actions.cancel') }}
        </Button>
        <Button type="button" class="order-1 w-full sm:order-2" :disabled="completing" @click="complete">
          <span v-if="completing" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.actions.confirm') }}
          </span>
          <span v-else>{{ t('modemDetail.esim.carrierWebsheetDone') }}</span>
        </Button>
      </DialogFooter>
    </EsimPersistentDialogContent>
  </Dialog>
</template>
