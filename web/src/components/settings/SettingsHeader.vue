<script setup lang="ts">
const props = defineProps<{
  title: string
  description: string
}>()
</script>

<template>
  <header class="min-w-0 space-y-1">
    <h1 class="text-2xl font-semibold text-foreground">
      {{ props.title }}
    </h1>
    <p class="text-sm text-muted-foreground">
      {{ props.description }}
    </p>
  </header>
</template>
