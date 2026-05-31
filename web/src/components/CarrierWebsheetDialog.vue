<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { X } from 'lucide-vue-next'
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
import { resolveAPIURL } from '@/lib/apiUrl'
import { getStoredToken } from '@/lib/authStorage'
import { fetchJson } from '@/lib/fetch'
import type { CarrierWebsheetInfo } from '@/types/websheet'

const props = withDefaults(
  defineProps<{
    open: boolean
    websheet: CarrierWebsheetInfo | null
    browserFrame?: boolean
    showFooter?: boolean
    closeOnStatusChange?: boolean
  }>(),
  {
    browserFrame: true,
    showFooter: false,
    closeOnStatusChange: false,
  },
)

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

const title = computed(() => props.websheet?.title || t('modemDetail.esim.carrierWebsheetTitle'))

const complete = async () => {
  const id = props.websheet?.id
  if (!id || completing.value) return
  completing.value = true
  try {
    await fetchJson<void>(`websheets/${id}/done`, { method: 'POST' })
    emit('done')
  } finally {
    completing.value = false
  }
}

const sendCallback = async (callback: unknown) => {
  const id = props.websheet?.id
  if (!id || !callback || typeof callback !== 'object') return
  try {
    await fetchJson<void>(`websheets/${id}/callback`, {
      method: 'POST',
      body: JSON.stringify(callback),
    })
  } catch (err) {
    console.error('[CarrierWebsheetDialog] Failed to relay websheet callback:', err)
  }
}

const isTerminalCallback = (callback: unknown) => {
  if (!callback || typeof callback !== 'object') return true
  const record = callback as { event?: unknown; method?: unknown; resultCode?: unknown }
  const value = String(record.event ?? record.method ?? record.resultCode ?? '').toLowerCase()
  if (!value) return true
  if (value.includes('phoneservicesaccountstatuschanged')) return props.closeOnStatusChange
  return true
}

const onMessage = (event: MessageEvent) => {
  if (event.source !== iframeEl.value?.contentWindow) return
  const data = event.data as { type?: unknown; callback?: unknown } | null
  if (!data || data.type !== 'sigmo-websheet-callback') return
  void sendCallback(data.callback)
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
    <EsimPersistentDialogContent
      :show-close-button="!props.browserFrame"
      :class="[
        'flex flex-col overflow-hidden',
        props.browserFrame
          ? 'h-[88dvh] max-h-184 gap-0! rounded-lg border-0! p-0! sm:max-w-2xl'
          : 'h-[85dvh] max-h-180 sm:max-w-[24rem]',
      ]"
    >
      <template v-if="props.browserFrame">
        <div class="grid shrink-0 grid-cols-[minmax(0,1fr)_2.25rem] items-center gap-2 border-b bg-muted/40 px-2 py-2">
          <div class="min-w-0 rounded-md border bg-background px-3 py-1.5 text-center text-xs text-muted-foreground shadow-xs">
            <span class="block truncate">{{ title }}</span>
          </div>
          <button
            type="button"
            class="inline-flex size-8 items-center justify-center justify-self-end rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden"
            :aria-label="t('modemDetail.actions.cancel')"
            @click="emit('cancel')"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>
        <DialogTitle class="sr-only">{{ title }}</DialogTitle>
        <DialogDescription class="sr-only">{{ title }}</DialogDescription>
      </template>

      <DialogHeader v-else class="shrink-0">
        <DialogTitle>{{ title }}</DialogTitle>
        <DialogDescription class="sr-only">{{ title }}</DialogDescription>
      </DialogHeader>

      <div
        :class="[
          'relative min-h-0 w-full flex-1 overflow-hidden bg-background',
          props.browserFrame ? '' : 'rounded-md border',
        ]"
      >
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
          class="block size-full border-0"
          sandbox="allow-forms allow-same-origin allow-scripts allow-popups allow-popups-to-escape-sandbox"
          @load="loaded = true"
        />
      </div>

      <DialogFooter
        v-if="props.showFooter"
        class="shrink-0 grid grid-cols-1 gap-3 sm:grid-cols-2"
      >
        <Button
          variant="outline"
          type="button"
          class="order-2 w-full sm:order-1"
          @click="emit('cancel')"
        >
          {{ t('modemDetail.actions.cancel') }}
        </Button>
        <Button
          type="button"
          class="order-1 w-full sm:order-2"
          :disabled="completing"
          @click="complete"
        >
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
