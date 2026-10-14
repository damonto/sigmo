<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <header class="flex items-start justify-between gap-3">
    <div class="min-w-0 space-y-1">
      <h1 class="text-2xl font-semibold text-foreground">{{ title }}</h1>
      <p
        v-if="subtitle"
        class="text-sm text-muted-foreground"
      >
        {{ subtitle }}
      </p>
    </div>
    <div
      v-if="$slots.actions"
      class="flex shrink-0 items-center gap-2"
    >
      <slot name="actions" />
    </div>
  </header>
</template>
