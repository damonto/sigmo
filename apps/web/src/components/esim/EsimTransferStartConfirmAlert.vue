<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import EsimPersistentAlertDialogContent from '@/components/esim/EsimPersistentAlertDialogContent.vue'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  (event: 'cancel'): void
  (event: 'confirm'): void
}>()

const { t } = useI18n()
</script>

<template>
  <AlertDialog :open="props.open">
    <EsimPersistentAlertDialogContent class="sm:max-w-sm">
      <AlertDialogHeader class="text-left">
        <AlertDialogTitle>{{ t('modemDetail.esim.transferConfirmTitle') }}</AlertDialogTitle>
        <AlertDialogDescription>
          {{ t('modemDetail.esim.transferConfirmDescription') }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <AlertDialogCancel class="order-2 w-full sm:order-1" @click="emit('cancel')">
          {{ t('modemDetail.actions.cancel') }}
        </AlertDialogCancel>
        <AlertDialogAction class="order-1 w-full sm:order-2" @click="emit('confirm')">
          {{ t('modemDetail.esim.transferConfirmContinue') }}
        </AlertDialogAction>
      </AlertDialogFooter>
    </EsimPersistentAlertDialogContent>
  </AlertDialog>
</template>
