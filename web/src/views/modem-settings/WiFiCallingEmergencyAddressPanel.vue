<script setup lang="ts">
import { MapPin } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Spinner } from '@/components/ui/spinner'

const props = defineProps<{
  isStarting: boolean
}>()

const emit = defineEmits<{
  (event: 'start'): void
}>()

const { t } = useI18n()
</script>

<template>
  <Card class="gap-4 border-0 py-4 shadow-sm">
    <CardHeader class="flex grid-cols-none flex-row items-center justify-between gap-4 px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.wifiCallingE911Title') }}
      </CardTitle>
    </CardHeader>

    <CardContent class="space-y-4 px-4">
      <p class="text-sm leading-5 text-muted-foreground">
        {{ t('modemDetail.settings.wifiCallingE911Description') }}
      </p>

      <Button
        size="sm"
        type="button"
        variant="outline"
        class="w-full"
        :disabled="props.isStarting"
        @click="emit('start')"
      >
        <span v-if="props.isStarting" class="inline-flex items-center gap-2">
          <Spinner class="size-4" />
          {{ t('modemDetail.settings.wifiCallingE911Action') }}
        </span>
        <span v-else class="inline-flex items-center gap-2">
          <MapPin class="size-4" aria-hidden="true" />
          {{ t('modemDetail.settings.wifiCallingE911Action') }}
        </span>
      </Button>
    </CardContent>
  </Card>
</template>
