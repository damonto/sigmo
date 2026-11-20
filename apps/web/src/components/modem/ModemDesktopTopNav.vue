<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import DesktopTabNav from '@/components/DesktopTabNav.vue'
import ModemDetailHeader from '@/components/modem/ModemDetailHeader.vue'
import { useModemResource } from '@/composables/useModemResource'
import type { Modem } from '@/types/modem'
import type { NavigationItem } from '@/types/navigation'

const props = defineProps<{
  items: NavigationItem[]
  modem: Modem | null
  modemId: string
}>()

const { t } = useI18n()
const { modem, isLoading } = useModemResource(computed(() => props.modemId))
const currentModem = computed(() => modem.value ?? props.modem)
</script>

<template>
  <div class="mb-4 shrink-0 space-y-3 border-b pb-2">
    <ModemDetailHeader
      :modem="currentModem"
      :is-loading="isLoading && !currentModem"
      variant="desktop"
    />
    <DesktopTabNav
      :items="props.items"
      :label="t('modemDetail.navigation')"
      class="flex-wrap"
    />
  </div>
</template>
