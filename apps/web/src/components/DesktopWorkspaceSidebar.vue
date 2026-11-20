<script setup lang="ts">
import type { Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, type RouteLocationRaw } from 'vue-router'

import { Button } from '@/components/ui/button'

defineProps<{
  to: RouteLocationRaw
  label: string
  icon: Component
}>()

const { t } = useI18n()
</script>

<template>
  <aside
    class="hidden min-h-0 flex-col overflow-hidden rounded-xl border bg-card/40 p-3 lg:flex lg:h-(--desktop-workspace-height)"
  >
    <div class="flex shrink-0 items-center justify-between gap-2 border-b border-border px-2 pb-2">
      <span class="text-lg font-semibold tracking-tight">{{ t('home.title') }}</span>
      <Button
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
  </aside>
</template>
