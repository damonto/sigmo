<script setup lang="ts">
import { Button } from '@/components/ui/button'

/** Shared secondary actions for message, call and contact details. */
defineProps<{ asChild?: boolean; destructive?: boolean }>()
</script>

<template>
  <Button
    variant="outline"
    size="default"
    :as-child="asChild"
    :class="destructive ? 'text-destructive hover:text-destructive' : ''"
  >
    <slot />
  </Button>
</template>
