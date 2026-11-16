<script setup lang="ts">
import { onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterView } from 'vue-router'
import { toast } from 'vue-sonner'
import 'vue-sonner/style.css'

import EsimTransferRequestProvider from '@/components/esim/EsimTransferRequestProvider.vue'
import ModemCallProvider from '@/components/modem/ModemCallProvider.vue'
import { Toaster } from '@/components/ui/sonner'
import { useAppUpdate } from '@/composables/useAppUpdate'
import { isApiError } from '@/lib/apiProblem'
import { localizeErrorMessage } from '@/lib/errorHandler'

const { t } = useI18n()
const update = useAppUpdate()
watch(update.failure, (failure) => {
  if (!failure) return
  toast.error(
    failure.kind === 'timeout'
      ? t('settings.updates.recoveryTimedOut')
      : localizeErrorMessage(
          isApiError(failure.error) ? failure.error : undefined,
          t('settings.updates.monitorFailed'),
        ),
  )
})
onUnmounted(update.dispose)
</script>

<template>
  <ModemCallProvider>
    <EsimTransferRequestProvider>
      <RouterView />
    </EsimTransferRequestProvider>
  </ModemCallProvider>
  <Toaster
    position="top-center"
    rich-colors
  />
</template>
