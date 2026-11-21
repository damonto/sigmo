<script setup lang="ts">
import { Trash2 } from 'lucide-vue-next'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button, buttonVariants } from '@/components/ui/button'

const props = defineProps<{
  label: string
  title: string
  description: string
  cancelLabel: string
  disabled?: boolean
}>()
const emit = defineEmits<{ clear: [] }>()
</script>

<template>
  <AlertDialog>
    <AlertDialogTrigger as-child>
      <Button
        type="button"
        variant="ghost"
        class="text-destructive hover:text-destructive"
        :disabled="props.disabled"
      >
        <Trash2
          class="size-4"
          aria-hidden="true"
        />
        {{ props.label }}
      </Button>
    </AlertDialogTrigger>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ props.title }}</AlertDialogTitle>
        <AlertDialogDescription>{{ props.description }}</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>{{ props.cancelLabel }}</AlertDialogCancel>
        <AlertDialogAction
          :class="buttonVariants({ variant: 'destructive' })"
          @click="emit('clear')"
        >
          {{ props.label }}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
