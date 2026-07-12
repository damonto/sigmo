<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'

const props = defineProps<{
  managed: boolean
  canEnable: boolean
  modemRegistered: boolean
  isLoading: boolean
  isUpdating: boolean
  canUpdate: boolean
}>()

const emit = defineEmits<{
  (event: 'update', managed: boolean): void
}>()

const { t } = useI18n()

const description = computed(() => {
  if (props.managed) return t('modemDetail.settings.networkVoLTEManagedDescription')
  if (props.modemRegistered) return t('modemDetail.settings.networkVoLTEModemRegisteredDescription')
  if (!props.canEnable) return t('modemDetail.settings.networkVoLTEUnavailable')
  return t('modemDetail.settings.networkVoLTEDescription')
})
</script>

<template>
  <Card class="gap-4 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.networkVoLTETitle') }}
      </CardTitle>
    </CardHeader>

    <CardContent class="space-y-3 px-4">
      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1 space-y-1">
          <Label for="network-volte">
            {{ t('modemDetail.settings.networkVoLTELabel') }}
          </Label>
          <p class="text-xs leading-5 text-muted-foreground">
            {{ description }}
          </p>
        </div>
        <div class="inline-flex shrink-0 items-center gap-2">
          <Spinner v-if="props.isUpdating" class="size-4 text-muted-foreground" />
          <Switch
            id="network-volte"
            :model-value="props.managed"
            :disabled="props.isLoading || !props.canUpdate"
            @update:model-value="emit('update', $event === true)"
          />
        </div>
      </div>
      <p class="border-t pt-3 text-xs leading-5 text-muted-foreground">
        {{ t('modemDetail.settings.networkVoLTEInternetIPTypeNotice') }}
      </p>
    </CardContent>
  </Card>
</template>
