<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import SettingsHeader from '@/components/settings/SettingsHeader.vue'
import SettingsNotificationSection from '@/components/settings/SettingsNotificationSection.vue'
import { Spinner } from '@/components/ui/spinner'
import { useSettingsContext } from '@/composables/useSettingsContext'

const { t } = useI18n()
const {
  channels,
  channelSchemas,
  expandedChannels,
  isReady,
  isSaving,
  savingNotificationChannels,
  saveChannel,
  setChannelValue,
  toggleChannel,
  toggleChannelDetails,
} = useSettingsContext()
</script>

<template>
  <div class="space-y-4">
    <SettingsHeader
      :title="t('settings.notificationTitle')"
      :description="t('settings.notificationDescription')"
    />

    <div v-if="!isReady" class="flex items-center justify-center py-24">
      <Spinner class="size-6 text-muted-foreground" />
      <span class="sr-only">{{ t('settings.loading') }}</span>
    </div>

    <SettingsNotificationSection
      v-else
      :channels="channels"
      :disabled="isSaving"
      :expanded-channels="expandedChannels"
      :saving-channels="savingNotificationChannels"
      :schemas="channelSchemas"
      @toggle-channel="toggleChannel"
      @toggle-details="toggleChannelDetails"
      @update-field="setChannelValue"
      @save-channel="saveChannel"
    />
  </div>
</template>
