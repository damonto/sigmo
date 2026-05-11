<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import { RouterLink } from 'vue-router'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

const props = defineProps<{
  label: string
  to?: RouteLocationRaw
  class?: HTMLAttributes['class']
}>()

const emit = defineEmits<{
  (event: 'click', value: MouseEvent): void
}>()
</script>

<template>
  <Button
    v-if="props.to !== undefined"
    as-child
    variant="ghost"
    size="sm"
    :class="cn('px-3 text-muted-foreground', props.class)"
  >
    <RouterLink :to="props.to ?? '/'"> &larr; {{ props.label }} </RouterLink>
  </Button>
  <Button
    v-else
    variant="ghost"
    size="sm"
    type="button"
    :class="cn('px-3 text-muted-foreground', props.class)"
    @click="emit('click', $event)"
  >
    &larr; {{ props.label }}
  </Button>
</template>
