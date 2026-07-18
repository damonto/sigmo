<script setup lang="ts">
import {
  Bell,
  BellOff,
  Check,
  Globe2,
  Laptop,
  LoaderCircle,
  Monitor,
  Pencil,
  Smartphone,
  Tablet,
  Trash2,
  X,
} from 'lucide-vue-next'
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
const editingID = ref<string | null>(null)

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
    return true
  } catch (err) {
    toast.error(err instanceof Error ? err.message : t('settings.webPush.actionFailed'))
    return false
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

const startRenaming = (subscription: WebPushSubscriptionResponse) => {
  labels.value[subscription.id] = subscription.label
  editingID.value = subscription.id
}

const cancelRenaming = (subscription: WebPushSubscriptionResponse) => {
  labels.value[subscription.id] = subscription.label
  editingID.value = null
}

const handleRename = async (subscription: WebPushSubscriptionResponse) => {
  const label = labels.value[subscription.id]?.trim() ?? ''
  if (!label) return
  if (label === subscription.label) {
    editingID.value = null
    return
  }
  if (await run(() => renameSubscription(subscription, label), t('settings.webPush.renamed'))) {
    editingID.value = null
  }
}

const isCurrent = (subscription: WebPushSubscriptionResponse) =>
  subscription.id === currentSubscription.value?.id

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

      <div v-if="subscriptions.length > 0" class="space-y-3">
        <div
          v-for="subscription in subscriptions"
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
            <div v-if="editingID === subscription.id" class="flex min-w-0 items-center gap-2">
              <Input
                v-model="labels[subscription.id]"
                :aria-label="t('settings.webPush.deviceLabel')"
                :disabled="props.disabled || isUpdating"
                class="h-8 min-w-0 max-w-sm"
                @keyup.enter="handleRename(subscription)"
                @keyup.esc="cancelRenaming(subscription)"
              />
            </div>
            <div v-else class="min-w-0">
              <p class="truncate text-sm font-semibold text-foreground" :title="subscription.label">
                {{ subscription.label }}
              </p>
            </div>
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
            <template v-if="editingID === subscription.id">
              <Button
                type="button"
                size="icon-sm"
                variant="outline"
                class="rounded-lg text-primary"
                :aria-label="t('settings.webPush.saveRename')"
                :title="t('settings.webPush.saveRename')"
                :disabled="props.disabled || isUpdating"
                @click="handleRename(subscription)"
              >
                <Check class="size-4" />
              </Button>
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                class="rounded-lg"
                :aria-label="t('settings.webPush.cancelRename')"
                :title="t('settings.webPush.cancelRename')"
                :disabled="props.disabled || isUpdating"
                @click="cancelRenaming(subscription)"
              >
                <X class="size-4" />
              </Button>
            </template>
            <Button
              v-else
              type="button"
              size="icon-sm"
              variant="outline"
              class="rounded-lg"
              :aria-label="t('settings.webPush.rename')"
              :title="t('settings.webPush.rename')"
              :disabled="props.disabled || isUpdating"
              @click="startRenaming(subscription)"
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
              :disabled="props.disabled || isUpdating"
              @click="handleDelete(subscription)"
            >
              <Trash2 class="size-4" />
            </Button>
          </div>
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
