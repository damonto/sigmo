<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import ModemMessagesPanel from '@/components/modem/messages/ModemMessagesPanel.vue'

const route = useRoute()
const { t } = useI18n()

const modemId = computed(() => (route.params.id ?? 'unknown') as string)
</script>

<template>
  <div
    class="grid min-h-[calc(100dvh-6.5rem)] gap-6 lg:h-(--modem-desktop-content-height) lg:min-h-0 lg:grid-cols-[22rem_minmax(0,1fr)] lg:overflow-hidden"
  >
    <ModemMessagesPanel :modem-id="modemId" />

    <section
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
  </div>
</template>
