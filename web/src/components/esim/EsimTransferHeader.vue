<script setup lang="ts">
import { ArrowRightLeft } from 'lucide-vue-next'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { DialogHeader, DialogTitle } from '@/components/ui/dialog'

const { t } = useI18n()
const focusTarget = ref<HTMLElement | null>(null)

const focus = () => {
  focusTarget.value?.focus({ preventScroll: true })
}

defineExpose({ focus })
</script>

<template>
  <DialogHeader class="text-left">
    <div class="flex gap-3 pr-8">
      <div class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
        <ArrowRightLeft class="size-4" />
      </div>
      <div ref="focusTarget" tabindex="-1" class="min-w-0 space-y-1 outline-none">
        <DialogTitle>{{ t('modemDetail.esim.transferTitle') }}</DialogTitle>
        <p class="text-sm text-muted-foreground">
          {{ t('modemDetail.esim.transferDescription') }}
        </p>
      </div>
    </div>
  </DialogHeader>
</template>
