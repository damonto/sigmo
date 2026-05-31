<script setup lang="ts">
import type { Component } from 'vue'
import { ChevronRight } from 'lucide-vue-next'
import { RouterLink, type RouteLocationRaw } from 'vue-router'

import { Card, CardContent } from '@/components/ui/card'

const props = defineProps<{
  title: string
  description: string
  icon: Component
  to: RouteLocationRaw
}>()
</script>

<template>
  <RouterLink :to="props.to" class="block rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring">
    <Card
      class="group gap-0 border-0 py-0 shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-md"
    >
      <CardContent class="flex items-center gap-3 p-3.5">
        <div
          class="flex size-10 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary transition-colors group-hover:bg-primary group-hover:text-primary-foreground"
        >
          <component :is="props.icon" class="size-4" />
        </div>
        <div class="min-w-0 flex-1">
          <h2 class="truncate text-sm font-medium text-foreground">
            {{ props.title }}
          </h2>
          <p class="line-clamp-2 text-xs leading-4 text-muted-foreground">
            {{ props.description }}
          </p>
        </div>
        <ChevronRight class="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
      </CardContent>
    </Card>
  </RouterLink>
</template>
