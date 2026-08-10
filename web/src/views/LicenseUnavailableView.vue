<script setup lang="ts">
import { RefreshCw, ShieldAlert } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Spinner } from '@/components/ui/spinner'
import { localizeErrorMessage } from '@/lib/errorHandler'
import { useLicenseStore } from '@/stores/license'

const { t } = useI18n()
const router = useRouter()
const licenseStore = useLicenseStore()
const retrying = ref(false)
const detail = computed(() =>
  localizeErrorMessage(
    {
      error_code: licenseStore.errorCode,
      message: licenseStore.errorMessage,
    },
    t('licenseUnavailable.description'),
  ),
)

const retry = async () => {
  retrying.value = true
  try {
    await licenseStore.check(true)
    if (!licenseStore.unavailable) await router.replace({ name: 'home' })
  } finally {
    retrying.value = false
  }
}
</script>

<template>
  <div class="min-h-dvh bg-background">
    <div class="mx-auto flex min-h-dvh w-full max-w-xl items-center px-6 py-12">
      <Card class="w-full">
        <CardHeader class="text-center">
          <div
            class="mx-auto mb-2 flex size-12 items-center justify-center rounded-full bg-destructive/10 text-destructive"
          >
            <ShieldAlert class="size-6" />
          </div>
          <CardTitle>{{ t('licenseUnavailable.title') }}</CardTitle>
          <CardDescription>{{ t('licenseUnavailable.description') }}</CardDescription>
        </CardHeader>
        <CardContent class="space-y-4 text-center">
          <p class="text-sm text-muted-foreground">{{ detail }}</p>
          <Button
            variant="outline"
            :disabled="retrying"
            @click="retry"
          >
            <Spinner
              v-if="retrying"
              class="size-4"
            />
            <RefreshCw
              v-else
              class="size-4"
            />
            {{ t('licenseUnavailable.retry') }}
          </Button>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
