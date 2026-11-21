<script setup lang="ts">
import { computed, onMounted, ref, useTemplateRef, watch } from 'vue'
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
const content = useTemplateRef<HTMLElement>('content')

watch(
  () => route.name,
  () => {
    if (content.value) content.value.scrollTop = 0
  },
  { flush: 'post' },
)

onMounted(() => {
  void load()
})
</script>

<template>
  <div
    class="grid min-w-0 gap-6 lg:h-(--desktop-workspace-height) lg:grid-cols-(--desktop-workspace-columns)"
  >
    <SettingsDesktopNav />

    <div class="min-w-0 lg:flex lg:min-h-0 lg:flex-col">
      <div
        class="mb-4 flex items-center justify-between gap-3 lg:shrink-0 lg:justify-end"
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

      <main
        ref="content"
        class="lg:-ms-1 lg:-me-3 lg:min-h-0 lg:flex-1 lg:overflow-y-auto lg:ps-1 lg:pe-3 lg:pb-1 lg:scrollbar-thin lg:scrollbar-thumb-muted-foreground/75 lg:scrollbar-gutter-stable"
      >
        <RouterView />
      </main>
    </div>
  </div>
</template>
