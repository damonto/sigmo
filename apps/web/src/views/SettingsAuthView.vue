<script setup lang="ts">
import { computed } from 'vue'
import { FlaskConical } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import SettingsAuthSection from '@/components/settings/SettingsAuthSection.vue'
import SettingsHeader from '@/components/settings/SettingsHeader.vue'
import SettingsSaveButton from '@/components/settings/SettingsSaveButton.vue'
import { Button } from '@/components/ui/button'
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
  isTestingAuth,
  saveAuth,
  setAuthValue,
  testAuth,
} = useSettingsContext()

const isAuthBusy = computed(() => isSaving.value || isTestingAuth.value)
const isTestDisabled = computed(
  () => isAuthBusy.value || (authValues.value?.authProviders.length ?? 0) === 0,
)
</script>

<template>
  <div class="space-y-4 pb-36 lg:pb-0">
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
        :disabled="isAuthBusy"
        @update-field="setAuthValue"
      />

      <div
        class="fixed inset-x-0 bottom-0 z-30 border-t bg-background/95 p-4 shadow-[0_-12px_30px_rgba(15,23,42,0.08)] backdrop-blur lg:static lg:z-auto lg:border-0 lg:bg-transparent lg:p-0 lg:shadow-none lg:backdrop-blur-none"
      >
        <div class="mx-auto grid w-full max-w-4xl gap-2 lg:max-w-none">
          <Button type="button" variant="outline" :disabled="isTestDisabled" @click="testAuth">
            <span class="inline-flex items-center gap-2">
              <Spinner v-if="isTestingAuth" class="size-4" />
              <FlaskConical v-else class="size-4" />
              {{ t('settings.test') }}
            </span>
          </Button>
          <SettingsSaveButton
            class="w-full"
            :disabled="isAuthBusy"
            :saving="isSavingAuth"
            @save="saveAuth"
          />
        </div>
      </div>
    </template>
  </div>
</template>
