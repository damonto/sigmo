<script setup lang="ts">
import type { Component } from 'vue'
import { computed } from 'vue'
import { RouterLink, useRoute, type RouteLocationRaw } from 'vue-router'

import { Button } from '@/components/ui/button'

export type TabBarItem = {
  key: string
  to: RouteLocationRaw
  routeName: string
  activeRouteNames?: string[]
  label: string
  icon: Component
}

const props = withDefaults(
  defineProps<{
    items: TabBarItem[]
    containerClass?: string
  }>(),
  {
    containerClass: 'max-w-6xl',
  },
)

const route = useRoute()

const tabs = computed(() =>
  props.items.map((item) => ({
    ...item,
    isActive:
      route.name === item.routeName || (item.activeRouteNames ?? []).includes(route.name as string),
  })),
)
</script>

<template>
  <nav
    aria-label="Primary navigation"
    class="fixed bottom-0 left-0 right-0 z-20 h-16 w-full border-t border-white/40 bg-white/60 px-6 py-2 shadow-[0_-12px_30px_rgba(15,23,42,0.08)] backdrop-blur-2xl backdrop-saturate-125 dark:border-white/10 dark:bg-slate-950/50"
  >
    <div class="mx-auto flex h-full w-full items-center" :class="props.containerClass">
      <div v-for="item in tabs" :key="item.key" class="flex flex-1 justify-center">
        <Button
          as-child
          variant="ghost"
          size="icon"
          class="size-10 rounded-full"
          :class="
            item.isActive ? 'bg-muted text-foreground dark:bg-white/10' : 'text-muted-foreground'
          "
        >
          <RouterLink :to="item.to" :aria-current="item.isActive ? 'page' : undefined">
            <component :is="item.icon" class="size-5" />
            <span class="sr-only">{{ item.label }}</span>
          </RouterLink>
        </Button>
      </div>
    </div>
  </nav>
</template>
