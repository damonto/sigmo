<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import SettingsHeader from '@/components/settings/SettingsHeader.vue'
import SettingsRootSection from '@/components/settings/SettingsRootSection.vue'
import SettingsSaveButton from '@/components/settings/SettingsSaveButton.vue'
import { Spinner } from '@/components/ui/spinner'
import { useSettingsContext } from '@/composables/useSettingsContext'

const { t } = useI18n()
const { isReady, isSaving, isSavingProxy, proxyFields, proxyValues, saveProxy, setRootValue } =
  useSettingsContext()
</script>

<template>
  <div class="space-y-4 pb-24 lg:pb-0">
    <SettingsHeader
      :title="t('settings.proxyTitle')"
      :description="t('settings.proxyDescription')"
    />

    <div v-if="!isReady" class="flex items-center justify-center py-24">
      <Spinner class="size-6 text-muted-foreground" />
      <span class="sr-only">{{ t('settings.loading') }}</span>
    </div>

    <template v-else>
      <SettingsRootSection
        section="proxy"
        :fields="proxyFields"
        :values="proxyValues"
        :disabled="isSaving"
        @update-field="(key, value) => setRootValue('proxy', key, value)"
      />

      <SettingsSaveButton
        class="hidden w-full lg:inline-flex"
        :disabled="isSaving"
        :saving="isSavingProxy"
        @save="saveProxy"
      />
    </template>

    <div
      class="fixed inset-x-0 bottom-0 z-30 border-t bg-background/95 p-4 shadow-[0_-12px_30px_rgba(15,23,42,0.08)] backdrop-blur lg:hidden"
    >
      <SettingsSaveButton
        class="mx-auto w-full max-w-4xl"
        :disabled="!isReady || isSaving"
        :saving="isSavingProxy"
        @save="saveProxy"
      />
    </div>
  </div>
</template>
