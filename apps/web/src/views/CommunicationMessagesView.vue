<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import CommunicationMessagesPanel from '@/components/communication/CommunicationMessagesPanel.vue'
import CommunicationMessageThread from '@/components/communication/CommunicationMessageThread.vue'

const route = useRoute()
const { t } = useI18n()
const threadID = computed(() =>
  typeof route.params.threadId === 'string' ? route.params.threadId : '',
)
</script>

<template>
  <main
    class="grid gap-6 lg:h-(--modem-desktop-content-height) lg:min-h-0 lg:grid-cols-[22rem_minmax(0,1fr)] lg:overflow-hidden"
    :class="
      threadID
        ? 'h-[calc(100dvh-6.5rem-env(safe-area-inset-bottom))] overflow-hidden'
        : 'min-h-[calc(100dvh-6.5rem)]'
    "
  >
    <div
      class="min-w-0 lg:min-h-0"
      :class="
        threadID
          ? 'hidden lg:block lg:overflow-hidden lg:rounded-xl lg:border lg:bg-card/40 lg:p-4'
          : ''
      "
    >
      <CommunicationMessagesPanel :selected-thread="threadID" />
    </div>
    <CommunicationMessageThread
      v-if="threadID"
      :key="threadID"
    />
    <section
      v-else
      class="hidden min-h-0 rounded-xl border border-dashed border-border bg-card/40 p-8 lg:flex lg:items-center lg:justify-center"
    >
      <div class="max-w-sm text-center">
        <h2 class="text-lg font-semibold text-foreground">
          {{ t('modemDetail.messages.desktopEmptyTitle') }}
        </h2>
        <p class="mt-2 text-sm text-muted-foreground">
          {{ t('modemDetail.messages.desktopEmptyDescription') }}
        </p>
      </div>
    </section>
  </main>
</template>
