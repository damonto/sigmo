<script setup lang="ts">
import type { Component } from 'vue'
import { RadioTower } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink, type RouteLocationRaw } from 'vue-router'

import { Button } from '@/components/ui/button'

const props = defineProps<{
  to: RouteLocationRaw
  label: string
  icon: Component
  embedded?: boolean
}>()

const { t } = useI18n()
</script>

<template>
  <aside
    class="hidden min-h-0 flex-col overflow-hidden border-sidebar-border bg-sidebar p-3 text-sidebar-foreground lg:flex"
    :class="
      props.embedded
        ? 'rounded-s-2xl border-e'
        : 'rounded-xl border lg:h-(--desktop-workspace-height)'
    "
  >
    <div class="flex shrink-0 items-center justify-between gap-2 px-2 pt-2 pb-4">
      <div class="flex min-w-0 items-center gap-2.5">
        <span
          class="grid size-8 shrink-0 place-items-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground"
        >
          <RadioTower
            class="size-4"
            aria-hidden="true"
          />
        </span>
        <span class="truncate text-xl font-bold tracking-tight">{{ t('home.title') }}</span>
      </div>
      <Button
        v-if="!props.embedded"
        as-child
        variant="ghost"
        size="icon-sm"
        class="text-muted-foreground hover:text-foreground"
      >
        <RouterLink
          :to="to"
          :aria-label="label"
          :title="label"
        >
          <component
            :is="icon"
            aria-hidden="true"
          />
        </RouterLink>
      </Button>
    </div>
    <slot />
    <div
      v-if="props.embedded"
      class="mt-auto shrink-0 border-t border-sidebar-border pt-2"
    >
      <Button
        as-child
        variant="ghost"
        size="sm"
        class="w-full justify-start text-muted-foreground"
      >
        <RouterLink
          :to="to"
          :aria-label="label"
        >
          <component
            :is="icon"
            aria-hidden="true"
          />
          <span class="truncate">{{ label }}</span>
        </RouterLink>
      </Button>
    </div>
  </aside>
</template>
