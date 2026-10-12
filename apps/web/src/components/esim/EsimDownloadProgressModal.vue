<script setup lang="ts">
import { computed } from 'vue'

import EsimPersistentDialogContent from '@/components/esim/EsimPersistentDialogContent.vue'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'

const props = defineProps<{
  open: boolean
  title: string
  stageLabel: string
  progress: number
  cancelLabel: string
}>()

const emit = defineEmits<{
  (event: 'cancel'): void
}>()

const progressValue = computed(() => Math.min(Math.max(props.progress, 0), 100))
</script>

<template>
  <Dialog :open="props.open">
    <EsimPersistentDialogContent class="sm:max-w-sm" :show-close-button="false">
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
        <DialogDescription>{{ stageLabel }}</DialogDescription>
      </DialogHeader>
      <Progress :model-value="progressValue" />
      <DialogFooter>
        <Button variant="ghost" type="button" @click="emit('cancel')">
          {{ cancelLabel }}
        </Button>
      </DialogFooter>
    </EsimPersistentDialogContent>
  </Dialog>
</template>
