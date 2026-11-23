<script setup lang="ts">
import { computed } from 'vue'
import { Settings } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import CarrierLogo from '@/components/CarrierLogo.vue'
import DesktopWorkspaceSidebar from '@/components/DesktopWorkspaceSidebar.vue'
import ModemSignalStatus from '@/components/modem/ModemSignalStatus.vue'
import { Skeleton } from '@/components/ui/skeleton'
import { displayModemName, isModemConnected } from '@/composables/useModemDisplay'
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
    const operator = modem.sim.operatorName.trim()
    const title = displayModemName(modem)
    const subtitle = operator && operator !== title ? operator : modem.id
    return {
      modem,
      title,
      subtitle: subtitle === title ? '' : subtitle,
      connected: isModemConnected(modem),
    }
  }),
)
</script>

<template>
  <DesktopWorkspaceSidebar
    :to="{ name: 'settings', state: { settingsReturnTo: route.fullPath } }"
    :label="t('home.globalSettings')"
    :icon="Settings"
    embedded
  >
    <div class="flex shrink-0 items-center justify-between gap-2 px-2 py-2">
      <h2
        id="desktop-device-list-title"
        class="text-sm font-semibold"
      >
        {{ t('home.deviceList') }}
      </h2>
      <span
        v-if="!isLoading"
        class="rounded-full border bg-card px-2 text-xs tabular-nums text-muted-foreground"
      >
        <span aria-hidden="true">{{ modems.length }}</span>
        <span class="sr-only">{{ t('home.subtitle', { count: modems.length }) }}</span>
      </span>
    </div>
    <nav
      aria-labelledby="desktop-device-list-title"
      :aria-busy="isLoading"
      class="min-h-0 flex-1 space-y-2 overflow-y-auto pb-3"
    >
      <RouterLink
        v-for="{ modem, title, subtitle, connected } in devices"
        :key="modem.id"
        :to="
          modem.id === modemId
            ? route.fullPath
            : { name: switchRouteName, params: { id: modem.id } }
        "
        :aria-current="modem.id === modemId ? 'true' : undefined"
        :class="
          modem.id === modemId
            ? 'border-primary/25 bg-sidebar-accent before:absolute before:inset-y-3 before:-start-px before:w-0.5 before:rounded-full before:bg-sidebar-primary'
            : 'border-sidebar-border bg-card hover:border-primary/30 hover:bg-sidebar-accent/50'
        "
        class="relative flex min-h-15 min-w-0 items-center gap-2 rounded-xl border px-2.5 py-2 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-sidebar-ring"
      >
        <span
          class="grid size-8 shrink-0 place-items-center overflow-hidden rounded-lg border border-sidebar-border bg-background"
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
            class="block truncate text-sm font-semibold"
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
        <div class="flex shrink-0 flex-col items-end gap-0.5">
          <ModemSignalStatus
            variant="stacked"
            :signal-quality="modem.signalQuality"
            :registration-state="modem.registrationState"
            :access-technology="connected ? modem.accessTechnology : null"
            :wifi-calling-connected="modem.wifiCallingConnected"
            :volte-connected="modem.volteConnected"
            :airplane-mode="modem.airplaneMode"
            :show-signal-value="false"
          />
          <span :class="connected ? 'sr-only' : 'text-xs text-muted-foreground'">
            {{
              t(
                connected
                  ? 'modemDetail.connection.connected'
                  : 'modemDetail.connection.disconnected',
              )
            }}
          </span>
        </div>
      </RouterLink>
      <div
        v-if="isLoading && !modems.length"
        class="space-y-1"
      >
        <Skeleton
          v-for="index in 3"
          :key="index"
          class="h-15 w-full bg-sidebar-accent"
        />
      </div>
    </nav>
  </DesktopWorkspaceSidebar>
</template>
