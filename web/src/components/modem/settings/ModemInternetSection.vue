<script setup lang="ts">
import { computed } from 'vue'

import ModemInternetConnectionCard from './ModemInternetConnectionCard.vue'
import ModemInternetProxyCard from './ModemInternetProxyCard.vue'
import ModemInternetPublicCard from './ModemInternetPublicCard.vue'
import type { InternetConnectionResponse, InternetPublicResponse } from '@/types/internet'

const apn = defineModel<string>('apn', { required: true })
const defaultRoute = defineModel<boolean>('defaultRoute', { required: true })
const proxyEnabled = defineModel<boolean>('proxyEnabled', { required: true })

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

const shouldShowProxyInfo = computed(
  () => props.isConnected && props.connection?.proxyEnabled === true,
)
</script>

<template>
  <ModemInternetConnectionCard
    v-model:apn="apn"
    v-model:default-route="defaultRoute"
    v-model:proxy-enabled="proxyEnabled"
    :connection="props.connection"
    :is-loading="props.isLoading"
    :is-connecting="props.isConnecting"
    :is-disconnecting="props.isDisconnecting"
    :is-connected="props.isConnected"
    :can-connect="props.canConnect"
    @connect="emit('connect')"
    @disconnect="emit('disconnect')"
  />

  <ModemInternetProxyCard v-if="shouldShowProxyInfo" :connection="props.connection" />

  <ModemInternetPublicCard v-if="props.isConnected" :public-info="props.publicInfo" />
</template>
