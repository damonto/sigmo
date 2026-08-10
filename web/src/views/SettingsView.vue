<script setup lang="ts">
import { Bell, Bot, Download, Globe2, Keyboard, MessageSquare } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import SettingsCategoryCard from '@/components/settings/SettingsCategoryCard.vue'
import SettingsHeader from '@/components/settings/SettingsHeader.vue'

const { t } = useI18n()

const cards = computed(() => [
  {
    key: 'auth',
    title: t('settings.authTitle'),
    description: t('settings.authDescription'),
    icon: Keyboard,
    to: { name: 'settings-auth' },
  },
  {
    key: 'proxy',
    title: t('settings.proxyTitle'),
    description: t('settings.proxyDescription'),
    icon: Globe2,
    to: { name: 'settings-proxy' },
  },
  {
    key: 'web-push',
    title: t('settings.webPush.title'),
    description: t('settings.webPush.description'),
    icon: Bell,
    to: { name: 'settings-web-push' },
  },
  {
    key: 'notifications',
    title: t('settings.notificationTitle'),
    description: t('settings.notificationDescription'),
    icon: MessageSquare,
    to: { name: 'settings-notifications' },
  },
  {
    key: 'mcp',
    title: t('settings.mcp.title'),
    description: t('settings.mcp.description'),
    icon: Bot,
    to: { name: 'settings-mcp' },
  },
  {
    key: 'updates',
    title: t('settings.updates.title'),
    description: t('settings.updates.description'),
    icon: Download,
    to: { name: 'settings-updates' },
  },
])
</script>

<template>
  <div class="space-y-4">
    <SettingsHeader
      :title="t('settings.title')"
      :description="t('settings.description')"
    />

    <div class="grid gap-3 xl:grid-cols-2">
      <SettingsCategoryCard
        v-for="card in cards"
        :key="card.key"
        :title="card.title"
        :description="card.description"
        :icon="card.icon"
        :to="card.to"
      />
    </div>
  </div>
</template>
