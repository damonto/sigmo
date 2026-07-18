<script setup lang="ts">
import { Bell, BellOff } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import type { WebPushSupportReason } from '@/lib/webPush'

const props = withDefaults(
  defineProps<{
    disabled?: boolean
    enabled: boolean
    errorMessage: string
    hasCurrentSubscription: boolean
    isLoading: boolean
    isUpdating: boolean
    permissionMessage: string
    supportMessage: string
    supportReason: WebPushSupportReason
  }>(),
  {
    disabled: false,
  },
)

const emit = defineEmits<{
  'disable-current': []
  'enable-current': []
  toggle: [enabled: boolean]
}>()

const { t } = useI18n()
const controlsDisabled = computed(() => props.disabled || props.isUpdating || props.isLoading)
const enableCurrentDisabled = computed(
  () => controlsDisabled.value || !props.enabled || props.supportReason !== 'supported',
)
</script>

<template>
  <Card data-testid="web-push-toggle-card" class="gap-0 border-0 py-0 shadow-sm">
    <CardContent class="space-y-4 p-4">
      <div class="flex items-start gap-3">
        <Bell class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <div class="min-w-0 flex-1 space-y-1">
          <div class="text-sm font-medium text-foreground">
            {{ t('settings.webPush.title') }}
          </div>
          <p class="text-xs leading-5 text-muted-foreground">
            {{ t('settings.webPush.description') }}
          </p>
        </div>
        <Switch
          :model-value="props.enabled"
          :disabled="controlsDisabled"
          :aria-label="t('settings.webPush.toggle')"
          @update:model-value="emit('toggle', $event === true)"
        />
      </div>

      <div class="space-y-3 pl-7">
        <p v-if="props.supportMessage" class="text-xs leading-5 text-muted-foreground">
          {{ props.supportMessage }}
        </p>
        <p v-else-if="props.permissionMessage" class="text-xs leading-5 text-muted-foreground">
          {{ props.permissionMessage }}
        </p>

        <Button
          v-if="!props.hasCurrentSubscription"
          type="button"
          size="sm"
          variant="outline"
          :disabled="enableCurrentDisabled"
          @click="emit('enable-current')"
        >
          <Bell class="size-4" />
          {{ t('settings.webPush.enableCurrent') }}
        </Button>
        <Button
          v-else
          type="button"
          size="sm"
          variant="outline"
          :disabled="controlsDisabled"
          @click="emit('disable-current')"
        >
          <BellOff class="size-4" />
          {{ t('settings.webPush.disableCurrent') }}
        </Button>

        <p v-if="props.errorMessage" class="text-xs text-destructive">
          {{ props.errorMessage }}
        </p>
      </div>
    </CardContent>
  </Card>
</template>
