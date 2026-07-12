<script setup lang="ts">
import { Globe2, Phone, PhoneCall, RadioTower, Smartphone } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import { Button } from '@/components/ui/button'
import { FEATURE, useCapabilities } from '@/composables/useCapabilities'

const route = useRoute()
const { t } = useI18n()
const { hasFeature } = useCapabilities()

const modemId = computed(() => (route.params.id ?? 'unknown') as string)
const canUseWiFiCalling = computed(() => hasFeature(FEATURE.wifiCalling))
const canUseVoLTE = computed(() => hasFeature(FEATURE.volte))

const items = computed(() => {
  const allItems = [
    {
      key: 'network',
      title: t('modemDetail.settings.networkTitle'),
      description: t('modemDetail.settings.networkCategoryDescription'),
      icon: RadioTower,
      routeName: 'modem-settings-network',
      to: { name: 'modem-settings-network', params: { id: modemId.value } },
    },
    {
      key: 'internet',
      title: t('modemDetail.settings.internetTitle'),
      description: t('modemDetail.settings.internetCategoryDescription'),
      icon: Globe2,
      routeName: 'modem-settings-internet',
      to: { name: 'modem-settings-internet', params: { id: modemId.value } },
    },
    {
      key: 'wifi-calling',
      title: t('modemDetail.settings.wifiCallingTitle'),
      description: t('modemDetail.settings.wifiCallingCategoryDescription'),
      icon: Phone,
      routeName: 'modem-settings-wifi-calling',
      to: { name: 'modem-settings-wifi-calling', params: { id: modemId.value } },
    },
    {
      key: 'volte',
      title: t('modemDetail.settings.volteTitle'),
      description: t('modemDetail.settings.volteCategoryDescription'),
      icon: PhoneCall,
      routeName: 'modem-settings-volte',
      to: { name: 'modem-settings-volte', params: { id: modemId.value } },
    },
    {
      key: 'device',
      title: t('modemDetail.settings.deviceTitle'),
      description: t('modemDetail.settings.deviceCategoryDescription'),
      icon: Smartphone,
      routeName: 'modem-settings-device',
      to: { name: 'modem-settings-device', params: { id: modemId.value } },
    },
  ]

  return allItems.filter((item) => {
    if (item.key === 'wifi-calling') return canUseWiFiCalling.value
    if (item.key === 'volte') return canUseVoLTE.value
    return true
  })
})
</script>

<template>
  <aside class="hidden rounded-xl border bg-card/60 p-3 shadow-sm lg:block">
    <div class="px-2 py-2">
      <p class="text-sm font-semibold text-foreground">
        {{ t('modemDetail.settings.title') }}
      </p>
      <p class="mt-1 text-xs text-muted-foreground">
        {{ t('modemDetail.settings.subtitle') }}
      </p>
    </div>

    <nav class="mt-2 space-y-1" :aria-label="t('modemDetail.settings.title')">
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
            <span class="mt-0.5 block line-clamp-2 text-xs opacity-80">{{ item.description }}</span>
          </span>
        </RouterLink>
      </Button>
    </nav>
  </aside>
</template>
