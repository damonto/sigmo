<script setup lang="ts">
import {
  Globe2,
  Laptop,
  LoaderCircle,
  Monitor,
  Pencil,
  Smartphone,
  Tablet,
  Trash2,
} from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { WebPushSubscriptionResponse } from '@/types/webPush'

const props = withDefaults(
  defineProps<{
    currentSubscriptionId?: string
    disabled?: boolean
    isLoading?: boolean
    isUpdating?: boolean
    subscriptions: WebPushSubscriptionResponse[]
  }>(),
  {
    currentSubscriptionId: undefined,
    disabled: false,
    isLoading: false,
    isUpdating: false,
  },
)

const emit = defineEmits<{
  delete: [subscription: WebPushSubscriptionResponse]
  rename: [subscription: WebPushSubscriptionResponse]
}>()

const { t } = useI18n()
const actionsDisabled = computed(() => props.disabled || props.isUpdating)

const isCurrent = (subscription: WebPushSubscriptionResponse) =>
  subscription.id === props.currentSubscriptionId

const platformLabel = (subscription: WebPushSubscriptionResponse) =>
  subscription.platform.trim() || t('settings.webPush.unknownDevice')

const platformIcon = (subscription: WebPushSubscriptionResponse) => {
  const device = `${subscription.platform} ${subscription.userAgent}`.toLowerCase()

  if (/ipad|tablet/.test(device)) return Tablet
  if (/android/.test(device)) return /mobile/.test(device) ? Smartphone : Tablet
  if (/iphone|ipod|ios|mobile/.test(device)) return Smartphone
  if (/mac|cros|chrome os/.test(device)) return Laptop
  if (/windows|win32|win64|linux|x11/.test(device)) return Monitor
  return Globe2
}
</script>

<template>
  <Card data-testid="web-push-devices-card" class="gap-0 border-0 py-0 shadow-sm">
    <CardHeader class="border-b px-4 py-4">
      <CardTitle class="text-sm">{{ t('settings.webPush.devicesTitle') }}</CardTitle>
      <CardDescription class="text-xs leading-5">
        {{ t('settings.webPush.devicesDescription') }}
      </CardDescription>
    </CardHeader>
    <CardContent class="p-4">
      <div v-if="props.isLoading" class="flex min-h-20 items-center justify-center">
        <LoaderCircle class="size-4 animate-spin text-muted-foreground" />
      </div>

      <div v-else-if="props.subscriptions.length > 0" class="space-y-3">
        <div
          v-for="subscription in props.subscriptions"
          :key="subscription.id"
          class="flex min-w-0 items-center gap-3 rounded-xl border bg-card/60 p-3 shadow-xs transition-colors hover:bg-muted/20 sm:p-4"
        >
          <div
            class="flex size-11 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary"
          >
            <component
              :is="platformIcon(subscription)"
              class="size-5"
              aria-hidden="true"
              data-testid="web-push-platform-icon"
            />
          </div>

          <div class="min-w-0 flex-1 space-y-1.5">
            <p class="truncate text-sm font-semibold text-foreground" :title="subscription.label">
              {{ subscription.label }}
            </p>
            <p
              class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground"
              data-testid="web-push-device-metadata"
            >
              <span>{{ platformLabel(subscription) }}</span>
              <template v-if="isCurrent(subscription)">
                <span aria-hidden="true">·</span>
                <span>{{ t('settings.webPush.currentDevice') }}</span>
              </template>
            </p>
          </div>

          <div class="flex shrink-0 items-center gap-1">
            <Button
              type="button"
              size="icon-sm"
              variant="outline"
              class="rounded-lg"
              :aria-label="t('settings.webPush.rename')"
              :title="t('settings.webPush.rename')"
              :disabled="actionsDisabled"
              @click="emit('rename', subscription)"
            >
              <Pencil class="size-4" />
            </Button>
            <Button
              type="button"
              size="icon-sm"
              variant="outline"
              class="rounded-lg text-destructive hover:text-destructive"
              :aria-label="t('settings.webPush.deleteDevice')"
              :title="t('settings.webPush.deleteDevice')"
              :disabled="actionsDisabled"
              @click="emit('delete', subscription)"
            >
              <Trash2 class="size-4" />
            </Button>
          </div>
        </div>
      </div>

      <p
        v-else
        class="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground"
      >
        {{ t('settings.webPush.noDevices') }}
      </p>
    </CardContent>
  </Card>
</template>
