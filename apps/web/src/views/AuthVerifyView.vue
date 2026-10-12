<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { Button } from '@/components/ui/button'
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@/components/ui/input-otp'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const { t } = useI18n()
const authStore = useAuthStore()

const CODE_LENGTH = 6
const RESEND_TICK_MS = 1000

const pin = ref('')
const now = ref(Date.now())
let tickTimer: ReturnType<typeof setInterval> | null = null

const tickNow = () => {
  now.value = Date.now()
}

const startTicker = () => {
  if (tickTimer) return
  tickTimer = setInterval(tickNow, RESEND_TICK_MS)
}

const stopTicker = () => {
  if (!tickTimer) return
  clearInterval(tickTimer)
  tickTimer = null
}

const ensureTicker = () => {
  tickNow()
  if (authStore.resendAvailableAt > now.value) {
    startTicker()
  } else {
    stopTicker()
  }
}

const resendRemaining = computed(() => {
  const remainingMs = authStore.resendAvailableAt - now.value
  return Math.max(0, Math.ceil(remainingMs / 1000))
})

const canResend = computed(
  () => authStore.otpRequired && resendRemaining.value === 0 && !authStore.isSending,
)
const resendLabel = computed(() =>
  resendRemaining.value > 0
    ? t('auth.resendCountdown', { seconds: resendRemaining.value })
    : t('auth.resend'),
)

const handleResend = async () => {
  if (!canResend.value) return
  await authStore.sendCode()
  ensureTicker()
}

onMounted(async () => {
  if (!authStore.otpRequired) {
    await router.replace({ name: 'home' })
    return
  }
  await authStore.sendCode()
  ensureTicker()
})

onUnmounted(() => {
  stopTicker()
})

watch(
  () => authStore.resendAvailableAt,
  () => {
    ensureTicker()
  },
)

watch(resendRemaining, (value) => {
  if (value === 0) {
    stopTicker()
  }
})

watch(
  () => pin.value,
  async (value) => {
    if (value.length !== CODE_LENGTH) return

    const token = await authStore.verifyCode(value)
    if (!token) return

    await router.replace({ name: 'home' })
  },
)
</script>

<template>
  <div class="min-h-dvh bg-background">
    <div class="mx-auto flex min-h-dvh w-full max-w-5xl items-center justify-center px-6 py-12">
      <div class="w-full max-w-lg space-y-10">
        <header class="space-y-4 text-center">
          <p class="text-xs uppercase tracking-[0.45em] text-muted-foreground">
            {{ t('auth.kicker') }}
          </p>
          <h1 class="text-3xl font-semibold tracking-tight text-foreground md:text-4xl">
            {{ t('auth.title') }}
          </h1>
          <p class="text-sm text-muted-foreground md:text-base">
            {{ t('auth.subtitle') }}
          </p>
        </header>

        <div class="space-y-6">
          <p class="text-center text-sm text-muted-foreground">
            {{ t('auth.prompt') }}
          </p>
          <div class="flex justify-center">
            <InputOTP v-model="pin" :maxlength="CODE_LENGTH" :disabled="authStore.isVerifying">
              <InputOTPGroup>
                <InputOTPSlot v-for="index in CODE_LENGTH" :key="index" :index="index - 1" />
              </InputOTPGroup>
            </InputOTP>
          </div>
          <div class="flex items-center justify-center gap-3 text-sm text-muted-foreground">
            <span>{{ t('auth.resendHint') }}</span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              :disabled="!canResend"
              @click="handleResend"
            >
              {{ resendLabel }}
            </Button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
