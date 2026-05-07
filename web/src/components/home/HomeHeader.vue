<script setup lang="ts">
import { RefreshCw, Settings } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import { Button } from '@/components/ui/button'

const props = defineProps<{
  subtitle: string
  isLoading: boolean
}>()

const emit = defineEmits<{
  (event: 'refresh'): void
}>()

const { t } = useI18n()
</script>

<template>
  <header class="flex flex-col gap-3">
    <div class="flex items-start justify-between gap-4">
      <div class="flex flex-col gap-2">
        <h1 class="text-3xl font-semibold tracking-tight text-foreground md:text-4xl">
          {{ t('home.title') }}
        </h1>
        <p class="text-sm text-muted-foreground md:text-base">
          {{ props.subtitle }}
        </p>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <Button
          as-child
          variant="outline"
          size="icon"
          :title="t('home.config')"
        >
          <RouterLink :to="{ name: 'config' }">
            <Settings class="size-5" />
          </RouterLink>
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon"
          :disabled="props.isLoading"
          :title="t('home.refresh')"
          @click="emit('refresh')"
        >
          <RefreshCw class="size-5" :class="{ 'animate-spin': props.isLoading }" />
        </Button>
      </div>
    </div>
  </header>
</template>
