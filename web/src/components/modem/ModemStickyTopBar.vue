<script setup lang="ts">
import { RouterLink } from 'vue-router'

import { Button } from '@/components/ui/button'

type BackRoute = string | { name: string; params?: Record<string, string> }

const props = defineProps<{
  title: string
  backLabel: string
  backTo: BackRoute
  show: boolean
}>()
</script>

<template>
  <div
    class="fixed inset-x-0 top-0 z-30 border-b border-border bg-background/95 backdrop-blur transition duration-200"
    :class="props.show ? 'translate-y-0 opacity-100' : '-translate-y-2 opacity-0 pointer-events-none'"
  >
    <div class="mx-auto w-full max-w-4xl px-6">
      <div class="grid grid-cols-[1fr_auto_1fr] items-center py-2">
        <div class="inline-flex justify-self-start">
          <Button as-child variant="ghost" size="sm" class="px-0 text-muted-foreground">
            <RouterLink :to="props.backTo">
              &larr; {{ props.backLabel }}
            </RouterLink>
          </Button>
        </div>
        <p class="max-w-[60vw] justify-self-center truncate text-sm font-medium text-foreground">
          {{ props.title }}
        </p>
        <div class="justify-self-end">
          <slot name="right" />
        </div>
      </div>
    </div>
  </div>
</template>
