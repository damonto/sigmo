<script setup lang="ts">
import { computed } from 'vue'
import { Settings } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import CarrierLogo from '@/components/CarrierLogo.vue'
import DesktopWorkspaceSidebar from '@/components/DesktopWorkspaceSidebar.vue'
import ModemSignalStatus from '@/components/modem/ModemSignalStatus.vue'
import { Skeleton } from '@/components/ui/skeleton'
import type { Modem } from '@/types/modem'

const props = defineProps<{
  modems: Modem[]
  modemId: string
  isLoading: boolean
  switchRouteName: string
}>()

const { t } = useI18n()
const route = useRoute()
const devices = computed(() =>
  props.modems.map((modem) => {
    const name = modem.name.trim()
    const title = modem.sim.operatorName.trim() || name || modem.id
    const subtitle = name && name !== title ? name : modem.id
    return { modem, title, subtitle: subtitle === title ? '' : subtitle }
  }),
)
</script>

<template>
  <DesktopWorkspaceSidebar
    :to="{ name: 'settings', state: { settingsReturnTo: route.fullPath } }"
    :label="t('home.globalSettings')"
    :icon="Settings"
  >
    <div class="flex shrink-0 items-baseline justify-between gap-2 px-2 py-3">
      <h2
        id="desktop-device-list-title"
        class="text-sm font-semibold"
      >
        {{ t('home.deviceList') }}
      </h2>
      <span
        v-if="!isLoading"
        class="text-xs text-muted-foreground"
      >
        {{ t('home.subtitle', { count: modems.length }) }}
      </span>
    </div>
    <nav
      aria-labelledby="desktop-device-list-title"
      :aria-busy="isLoading"
      class="min-h-0 space-y-1 overflow-y-auto"
    >
      <RouterLink
        v-for="{ modem, title, subtitle } in devices"
        :key="modem.id"
        :to="
          modem.id === modemId
            ? route.fullPath
            : { name: switchRouteName, params: { id: modem.id } }
        "
        :aria-current="modem.id === modemId ? 'true' : undefined"
        :class="
          modem.id === modemId
            ? 'border-primary/20 bg-primary/10'
            : 'border-transparent hover:bg-muted'
        "
        class="flex min-w-0 items-center gap-2 rounded-lg border px-2 py-2.5 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
      >
        <span
          class="flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-background"
          aria-hidden="true"
        >
          <CarrierLogo
            :name="modem.sim.operatorName"
            :icon="modem.sim.icon"
            :plmn="modem.sim.operatorIdentifier"
            :region-code="modem.sim.regionCode"
          />
        </span>
        <span class="min-w-0 flex-1">
          <span
            class="block truncate text-sm font-medium"
            :title="title"
            >{{ title }}</span
          >
          <span
            v-if="subtitle"
            class="block truncate text-xs text-muted-foreground"
            :title="subtitle"
            >{{ subtitle }}</span
          >
        </span>
        <ModemSignalStatus
          variant="stacked"
          :signal-quality="modem.signalQuality"
          :registration-state="modem.registrationState"
          :access-technology="modem.accessTechnology"
          :wifi-calling-connected="modem.wifiCallingConnected"
          :volte-connected="modem.volteConnected"
          :airplane-mode="modem.airplaneMode"
          :show-signal-value="false"
        />
      </RouterLink>
      <template v-if="isLoading && !modems.length">
        <Skeleton
          v-for="index in 3"
          :key="index"
          class="h-14 w-full"
        />
      </template>
    </nav>
  </DesktopWorkspaceSidebar>
</template>
