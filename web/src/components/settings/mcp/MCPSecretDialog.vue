<script setup lang="ts">
import { Check, Copy } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { writeClipboardText } from '@/lib/clipboard'
import { mcpClientConfig } from '@/lib/mcp'

const props = defineProps<{
  endpointUrl: string
  keyName: string
  open: boolean
  secret: string
}>()

const emit = defineEmits<{
  'update:open': [open: boolean]
}>()

const { t } = useI18n()
const copied = ref('')
const clientConfig = computed(() => mcpClientConfig(props.endpointUrl, props.secret || undefined))

const copyText = async (key: string, value: string) => {
  await writeClipboardText(value)
  copied.value = key
  window.setTimeout(() => {
    if (copied.value === key) copied.value = ''
  }, 1500)
}

watch(
  () => props.open,
  (open) => {
    if (!open) copied.value = ''
  },
)
</script>

<template>
  <Dialog :open="props.open" @update:open="emit('update:open', $event)">
    <DialogContent class="min-w-0 sm:max-w-xl">
      <DialogHeader class="min-w-0">
        <DialogTitle>{{ t('settings.mcp.secretTitle') }}</DialogTitle>
        <DialogDescription class="wrap-break-word">
          {{ t('settings.mcp.secretDescription', { name: props.keyName }) }}
        </DialogDescription>
      </DialogHeader>
      <div class="min-w-0 space-y-3">
        <div class="flex min-w-0 gap-2">
          <Input
            data-testid="mcp-secret"
            :model-value="props.secret"
            readonly
            class="min-w-0 flex-1 font-mono"
          />
          <Button
            data-testid="copy-mcp-secret"
            variant="outline"
            size="icon"
            class="shrink-0"
            @click="copyText('secret', props.secret)"
          >
            <Check v-if="copied === 'secret'" class="size-4" />
            <Copy v-else class="size-4" />
          </Button>
        </div>
        <pre class="max-h-56 min-w-0 max-w-full overflow-auto rounded-lg bg-muted p-3 text-xs">{{
          clientConfig
        }}</pre>
        <Button
          data-testid="copy-mcp-config"
          variant="outline"
          class="w-full"
          @click="copyText('config', clientConfig)"
        >
          <Check v-if="copied === 'config'" class="size-4" />
          <Copy v-else class="size-4" />
          {{ t('settings.mcp.copyConfig') }}
        </Button>
      </div>
    </DialogContent>
  </Dialog>
</template>
