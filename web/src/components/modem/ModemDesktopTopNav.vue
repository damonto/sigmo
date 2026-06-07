<script setup lang="ts">
import { ChevronLeft } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'

import { Button } from '@/components/ui/button'
import type { NavigationItem } from '@/types/navigation'

const props = defineProps<{
  items: NavigationItem[]
  modemId: string
}>()

const route = useRoute()
const { t } = useI18n()

const navItems = computed(() =>
  props.items.map((item) => ({
    ...item,
    isActive:
      route.name === item.routeName || (item.activeRouteNames ?? []).includes(route.name as string),
  })),
)
</script>

<template>
  <header
    class="sticky top-0 z-20 hidden border-b border-border bg-background/85 backdrop-blur-xl backdrop-saturate-125 lg:block"
  >
    <div class="mx-auto flex h-16 w-full max-w-7xl items-center gap-5 px-8">
      <Button as-child variant="ghost" size="sm" class="gap-2 px-2">
        <RouterLink :to="{ name: 'home' }">
          <ChevronLeft class="size-4" />
          {{ t('modemDetail.back') }}
        </RouterLink>
      </Button>

      <div class="min-w-0 border-l border-border pl-5">
        <p class="text-[0.65rem] font-medium uppercase tracking-normal text-muted-foreground">
          {{ t('home.title') }}
        </p>
        <p class="truncate text-sm font-semibold text-foreground" :title="props.modemId">
          {{ props.modemId }}
        </p>
      </div>

      <nav class="ml-auto flex items-center gap-1" aria-label="Primary navigation">
        <Button
          v-for="item in navItems"
          :key="item.key"
          as-child
          variant="ghost"
          class="h-9 gap-2 rounded-lg px-3"
          :class="
            item.isActive
              ? 'bg-primary/10 text-primary hover:bg-primary/10 dark:bg-primary/15'
              : 'text-muted-foreground hover:text-foreground'
          "
        >
          <RouterLink :to="item.to" :aria-current="item.isActive ? 'page' : undefined">
            <component :is="item.icon" class="size-4" />
            <span>{{ item.label }}</span>
          </RouterLink>
        </Button>
      </nav>
    </div>
  </header>
</template>
