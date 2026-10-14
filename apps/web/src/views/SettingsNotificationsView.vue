<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import SettingsChannelPicker from '@/components/settings/SettingsChannelPicker.vue'
import SettingsHeader from '@/components/settings/SettingsHeader.vue'
import SettingsNotificationSection from '@/components/settings/SettingsNotificationSection.vue'
import SettingsSaveButton from '@/components/settings/SettingsSaveButton.vue'
import { Spinner } from '@/components/ui/spinner'
import { useSettingsContext } from '@/composables/useSettingsContext'

const { t } = useI18n()
const {
  channels,
  channelSchemas,
  expandedChannels,
  isReady,
  isSaving,
  isSavingNotifications,
  savingNotificationChannels,
  saveChannel,
  setChannelValue,
  values,
  configuredChannelSchemas,
  saveNotifications,
  toggleChannelDetails,
} = useSettingsContext()
</script>

<template>
  <div class="space-y-4">
    <SettingsHeader
      :title="t('settings.notificationTitle')"
      :description="t('settings.notificationDescription')"
    />

    <div
      v-if="!isReady"
      class="flex items-center justify-center py-24"
    >
      <Spinner class="size-6 text-muted-foreground" />
      <span class="sr-only">{{ t('settings.loading') }}</span>
    </div>

    <section
      v-if="isReady && values"
      class="space-y-3 rounded-xl bg-card p-4"
    >
      <h2 class="text-sm font-semibold">{{ t('settings.notificationRouting') }}</h2>
      <p class="text-sm text-muted-foreground">
        {{ t('settings.notificationRoutingDescription') }}
      </p>
      <SettingsChannelPicker
        id="settings-notifications-channels"
        v-model="values.notifications.channels"
        :channels="configuredChannelSchemas"
        :disabled="isSaving"
      />
      <div class="flex justify-end border-t pt-4">
        <SettingsSaveButton
          id="settings-notifications-save"
          class="w-full sm:w-auto"
          :disabled="isSaving"
          :saving="isSavingNotifications"
          @save="saveNotifications"
        />
      </div>
    </section>
    <SettingsNotificationSection
      v-if="isReady"
      :channels="channels"
      :disabled="isSaving"
      :expanded-channels="expandedChannels"
      :saving-channels="savingNotificationChannels"
      :schemas="channelSchemas"
      @toggle-details="toggleChannelDetails"
      @update-field="setChannelValue"
      @save-channel="saveChannel"
    />
  </div>
</template>
