<script setup lang="ts">
import { CheckCircle2, CircleX } from 'lucide-vue-next'
import { computed } from 'vue'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

const props = defineProps<{
  open: boolean
  title: string
  message: string
  confirmLabel: string
  tone: 'success' | 'error'
}>()

const emit = defineEmits<{
  (event: 'confirm'): void
}>()

const isError = computed(() => props.tone === 'error')
const messageClass = computed(() => (isError.value ? 'text-destructive' : 'text-muted-foreground'))
const iconClass = computed(() => (isError.value ? 'text-destructive' : 'text-emerald-600'))
const iconWrapperClass = computed(() =>
  isError.value
    ? 'mx-auto flex size-16 items-center justify-center rounded-full bg-destructive/10'
    : 'mx-auto flex size-16 items-center justify-center rounded-full bg-emerald-50',
)
const iconComponent = computed(() => (isError.value ? CircleX : CheckCircle2))
</script>

<template>
  <AlertDialog :open="props.open">
    <AlertDialogContent>
      <AlertDialogHeader class="min-w-0 w-full items-center text-center">
        <AlertDialogTitle>{{ title }}</AlertDialogTitle>
        <div :class="iconWrapperClass">
          <component :is="iconComponent" class="size-8" :class="iconClass" />
        </div>
        <AlertDialogDescription
          :class="[messageClass, 'min-w-0 w-full whitespace-pre-wrap wrap-break-word']"
        >
          {{ message }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogAction @click="emit('confirm')">
          {{ confirmLabel }}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
