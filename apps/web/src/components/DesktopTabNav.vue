<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import { Button } from '@/components/ui/button'
import type { NavigationItem } from '@/types/navigation'

const props = defineProps<{ items: NavigationItem[] }>()
const route = useRoute()
const { t } = useI18n()
const tabs = computed(() =>
  props.items.map((item) => ({
    ...item,
    active:
      route.name === item.routeName || item.activeRouteNames?.some((name) => name === route.name),
  })),
)
</script>

<template>
  <nav
    class="ml-auto flex items-center gap-1"
    :aria-label="t('communication.navigation')"
  >
    <Button
      v-for="item in tabs"
      :key="item.key"
      as-child
      variant="ghost"
      class="h-9 gap-2 rounded-lg px-3"
      :class="
        item.active
          ? 'bg-primary/10 text-primary hover:bg-primary/10 dark:bg-primary/15'
          : 'text-muted-foreground hover:text-foreground'
      "
    >
      <RouterLink
        :to="item.to"
        :aria-current="item.active ? 'page' : undefined"
      >
        <component
          :is="item.icon"
          class="size-4"
          aria-hidden="true"
        />
        <span>{{ item.label }}</span>
      </RouterLink>
    </Button>
  </nav>
</template>
