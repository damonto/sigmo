<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import { Button } from '@/components/ui/button'

const props = defineProps<{
  subtitle: string
  version?: string
}>()

const { t } = useI18n()
</script>

<template>
  <header class="flex flex-col gap-3">
    <div class="flex items-start justify-between gap-4">
      <div class="flex flex-col gap-2">
        <div class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <h1 class="text-3xl font-semibold tracking-tight text-foreground md:text-4xl">
            {{ t('home.title') }}
          </h1>
          <span
            v-if="props.version"
            class="text-sm font-medium text-muted-foreground"
          >
            {{ props.version }}
          </span>
        </div>
        <p class="text-sm text-muted-foreground md:text-base">
          {{ props.subtitle }}
        </p>
      </div>
      <div class="shrink-0 lg:hidden">
        <Button
          as-child
          variant="outline"
        >
          <RouterLink :to="{ name: 'settings' }">
            {{ t('home.settings') }}
          </RouterLink>
        </Button>
      </div>
    </div>
  </header>
</template>
