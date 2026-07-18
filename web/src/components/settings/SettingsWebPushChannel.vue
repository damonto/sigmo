<script setup lang="ts">
import { Bell, BellOff, Check, LoaderCircle, Save, Trash2 } from 'lucide-vue-next'
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { useWebPush } from '@/composables/useWebPush'
import type { WebPushSubscriptionResponse } from '@/types/webPush'

const props = defineProps<{
  disabled?: boolean
}>()

const { t } = useI18n()
const {
  subscriptions,
  currentSubscription,
  enabled,
  supportReason,
  permission,
  isLoading,
  isUpdating,
  errorMessage,
  load,
  setEnabled,
  enableCurrentDevice,
  deleteSubscription,
  renameSubscription,
} = useWebPush()

const labels = ref<Record<string, string>>({})

watch(
  subscriptions,
  (items) => {
    for (const item of items) {
      labels.value[item.id] ??= item.label
    }
  },
  { immediate: true },
)

onMounted(() => {
  void load().catch(() => undefined)
})

const supportMessage = () => {
  switch (supportReason.value) {
    case 'insecure':
      return t('settings.webPush.insecure')
    case 'unsupported':
      return t('settings.webPush.unsupported')
    case 'ios_setup_required':
      return t('settings.webPush.iosSetupRequired')
    default:
      return ''
  }
}

const permissionMessage = () => {
  if (permission.value === 'denied') return t('settings.webPush.permissionDenied')
  if (permission.value === 'default') return t('settings.webPush.permissionDefault')
  return ''
}

const run = async (action: () => Promise<void>, successMessage?: string) => {
  try {
    await action()
    if (successMessage) toast.success(successMessage)
  } catch (err) {
    toast.error(err instanceof Error ? err.message : t('settings.webPush.actionFailed'))
  }
}

const handleToggle = (value: boolean) => {
  void run(() => setEnabled(value), t('settings.webPush.updated'))
}

const handleEnableCurrent = () => {
  void (async () => {
    try {
      if (await enableCurrentDevice()) {
        toast.success(t('settings.webPush.enabledCurrent'))
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('settings.webPush.actionFailed'))
    }
  })()
}

const handleDelete = (subscription: WebPushSubscriptionResponse) => {
  if (!window.confirm(t('settings.webPush.deleteConfirm', { label: subscription.label }))) return
  void run(() => deleteSubscription(subscription), t('settings.webPush.deleted'))
}

const handleRename = (subscription: WebPushSubscriptionResponse) => {
  const label = labels.value[subscription.id]?.trim() ?? ''
  if (!label || label === subscription.label) return
  void run(() => renameSubscription(subscription, label), t('settings.webPush.renamed'))
}

const isCurrent = (subscription: WebPushSubscriptionResponse) =>
  subscription.id === currentSubscription.value?.id
</script>

<template>
  <div class="space-y-4 py-4">
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
        :model-value="enabled"
        :disabled="props.disabled || isUpdating || isLoading"
        :aria-label="t('settings.webPush.toggle')"
        @update:model-value="handleToggle($event === true)"
      />
    </div>

    <div class="space-y-3 pl-7">
      <p v-if="supportMessage()" class="text-xs leading-5 text-muted-foreground">
        {{ supportMessage() }}
      </p>
      <p v-else-if="permissionMessage()" class="text-xs leading-5 text-muted-foreground">
        {{ permissionMessage() }}
      </p>

      <Button
        v-if="!currentSubscription"
        type="button"
        size="sm"
        variant="outline"
        :disabled="
          props.disabled || isUpdating || isLoading || !enabled || supportReason !== 'supported'
        "
        @click="handleEnableCurrent"
      >
        <Bell class="size-4" />
        {{ t('settings.webPush.enableCurrent') }}
      </Button>
      <Button
        v-else
        type="button"
        size="sm"
        variant="outline"
        :disabled="props.disabled || isUpdating || isLoading"
        @click="handleDelete(currentSubscription)"
      >
        <BellOff class="size-4" />
        {{ t('settings.webPush.disableCurrent') }}
      </Button>

      <div v-if="subscriptions.length > 0" class="divide-y border-y">
        <div
          v-for="subscription in subscriptions"
          :key="subscription.id"
          class="flex min-w-0 items-start gap-2 py-3"
        >
          <div class="min-w-0 flex-1">
            <div class="flex min-w-0 items-center gap-2">
              <Input
                v-model="labels[subscription.id]"
                :aria-label="t('settings.webPush.deviceLabel')"
                :disabled="props.disabled || isUpdating"
                class="h-8 min-w-0"
                @keyup.enter="handleRename(subscription)"
              />
              <Check v-if="isCurrent(subscription)" class="size-4 shrink-0 text-primary" />
            </div>
            <p class="mt-1 truncate text-xs text-muted-foreground">
              {{
                subscription.platform ||
                subscription.userAgent ||
                t('settings.webPush.unknownDevice')
              }}
              <span v-if="isCurrent(subscription)">
                · {{ t('settings.webPush.currentDevice') }}</span
              >
            </p>
          </div>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            :aria-label="t('settings.webPush.rename')"
            :title="t('settings.webPush.rename')"
            :disabled="props.disabled || isUpdating"
            @click="handleRename(subscription)"
          >
            <Save class="size-4" />
          </Button>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            :aria-label="t('settings.webPush.deleteDevice')"
            :title="t('settings.webPush.deleteDevice')"
            :disabled="props.disabled || isUpdating"
            @click="handleDelete(subscription)"
          >
            <Trash2 class="size-4 text-muted-foreground" />
          </Button>
        </div>
      </div>

      <p v-else-if="!isLoading" class="text-xs text-muted-foreground">
        {{ t('settings.webPush.noDevices') }}
      </p>
      <p v-if="errorMessage" class="text-xs text-destructive">{{ errorMessage }}</p>
      <LoaderCircle v-if="isLoading" class="size-4 animate-spin text-muted-foreground" />
    </div>
  </div>
</template>
