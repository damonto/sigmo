<script setup lang="ts">
import { Bell, Bot, Globe2, Keyboard, MessageSquare } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import { Button } from '@/components/ui/button'

const route = useRoute()
const { t } = useI18n()

const items = computed(() => [
  {
    key: 'auth',
    title: t('settings.authTitle'),
    description: t('settings.authDescription'),
    icon: Keyboard,
    routeName: 'settings-auth',
    to: { name: 'settings-auth' },
  },
  {
    key: 'proxy',
    title: t('settings.proxyTitle'),
    description: t('settings.proxyDescription'),
    icon: Globe2,
    routeName: 'settings-proxy',
    to: { name: 'settings-proxy' },
  },
  {
    key: 'web-push',
    title: t('settings.webPush.title'),
    description: t('settings.webPush.description'),
    icon: Bell,
    routeName: 'settings-web-push',
    to: { name: 'settings-web-push' },
  },
  {
    key: 'notifications',
    title: t('settings.notificationTitle'),
    description: t('settings.notificationDescription'),
    icon: MessageSquare,
    routeName: 'settings-notifications',
    to: { name: 'settings-notifications' },
  },
  {
    key: 'mcp',
    title: t('settings.mcp.title'),
    description: t('settings.mcp.description'),
    icon: Bot,
    routeName: 'settings-mcp',
    to: { name: 'settings-mcp' },
  },
])
</script>

<template>
  <aside class="hidden rounded-xl border bg-card/60 p-3 shadow-sm lg:block">
    <div class="px-2 py-2">
      <p class="text-sm font-semibold text-foreground">
        {{ t('settings.title') }}
      </p>
      <p class="mt-1 text-xs text-muted-foreground">
        {{ t('settings.description') }}
      </p>
    </div>

    <nav class="mt-2 space-y-1" :aria-label="t('settings.title')">
      <Button
        v-for="item in items"
        :key="item.key"
        as-child
        variant="ghost"
        class="h-auto w-full justify-start rounded-lg px-3 py-3 text-left"
        :class="
          route.name === item.routeName
            ? 'bg-primary/10 text-primary hover:bg-primary/10 dark:bg-primary/15'
            : 'text-muted-foreground hover:text-foreground'
        "
      >
        <RouterLink
          :to="item.to"
          :aria-current="route.name === item.routeName ? 'page' : undefined"
        >
          <component :is="item.icon" class="mt-0.5 size-4 shrink-0" />
          <span class="min-w-0">
            <span class="block truncate text-sm font-medium">{{ item.title }}</span>
            <span
              :data-testid="`settings-nav-description-${item.key}`"
              class="mt-0.5 block whitespace-normal wrap-break-word text-xs leading-4 opacity-80"
            >
              {{ item.description }}
            </span>
          </span>
        </RouterLink>
      </Button>
    </nav>
  </aside>
</template>
