<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterView, useRoute, useRouter } from 'vue-router'

import BackButton from '@/components/BackButton.vue'
import SettingsDesktopNav from '@/components/settings/SettingsDesktopNav.vue'
import { provideSettingsContext } from '@/composables/useSettingsContext'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const signingOut = ref(false)
const logoutFailed = ref(false)
const logout = async () => {
  signingOut.value = true
  logoutFailed.value = false
  try {
    await auth.logout()
    if (auth.configuration?.mode !== 'cloudflare_access') await router.replace({ name: 'auth' })
  } catch {
    logoutFailed.value = true
  } finally {
    signingOut.value = false
  }
}
const { t } = useI18n()
const { load } = provideSettingsContext()
const backTo = computed(() => (route.name === 'settings' ? { name: 'home' } : { name: 'settings' }))

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="min-w-0">
    <div
      class="mb-4 flex items-center justify-between gap-3 lg:justify-end"
      :class="{ 'lg:hidden': !auth.configuration || auth.configuration.mode === 'none' }"
    >
      <BackButton
        :to="backTo"
        :label="t('settings.back')"
        class="w-fit lg:hidden"
      />
      <Button
        v-if="auth.configuration && auth.configuration.mode !== 'none'"
        variant="outline"
        :disabled="signingOut"
        @click="logout"
        >{{ t('auth.logout') }}</Button
      >
    </div>
    <p
      v-if="logoutFailed"
      role="alert"
      class="mb-4 text-sm text-destructive"
    >
      {{ t('errors.revoke_session_failed') }}
    </p>

    <div class="grid gap-6 lg:grid-cols-[18rem_minmax(0,1fr)] lg:items-start">
      <SettingsDesktopNav />

      <main class="min-w-0">
        <RouterView />
      </main>
    </div>
  </div>
</template>
