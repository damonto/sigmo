<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterView, useRoute } from 'vue-router'

import BackButton from '@/components/BackButton.vue'
import SettingsDesktopNav from '@/components/settings/SettingsDesktopNav.vue'
import { provideSettingsContext } from '@/composables/useSettingsContext'

const route = useRoute()
const { t } = useI18n()
const { load } = provideSettingsContext()
const backTo = computed(() => (route.name === 'settings' ? { name: 'home' } : { name: 'settings' }))

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="min-h-dvh bg-background">
    <div class="mx-auto w-full max-w-7xl px-4 py-4 sm:px-6 lg:px-8 lg:py-6">
      <div class="mb-4">
        <BackButton :to="backTo" :label="t('settings.back')" class="w-fit" />
      </div>

      <div class="grid gap-6 lg:grid-cols-[18rem_minmax(0,1fr)] lg:items-start">
        <SettingsDesktopNav />

        <main class="min-w-0">
          <RouterView />
        </main>
      </div>
    </div>
  </div>
</template>
