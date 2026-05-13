<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Spinner } from '@/components/ui/spinner'

const props = defineProps<{
  operatorLabel: string
  registrationState: string
  accessTechnology: string
  isScanning: boolean
}>()

const emit = defineEmits<{
  (event: 'scan'): void
}>()

const { t } = useI18n()

const isScanDisabled = computed(() => props.isScanning)
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.networkTitle') }}
      </CardTitle>
      <CardAction>
        <Button size="sm" type="button" :disabled="isScanDisabled" @click="emit('scan')">
          <span v-if="props.isScanning" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.settings.networkSearch') }}
          </span>
          <span v-else>{{ t('modemDetail.settings.networkSearch') }}</span>
        </Button>
      </CardAction>
    </CardHeader>

    <CardContent class="space-y-2 px-4 text-sm">
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.networkOperator') }}</span>
        <span class="font-medium text-foreground">
          {{ props.operatorLabel }}
        </span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.networkStatus') }}</span>
        <span class="font-medium text-foreground">
          {{ props.registrationState }}
        </span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.networkAccess') }}</span>
        <span class="font-medium text-foreground">
          {{ props.accessTechnology }}
        </span>
      </div>
    </CardContent>
  </Card>
</template>
