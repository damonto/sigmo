<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'

const props = defineProps<{
  targetLabel: string
  isDeleting: boolean
}>()

const emit = defineEmits<{
  (event: 'confirm'): void
}>()

const open = defineModel<boolean>('open', { required: true })

const { t } = useI18n()

const title = computed(() =>
  t('modemDetail.messages.deleteTitle', { participant: props.targetLabel }),
)
</script>

<template>
  <AlertDialog v-model:open="open">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ title }}</AlertDialogTitle>
        <AlertDialogDescription>
          {{ t('modemDetail.messages.deleteDescription') }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel :disabled="props.isDeleting">
          {{ t('modemDetail.actions.cancel') }}
        </AlertDialogCancel>
        <Button
          variant="destructive"
          type="button"
          @click="emit('confirm')"
          :disabled="props.isDeleting"
        >
          <span v-if="props.isDeleting" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.actions.delete') }}
          </span>
          <span v-else>{{ t('modemDetail.actions.delete') }}</span>
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
