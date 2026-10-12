<script setup lang="ts">
import { Bot, Check, Copy, Download } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { writeClipboardText } from '@/lib/clipboard'
import { mcpClientConfig } from '@/lib/mcp'

const props = defineProps<{
  enabled: boolean
  endpointUrl: string
  isSaving: boolean
}>()

const emit = defineEmits<{
  download: []
  toggle: [enabled: boolean]
}>()

const { t } = useI18n()
const copied = ref('')
const clientConfig = computed(() => mcpClientConfig(props.endpointUrl))

const copyText = async (key: string, value: string) => {
  await writeClipboardText(value)
  copied.value = key
  window.setTimeout(() => {
    if (copied.value === key) copied.value = ''
  }, 1500)
}
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle class="flex items-center gap-2">
        <Bot class="size-4" />
        {{ t('settings.mcp.serviceTitle') }}
      </CardTitle>
      <CardDescription>{{ t('settings.mcp.serviceDescription') }}</CardDescription>
    </CardHeader>
    <CardContent class="space-y-4">
      <div class="flex items-center justify-between gap-4 rounded-lg border p-3">
        <div>
          <p class="text-sm font-medium">{{ t('settings.mcp.enabled') }}</p>
          <p class="text-xs text-muted-foreground">
            {{ t('settings.mcp.enabledDescription') }}
          </p>
        </div>
        <Switch
          data-testid="mcp-enabled"
          :model-value="props.enabled"
          :disabled="props.isSaving"
          @update:model-value="emit('toggle', $event === true)"
        />
      </div>

      <div class="space-y-2">
        <Label for="mcp-endpoint">{{ t('settings.mcp.endpoint') }}</Label>
        <div class="flex gap-2">
          <Input id="mcp-endpoint" :model-value="props.endpointUrl" readonly />
          <Button
            data-testid="copy-mcp-endpoint"
            variant="outline"
            size="icon"
            @click="copyText('endpoint', props.endpointUrl)"
          >
            <Check v-if="copied === 'endpoint'" class="size-4" />
            <Copy v-else class="size-4" />
          </Button>
        </div>
      </div>

      <div class="space-y-2">
        <Label>{{ t('settings.mcp.clientConfig') }}</Label>
        <pre class="max-h-56 overflow-auto rounded-lg bg-muted p-3 text-xs">{{ clientConfig }}</pre>
        <Button
          data-testid="copy-mcp-example-config"
          variant="outline"
          @click="copyText('example-config', clientConfig)"
        >
          <Check v-if="copied === 'example-config'" class="size-4" />
          <Copy v-else class="size-4" />
          {{ t('settings.mcp.copyConfig') }}
        </Button>
      </div>

      <Button data-testid="download-mcp-skill" variant="outline" @click="emit('download')">
        <Download class="size-4" />
        {{ t('settings.mcp.downloadSkill') }}
      </Button>
    </CardContent>
  </Card>
</template>
