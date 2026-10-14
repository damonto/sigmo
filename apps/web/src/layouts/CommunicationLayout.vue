<script setup lang="ts">
import { computed } from 'vue'
import { ContactRound, MessageSquare, Phone, Server, Settings } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink, RouterView, useRoute } from 'vue-router'

import BottomTabBar from '@/components/BottomTabBar.vue'
import DesktopTabNav from '@/components/DesktopTabNav.vue'
import type { NavigationItem } from '@/types/navigation'

const { t } = useI18n()
const route = useRoute()
const isSettings = computed(() => route.matched.some((record) => record.path === '/settings'))
const tabs = computed<NavigationItem[]>(() => [
  {
    key: 'devices',
    routeName: 'home',
    to: { name: 'home' },
    label: t('communication.devices'),
    icon: Server,
  },
  {
    key: 'messages',
    routeName: 'messages',
    activeRouteNames: ['message-thread'],
    to: { name: 'messages' },
    label: t('communication.messages'),
    icon: MessageSquare,
  },
  {
    key: 'calls',
    routeName: 'calls',
    to: { name: 'calls' },
    label: t('communication.calls'),
    icon: Phone,
  },
  {
    key: 'contacts',
    routeName: 'contacts',
    activeRouteNames: ['contact'],
    to: { name: 'contacts' },
    label: t('communication.contacts'),
    icon: ContactRound,
  },
  {
    key: 'settings',
    routeName: 'settings',
    activeRouteNames: [
      'settings-auth',
      'settings-proxy',
      'settings-web-push',
      'settings-notifications',
      'settings-mcp',
      'settings-updates',
    ],
    to: { name: 'settings' },
    label: t('home.settings'),
    icon: Settings,
  },
])
</script>

<template>
  <div class="min-h-dvh bg-background">
    <header
      class="sticky top-0 z-20 hidden border-b border-border bg-background/85 backdrop-blur-xl backdrop-saturate-125 lg:block"
    >
      <div class="mx-auto flex h-16 w-full max-w-7xl items-center gap-5 px-8">
        <RouterLink
          :to="{ name: 'home' }"
          class="text-lg font-semibold tracking-tight"
          >{{ t('home.title') }}</RouterLink
        >
        <DesktopTabNav :items="tabs" />
      </div>
    </header>
    <div
      class="mx-auto flex w-full max-w-4xl flex-col gap-4 px-4 py-4 lg:max-w-7xl lg:px-8 lg:py-6 lg:pb-8"
      :class="{ 'pb-[calc(5rem+env(safe-area-inset-bottom))]': !isSettings }"
    >
      <RouterView />
    </div>
  </div>
  <div
    v-if="!isSettings"
    class="lg:hidden"
  >
    <BottomTabBar
      :items="tabs"
      container-class="max-w-4xl"
    />
  </div>
</template>
