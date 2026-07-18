<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import SettingsAuthSection from '@/components/settings/SettingsAuthSection.vue'
import SettingsHeader from '@/components/settings/SettingsHeader.vue'
import SettingsSaveButton from '@/components/settings/SettingsSaveButton.vue'
import { Spinner } from '@/components/ui/spinner'
import { useSettingsContext } from '@/composables/useSettingsContext'

const { t } = useI18n()
const {
  authFields,
  authValues,
  enabledChannelSchemas,
  isReady,
  isSaving,
  isSavingAuth,
  saveAuth,
  setAuthValue,
} = useSettingsContext()
</script>

<template>
  <div class="space-y-4 pb-24 lg:pb-0">
    <SettingsHeader :title="t('settings.authTitle')" :description="t('settings.authDescription')" />

    <div v-if="!isReady" class="flex items-center justify-center py-24">
      <Spinner class="size-6 text-muted-foreground" />
      <span class="sr-only">{{ t('settings.loading') }}</span>
    </div>

    <template v-else>
      <SettingsAuthSection
        :auth="authValues"
        :enabled-channels="enabledChannelSchemas"
        :fields="authFields"
        :disabled="isSaving"
        @update-field="setAuthValue"
      />

      <SettingsSaveButton
        class="hidden w-full lg:inline-flex"
        :disabled="isSaving"
        :saving="isSavingAuth"
        @save="saveAuth"
      />
    </template>

    <div
      class="fixed inset-x-0 bottom-0 z-30 border-t bg-background/95 p-4 shadow-[0_-12px_30px_rgba(15,23,42,0.08)] backdrop-blur lg:hidden"
    >
      <SettingsSaveButton
        class="mx-auto w-full max-w-4xl"
        :disabled="!isReady || isSaving"
        :saving="isSavingAuth"
        @save="saveAuth"
      />
    </div>
  </div>
</template>
