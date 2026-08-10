<script setup lang="ts">
import { ExternalLink, RefreshCw, ShieldCheck } from 'lucide-vue-next'
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { useAppApi } from '@/apis/app'
import { useLicenseApi } from '@/apis/license'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { localizeErrorMessage } from '@/lib/errorHandler'
import { useLicenseStore } from '@/stores/license'
import type { LicensePairing } from '@/types/license'

const { t } = useI18n()
const router = useRouter()
const appApi = useAppApi()
const api = useLicenseApi()
const licenseStore = useLicenseStore()
const pairing = ref<LicensePairing>()
const loading = ref(false)
const failure = ref('')
let timer: number | undefined
let active = false

const stopPolling = () => {
  if (timer !== undefined) window.clearTimeout(timer)
  timer = undefined
}

const enterAppIfReady = async () => {
  try {
    if (!(await appApi.ready())) return false
    const authorized = await licenseStore.check(true)
    if (!active || !authorized) return false
    stopPolling()
    await router.replace({ name: 'home' })
    return true
  } catch {
    return false
  }
}

const waitForApp = async () => {
  timer = undefined
  if (!active) return
  if (await enterAppIfReady()) return
  if (active) timer = window.setTimeout(waitForApp, 1000)
}

const poll = async () => {
  timer = undefined
  if (!active || !pairing.value || pairing.value.status !== 'pending') return
  try {
    const next = await api.pairing(pairing.value.id)
    if (!active) return
    pairing.value = next
    if (pairing.value.status === 'active') {
      void waitForApp()
      return
    }
    if (pairing.value.status === 'expired') return
  } catch (error) {
    // Activation restarts the process. If the polling response was lost, the
    // status resource on the new process is the source of truth.
    if (!active) return
    if (await enterAppIfReady()) return
    const code =
      error && typeof error === 'object' && 'error_code' in error ? String(error.error_code) : ''
    if (code === 'license_pairing_expired' || code === 'license_pairing_not_found') {
      stopPolling()
      pairing.value = { ...pairing.value, status: 'expired' }
      return
    }
  }
  if (active) timer = window.setTimeout(poll, 2000)
}

const createPairing = async () => {
  stopPolling()
  loading.value = true
  failure.value = ''
  try {
    const next = await api.createPairing()
    if (!active) return
    pairing.value = next
    timer = window.setTimeout(poll, 2000)
  } catch (error) {
    if (!active) return
    failure.value = localizeErrorMessage(error, t('activate.failed'))
  } finally {
    if (active) loading.value = false
  }
}

onMounted(() => {
  active = true
  void createPairing()
})
onUnmounted(() => {
  active = false
  stopPolling()
})
</script>

<template>
  <div class="min-h-dvh bg-background">
    <main class="mx-auto flex min-h-dvh w-full max-w-lg flex-col justify-center px-6 py-12">
      <header class="text-center">
        <div
          class="mx-auto mb-4 flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary"
        >
          <ShieldCheck class="size-6" />
        </div>
        <h1 class="text-xl font-semibold">{{ t('activate.title') }}</h1>
        <p class="mt-2 text-sm text-muted-foreground">{{ t('activate.description') }}</p>
      </header>

      <div class="mt-8 space-y-5">
        <div class="rounded-lg border bg-muted/40 p-4 text-sm">
          <p class="text-muted-foreground">{{ t('activate.device') }}</p>
          <p class="mt-1 break-all font-mono">{{ licenseStore.deviceId }}</p>
        </div>

        <div
          v-if="loading"
          class="flex justify-center py-8"
        >
          <Spinner class="size-6" />
        </div>
        <template v-else-if="pairing">
          <div
            v-if="pairing.status === 'pending'"
            class="space-y-3 text-center"
          >
            <p class="text-sm text-muted-foreground">{{ t('activate.openTelegram') }}</p>
            <Button
              as-child
              class="w-full"
            >
              <a
                :href="pairing.activationUrl"
                target="_blank"
                rel="noreferrer"
              >
                <ExternalLink class="size-4" />
                {{ t('activate.authorize') }}
              </a>
            </Button>
            <p class="text-xs text-muted-foreground">{{ t('activate.waiting') }}</p>
          </div>
          <div
            v-else-if="pairing.status === 'active'"
            class="text-center text-sm text-primary"
          >
            {{ t('activate.success') }}
          </div>
          <div
            v-else
            class="space-y-3 text-center"
          >
            <p class="text-sm text-destructive">{{ t('activate.expired') }}</p>
            <Button
              variant="outline"
              @click="createPairing"
              ><RefreshCw class="size-4" />{{ t('activate.retry') }}</Button
            >
          </div>
        </template>
        <div
          v-if="failure"
          class="space-y-3 text-center"
        >
          <p class="text-sm text-destructive">{{ failure }}</p>
          <Button
            variant="outline"
            @click="createPairing"
            ><RefreshCw class="size-4" />{{ t('activate.retry') }}</Button
          >
        </div>
      </div>
    </main>
  </div>
</template>
