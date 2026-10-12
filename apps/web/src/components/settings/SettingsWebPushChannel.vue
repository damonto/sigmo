<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'
import { Smartphone } from 'lucide-vue-next'

import SettingsWebPushControlCard from '@/components/settings/SettingsWebPushControlCard.vue'
import SettingsWebPushDeviceList from '@/components/settings/SettingsWebPushDeviceList.vue'
import SettingsWebPushRenameDialog from '@/components/settings/SettingsWebPushRenameDialog.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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

const renameDialogOpen = ref(false)
const renamingSubscription = ref<WebPushSubscriptionResponse | null>(null)
const renameLabel = ref('')
const hasCurrentSubscription = computed(() => currentSubscription.value !== null)
const currentSubscriptionID = computed(() => currentSubscription.value?.id)
const renameOriginalLabel = computed(() => renamingSubscription.value?.label ?? '')
const supportMessage = computed(() => {
  switch (supportReason.value) {
    case 'insecure':
      return t('settings.webPush.insecure')
    case 'unsupported':
      return t('settings.webPush.unsupported')
    default:
      return ''
  }
})
const permissionMessage = computed(() => {
  if (supportReason.value !== 'supported') return ''
  if (permission.value === 'denied') return t('settings.webPush.permissionDenied')
  if (permission.value === 'default') return t('settings.webPush.permissionDefault')
  return ''
})

onMounted(() => {
  void load().catch(() => undefined)
})

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

const handleDisableCurrent = () => {
  if (currentSubscription.value) handleDelete(currentSubscription.value)
}

const startRenaming = (subscription: WebPushSubscriptionResponse) => {
  renamingSubscription.value = subscription
  renameLabel.value = subscription.label
  renameDialogOpen.value = true
}

const resetRenameDialog = () => {
  renamingSubscription.value = null
  renameLabel.value = ''
}

const closeRenameDialog = () => {
  renameDialogOpen.value = false
  resetRenameDialog()
}

const handleRename = async (label: string) => {
  const subscription = renamingSubscription.value
  if (!subscription) return
  if (await run(() => renameSubscription(subscription, label), t('settings.webPush.renamed'))) {
    closeRenameDialog()
  }
}
</script>

<template>
  <div class="space-y-4">
    <SettingsWebPushControlCard
      :disabled="props.disabled"
      :enabled="enabled"
      :error-message="errorMessage"
      :has-current-subscription="hasCurrentSubscription"
      :is-loading="isLoading"
      :is-updating="isUpdating"
      :permission-message="permissionMessage"
      :support-message="supportMessage"
      :support-reason="supportReason"
      @disable-current="handleDisableCurrent"
      @enable-current="handleEnableCurrent"
      @toggle="handleToggle"
    />

    <Alert v-if="supportReason === 'ios_setup_required'" data-testid="web-push-ios-alert">
      <Smartphone aria-hidden="true" />
      <AlertTitle>{{ t('settings.webPush.iosSetupTitle') }}</AlertTitle>
      <AlertDescription class="leading-5">
        {{ t('settings.webPush.iosSetupRequired') }}
      </AlertDescription>
    </Alert>

    <SettingsWebPushDeviceList
      :current-subscription-id="currentSubscriptionID"
      :disabled="props.disabled"
      :is-loading="isLoading"
      :is-updating="isUpdating"
      :subscriptions="subscriptions"
      @delete="handleDelete"
      @rename="startRenaming"
    />

    <SettingsWebPushRenameDialog
      v-model:label="renameLabel"
      v-model:open="renameDialogOpen"
      :disabled="props.disabled"
      :original-label="renameOriginalLabel"
      :saving="isUpdating"
      @close="resetRenameDialog"
      @save="handleRename"
    />
  </div>
</template>
