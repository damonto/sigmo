<script setup lang="ts">
import { computed, onScopeDispose, ref } from 'vue'
import { Check, Copy } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { writeClipboardText } from '@/lib/clipboard'

const props = defineProps<{ field: string; value?: string }>()
const { t } = useI18n()
const value = computed(() => {
  const identifier = props.value?.trim()
  return identifier === 'N/A' ? undefined : identifier
})
const copiedValue = ref('')
const copied = computed(() => !!copiedValue.value && copiedValue.value === value.value)
const label = computed(() => t('modemDetail.actions.copyIdentifier', { field: props.field }))
let resetTimer: number | undefined
let disposed = false

const copy = async () => {
  const identifier = value.value
  if (!identifier) return
  try {
    await writeClipboardText(identifier)
    if (disposed || identifier !== value.value) return
    copiedValue.value = identifier
    window.clearTimeout(resetTimer)
    resetTimer = window.setTimeout(() => {
      copiedValue.value = ''
    }, 1200)
  } catch (error) {
    console.error('[CopyIdentifierButton] Could not copy identifier:', error)
  }
}

onScopeDispose(() => {
  disposed = true
  window.clearTimeout(resetTimer)
})
</script>

<template>
  <span class="relative inline-flex shrink-0">
    <Button
      size="icon-xs"
      variant="ghost"
      type="button"
      class="text-muted-foreground hover:text-foreground"
      :disabled="!value"
      :aria-label="label"
      :title="label"
      @click="copy"
    >
      <component
        :is="copied ? Check : Copy"
        :class="copied && 'text-emerald-500'"
        aria-hidden="true"
      />
    </Button>
    <span
      aria-live="polite"
      class="sr-only"
    >
      {{ copied ? t('modemDetail.actions.identifierCopied', { field: props.field }) : '' }}
    </span>
  </span>
</template>
