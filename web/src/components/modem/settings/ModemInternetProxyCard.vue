<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { Check, Copy } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { InternetConnectionResponse } from '@/types/internet'

const props = defineProps<{
  connection: InternetConnectionResponse | null
}>()

const { t } = useI18n()

const isProxyActive = computed(() => props.connection?.proxy?.enabled === true)
const proxyStatusAriaLabel = computed(() =>
  isProxyActive.value
    ? t('modemDetail.settings.internetProxyEnabled')
    : t('modemDetail.settings.internetProxyDisabled'),
)
const proxyUsernameLabel = computed(() => {
  return props.connection?.proxy?.username || t('modemDetail.settings.internetNone')
})
const proxyPasswordLabel = computed(() => {
  return props.connection?.proxy?.password || t('modemDetail.settings.internetNone')
})
const proxyHTTPURL = computed(() => {
  return proxyURL('http', props.connection?.proxy?.httpAddress)
})
const proxySOCKS5URL = computed(() => {
  return proxyURL('socks5h', props.connection?.proxy?.socks5Address)
})
const proxyHTTPAddressLabel = computed(
  () => props.connection?.proxy?.httpAddress || t('modemDetail.settings.internetNone'),
)
const proxySOCKS5AddressLabel = computed(
  () => props.connection?.proxy?.socks5Address || t('modemDetail.settings.internetNone'),
)
const copiedProxyURL = ref<'http' | 'socks5' | ''>('')
const copyTimer = ref<number>()

const proxyURL = (scheme: string, address?: string) => {
  const username = props.connection?.proxy?.username
  const password = props.connection?.proxy?.password
  if (!isProxyActive.value || !address || !username || !password) {
    return null
  }
  const user = encodeURIComponent(username)
  const pass = encodeURIComponent(password)
  return `${scheme}://${user}:${pass}@${address}`
}

const markProxyURLCopied = (kind: 'http' | 'socks5') => {
  copiedProxyURL.value = kind
  if (copyTimer.value !== undefined) {
    window.clearTimeout(copyTimer.value)
  }
  copyTimer.value = window.setTimeout(() => {
    copiedProxyURL.value = ''
    copyTimer.value = undefined
  }, 1200)
}

const writeClipboardText = async (value: string) => {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value)
    return
  }

  const textarea = document.createElement('textarea')
  textarea.value = value
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.style.top = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    if (!document.execCommand('copy')) {
      throw new Error('copy command was rejected')
    }
  } finally {
    document.body.removeChild(textarea)
  }
}

const copyProxyURL = async (kind: 'http' | 'socks5', value: string | null) => {
  if (!value) return
  try {
    await writeClipboardText(value)
    markProxyURLCopied(kind)
  } catch (err) {
    console.error('[ModemInternetProxyCard] Failed to copy proxy URL:', err)
  }
}

onUnmounted(() => {
  if (copyTimer.value !== undefined) {
    window.clearTimeout(copyTimer.value)
  }
})
</script>

<template>
  <Card class="gap-4 rounded-2xl border-0 py-4 shadow-sm">
    <CardHeader class="flex grid-cols-none flex-row items-center justify-between gap-4 px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.internetProxyInfoTitle') }}
      </CardTitle>
      <span
        class="relative flex size-3 items-center justify-center"
        role="status"
        :aria-label="proxyStatusAriaLabel"
        :title="proxyStatusAriaLabel"
      >
        <span
          v-if="isProxyActive"
          class="absolute inline-flex size-full animate-ping rounded-full bg-emerald-500 opacity-70"
        />
        <span
          class="relative inline-flex size-2.5 rounded-full"
          :class="
            isProxyActive
              ? 'bg-emerald-500 shadow-[0_0_0_3px_rgba(16,185,129,0.16)]'
              : 'bg-muted-foreground/40'
          "
        />
        <span class="sr-only">{{ proxyStatusAriaLabel }}</span>
      </span>
    </CardHeader>
    <CardContent class="space-y-2 px-4 text-sm">
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{
          t('modemDetail.settings.internetProxyUsername')
        }}</span>
        <span class="break-all text-right font-medium text-foreground">{{
          proxyUsernameLabel
        }}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{
          t('modemDetail.settings.internetProxyPassword')
        }}</span>
        <span class="break-all text-right font-medium text-foreground">{{
          proxyPasswordLabel
        }}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.internetHTTPProxy') }}</span>
        <div class="flex min-w-0 items-center justify-end gap-1.5">
          <span class="break-all text-right font-medium text-foreground">{{
            proxyHTTPAddressLabel
          }}</span>
          <Button
            size="icon-sm"
            variant="ghost"
            type="button"
            class="size-5 rounded-sm"
            :disabled="!proxyHTTPURL"
            :title="t('modemDetail.settings.internetCopyProxyURL')"
            @click="copyProxyURL('http', proxyHTTPURL)"
          >
            <Check v-if="copiedProxyURL === 'http'" class="size-3.5" />
            <Copy v-else class="size-3.5" />
            <span class="sr-only">{{ t('modemDetail.settings.internetCopyProxyURL') }}</span>
          </Button>
        </div>
      </div>

      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{
          t('modemDetail.settings.internetSOCKS5Proxy')
        }}</span>
        <div class="flex min-w-0 items-center justify-end gap-1.5">
          <span class="break-all text-right font-medium text-foreground">{{
            proxySOCKS5AddressLabel
          }}</span>
          <Button
            size="icon-sm"
            variant="ghost"
            type="button"
            class="size-5 rounded-sm"
            :disabled="!proxySOCKS5URL"
            :title="t('modemDetail.settings.internetCopyProxyURL')"
            @click="copyProxyURL('socks5', proxySOCKS5URL)"
          >
            <Check v-if="copiedProxyURL === 'socks5'" class="size-3.5" />
            <Copy v-else class="size-3.5" />
            <span class="sr-only">{{ t('modemDetail.settings.internetCopyProxyURL') }}</span>
          </Button>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
