<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { ContactRound, Info, MessageSquare, Phone, Settings } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterView, useRoute, useRouter } from 'vue-router'

import BottomTabBar from '@/components/BottomTabBar.vue'
import ModemDesktopList from '@/components/modem/ModemDesktopList.vue'
import ModemDesktopTopNav from '@/components/modem/ModemDesktopTopNav.vue'
import { useModems } from '@/composables/useModems'
import type { NavigationItem } from '@/types/navigation'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const desktop = useMediaQuery('(min-width: 64rem)')
const { modems, fetchModems, isLoading } = useModems()
const mounted = ref(false)

const modemId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''))
const firstModemId = computed(() => modems.value[0]?.id)
const currentModem = computed(
  () => modems.value.find((modem) => modem.id === modemId.value) ?? null,
)
const switchRouteName = computed(() => {
  if (route.name === 'modem-message-thread') return 'modem-messages'
  if (route.name === 'modem-contact') return 'modem-contacts'
  return typeof route.name === 'string' && modemId.value ? route.name : 'modem-detail'
})

onMounted(() => {
  void fetchModems()
  mounted.value = true
})

watch(
  [mounted, desktop, firstModemId, isLoading, () => route.name],
  ([ready, isDesktop, id, loading, name]) => {
    if (ready && isDesktop && id && !loading && name === 'home') {
      void router.replace({ name: 'modem-detail', params: { id } })
    }
  },
  { flush: 'post' },
)

const tabItems = computed<NavigationItem[]>(() => [
  {
    key: 'detail',
    routeName: 'modem-detail',
    activeRouteNames: ['modem-notifications'],
    to: { name: 'modem-detail', params: { id: modemId.value } },
    label: t('modemDetail.tabs.detail'),
    icon: Info,
  },
  {
    key: 'messages',
    routeName: 'modem-messages',
    activeRouteNames: ['modem-message-thread'],
    to: { name: 'modem-messages', params: { id: modemId.value } },
    label: t('modemDetail.tabs.messages'),
    icon: MessageSquare,
  },
  {
    key: 'calls',
    routeName: 'modem-calls',
    to: { name: 'modem-calls', params: { id: modemId.value } },
    label: t('modemDetail.tabs.calls'),
    icon: Phone,
  },
  {
    key: 'contacts',
    routeName: 'modem-contacts',
    activeRouteNames: ['modem-contact'],
    to: { name: 'modem-contacts', params: { id: modemId.value } },
    label: t('communication.contacts'),
    icon: ContactRound,
  },
  {
    key: 'settings',
    routeName: 'modem-settings',
    activeRouteNames: [
      'modem-settings-network',
      'modem-settings-internet',
      'modem-settings-device',
      'modem-settings-wifi-calling',
      'modem-settings-volte',
    ],
    to: { name: 'modem-settings', params: { id: modemId.value } },
    label: t('modemDetail.tabs.settings'),
    icon: Settings,
  },
])
</script>

<template>
  <div
    class="lg:grid lg:h-(--desktop-workspace-height) lg:grid-cols-[minmax(17rem,1fr)_minmax(0,5fr)] lg:gap-6"
  >
    <ModemDesktopList
      v-if="desktop"
      :modems="modems"
      :modem-id="modemId"
      :is-loading="isLoading"
      :switch-route-name="switchRouteName"
    />
    <div class="flex min-w-0 flex-col lg:min-h-0">
      <ModemDesktopTopNav
        v-if="desktop && modemId"
        :items="tabItems"
        :modem="currentModem"
        :modem-id="modemId"
      />
      <div
        class="@container/modem flex min-w-0 flex-col gap-4 lg:-me-3 lg:min-h-0 lg:flex-1 lg:overflow-y-auto lg:pe-3 lg:scrollbar-thin lg:scrollbar-thumb-muted-foreground/75 lg:scrollbar-gutter-stable lg:[--modem-desktop-content-height:100%]"
      >
        <RouterView />
      </div>
    </div>
  </div>

  <div
    v-if="modemId"
    class="lg:hidden"
  >
    <BottomTabBar
      :items="tabItems"
      container-class="max-w-4xl"
    />
  </div>
</template>
