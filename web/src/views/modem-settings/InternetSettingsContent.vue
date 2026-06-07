<script setup lang="ts">
import { computed } from 'vue'
import { Plug, Unplug } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import InternetConnectionPanel from './InternetConnectionPanel.vue'
import InternetProxyPanel from './InternetProxyPanel.vue'
import InternetPublicPanel from './InternetPublicPanel.vue'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import type { InternetConnectionResponse, InternetPublicResponse } from '@/types/internet'

const apn = defineModel<string>('apn', { required: true })
const ipType = defineModel<string>('ipType', { required: true })
const apnUsername = defineModel<string>('apnUsername', { required: true })
const apnPassword = defineModel<string>('apnPassword', { required: true })
const apnAuth = defineModel<string>('apnAuth', { required: true })
const defaultRoute = defineModel<boolean>('defaultRoute', { required: true })
const proxyEnabled = defineModel<boolean>('proxyEnabled', { required: true })
const alwaysOn = defineModel<boolean>('alwaysOn', { required: true })

const props = defineProps<{
  connection: InternetConnectionResponse | null
  publicInfo: InternetPublicResponse | null
  isLoading: boolean
  isConnecting: boolean
  isDisconnecting: boolean
  isConnected: boolean
  canConnect: boolean
}>()

const emit = defineEmits<{
  (event: 'connect'): void
  (event: 'disconnect'): void
}>()

const { t } = useI18n()

const shouldShowProxyInfo = computed(
  () => props.isConnected && props.connection?.proxyEnabled === true,
)
const isActionLoading = computed(
  () => props.isLoading || props.isConnecting || props.isDisconnecting,
)
const isActionDisabled = computed(() => {
  if (props.isLoading) return true
  if (props.isConnected) return props.isDisconnecting
  return !props.canConnect || props.isConnecting || props.isDisconnecting
})
const actionLabel = computed(() => {
  if (props.isConnected) return t('modemDetail.settings.internetDisconnect')
  return t('modemDetail.settings.internetConnect')
})

const handleAction = () => {
  if (props.isConnected) {
    emit('disconnect')
    return
  }
  emit('connect')
}
</script>

<template>
  <div class="space-y-3 pb-16 lg:pb-0">
    <InternetConnectionPanel
      v-model:apn="apn"
      v-model:ip-type="ipType"
      v-model:apn-username="apnUsername"
      v-model:apn-password="apnPassword"
      v-model:apn-auth="apnAuth"
      v-model:default-route="defaultRoute"
      v-model:proxy-enabled="proxyEnabled"
      v-model:always-on="alwaysOn"
      :connection="props.connection"
      :is-loading="props.isLoading"
      :is-connecting="props.isConnecting"
      :is-disconnecting="props.isDisconnecting"
      :is-connected="props.isConnected"
      :can-connect="props.canConnect"
    />

    <InternetProxyPanel v-if="shouldShowProxyInfo" :connection="props.connection" />

    <InternetPublicPanel v-if="props.isConnected" :public-info="props.publicInfo" />
  </div>

  <div
    class="fixed inset-x-0 bottom-16 z-10 border-t border-white/40 bg-background/80 px-4 py-3 backdrop-blur-xl dark:border-white/10 lg:static lg:mt-3 lg:border-0 lg:bg-transparent lg:p-0 lg:backdrop-blur-none"
  >
    <div class="mx-auto max-w-4xl lg:max-w-none">
      <Button
        size="sm"
        type="button"
        class="w-full"
        :variant="props.isConnected ? 'outline' : 'default'"
        :disabled="isActionDisabled"
        @click="handleAction"
      >
        <Spinner v-if="isActionLoading" class="size-4" />
        <Unplug v-else-if="props.isConnected" class="size-4" />
        <Plug v-else class="size-4" />
        {{ actionLabel }}
      </Button>
    </div>
  </div>
</template>
