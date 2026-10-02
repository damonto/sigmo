<script setup lang="ts">
import { Download, X } from 'lucide-vue-next'

import { Button } from '@/components/ui/button'

// The notice remains useful after the call has ended, so it only receives the saved result.
const props = defineProps<{
  filename: string
  errorMessage: string
}>()
const emit = defineEmits<{
  download: []
  dismiss: []
}>()
</script>

<template>
  <div
    v-if="props.filename || props.errorMessage"
    class="fixed inset-x-0 bottom-4 z-40 mx-auto flex w-[calc(100%-1.5rem)] max-w-lg items-center gap-3 rounded-xl border bg-background/95 p-4 shadow-lg backdrop-blur"
  >
    <div class="min-w-0 flex-1">
      <p
        v-if="props.errorMessage"
        class="text-sm text-destructive"
        role="alert"
      >
        {{ props.errorMessage }}
      </p>
      <template v-if="props.filename">
        <p
          class="text-sm font-medium"
          role="status"
        >
          {{ $t('modemDetail.phone.recording.ready') }}
        </p>
        <p class="truncate text-xs text-muted-foreground">{{ props.filename }}</p>
        <p class="mt-1 text-xs text-muted-foreground">
          {{ $t('modemDetail.phone.recording.temporary') }}
        </p>
      </template>
    </div>
    <Button
      v-if="props.filename"
      size="icon"
      variant="outline"
      :aria-label="$t('modemDetail.phone.recording.download')"
      @click="emit('download')"
    >
      <Download class="size-4" />
    </Button>
    <Button
      size="icon"
      variant="ghost"
      :aria-label="$t('modemDetail.phone.recording.dismiss')"
      @click="emit('dismiss')"
    >
      <X class="size-4" />
    </Button>
  </div>
</template>
