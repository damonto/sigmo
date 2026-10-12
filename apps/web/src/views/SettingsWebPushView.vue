<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import SettingsHeader from '@/components/settings/SettingsHeader.vue'
import SettingsWebPushChannel from '@/components/settings/SettingsWebPushChannel.vue'

const { t } = useI18n()
</script>

<template>
  <div class="space-y-4">
    <SettingsHeader
      :title="t('settings.webPush.title')"
      :description="t('settings.webPush.description')"
    />

    <SettingsWebPushChannel />
  </div>
</template>
