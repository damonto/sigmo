<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import { Button } from '@/components/ui/button'
import type { NavigationItem } from '@/types/navigation'

const props = defineProps<{ items: NavigationItem[]; label?: string; variant?: 'underline' }>()
const route = useRoute()
const { t } = useI18n()
const underlined = computed(() => props.variant === 'underline')
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
    class="flex items-center"
    :class="underlined ? 'gap-x-4' : 'gap-1'"
    :aria-label="props.label ?? t('communication.navigation')"
  >
    <Button
      v-for="item in tabs"
      :key="item.key"
      as-child
      variant="ghost"
      class="text-muted-foreground hover:text-foreground aria-[current=page]:font-semibold aria-[current=page]:text-primary aria-[current=page]:hover:text-primary"
      :class="
        underlined
          ? 'h-10 gap-1.5 rounded-none border-b-2 border-transparent px-1 hover:bg-transparent aria-[current=page]:border-primary dark:hover:bg-transparent'
          : 'h-9 gap-2 rounded-lg px-3 aria-[current=page]:bg-primary/10 aria-[current=page]:hover:bg-primary/10 dark:aria-[current=page]:bg-primary/15'
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
