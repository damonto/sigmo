<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { useWiFiCallingProxies } from '@/composables/useWiFiCallingProxies'
import { DEFAULT_WIFI_PROXY_PORT, useWiFiProxyPaste } from '@/composables/useWiFiProxyPaste'
import type { WiFiProxy, WiFiProxyCheckStep, WiFiProxySettings } from '@/types/modem'

const props = defineProps<{
  modemId: string
  savedProxyId?: string
  usingProxy: boolean
  disabled: boolean
}>()
const emit = defineEmits<{ 'update-proxy': [proxyId: string | null] }>()
const { t } = useI18n()
const { proxies, selected, result, loading, saving, checking, failure, load, save, check } =
  useWiFiCallingProxies(props.modemId, toRef(props, 'savedProxyId'))

const selectedProxy = computed(() => proxies.value.find((proxy) => proxy.id === selected.value))
const prefix = 'modemDetail.settings.wifiProxy.'
const toggleDisabled = computed(
  () =>
    props.disabled ||
    (!props.usingProxy && (!selectedProxy.value || loading.value || saving.value)),
)
const toggleProxy = (enabled: boolean) => {
  if (toggleDisabled.value) return
  if (!enabled) {
    emit('update-proxy', null)
  } else if (selectedProxy.value) {
    emit('update-proxy', selectedProxy.value.id)
  }
}
type Editor = {
  id: string
  settings: Omit<WiFiProxySettings, 'port'> & { port: number | string }
  auth: boolean
  hasPassword: boolean
}

const editor = ref<Editor>()
const { invalidPaste, clearPasteError, readProxyPaste } = useWiFiProxyPaste()
const edit = (proxy?: WiFiProxy) => {
  clearPasteError()
  editor.value = {
    id: proxy?.id ?? '',
    settings: {
      name: proxy?.name ?? '',
      host: proxy?.host ?? '',
      port: proxy?.port ?? '',
      username: proxy?.username ?? '',
      password: '',
    },
    auth: Boolean(proxy?.username),
    hasPassword: proxy?.hasPassword ?? false,
  }
}

const pasteHost = (event: ClipboardEvent) => {
  const entry = editor.value
  if (!entry || entry.id || saving.value) return

  const address = readProxyPaste(event)
  if (!address) return

  entry.settings = {
    name: entry.settings.name.trim() ? entry.settings.name : address.host.slice(0, 128),
    ...address,
  }
  entry.auth = Boolean(address.username)
}

const saveEditor = async () => {
  const entry = editor.value
  if (!entry || saving.value) return
  const settings = {
    ...entry.settings,
    port: entry.settings.port === '' ? DEFAULT_WIFI_PROXY_PORT : Number(entry.settings.port),
  }
  if (!entry.auth) {
    settings.username = ''
    settings.password = ''
  }
  if (await save(entry.id, settings)) editor.value = undefined
}

const stepError = (step: WiFiProxyCheckStep) =>
  step.errorCode ? t(prefix + 'errors.' + step.errorCode) : '—'
const udpAddress = (step: WiFiProxyCheckStep) => {
  if (!step.ip) return stepError(step)
  if (!step.port) return step.ip
  return step.ip.includes(':') ? `[${step.ip}]:${step.port}` : `${step.ip}:${step.port}`
}
</script>

<template>
  <Card class="gap-4 border-0 py-4 shadow-sm">
    <CardHeader class="flex flex-row items-center justify-between gap-3 px-4">
      <CardTitle class="text-base">{{ t(prefix + 'title') }}</CardTitle>
      <Button
        type="button"
        size="sm"
        variant="outline"
        :disabled="loading || Boolean(editor)"
        @click="edit()"
      >
        {{ t(prefix + 'add') }}
      </Button>
    </CardHeader>
    <CardContent class="space-y-4 px-4">
      <div class="flex items-center justify-between gap-3">
        <Label for="wifi-proxy-enabled">{{ t(prefix + 'useProxy') }}</Label>
        <Switch
          id="wifi-proxy-enabled"
          :model-value="usingProxy"
          :disabled="toggleDisabled"
          @update:model-value="toggleProxy"
        />
      </div>
      <div
        v-if="proxies.length || savedProxyId"
        class="space-y-2"
      >
        <Label for="wifi-proxy-selection">{{ t(prefix + 'selection') }}</Label>
        <Select
          v-model="selected"
          :disabled="loading || saving"
        >
          <SelectTrigger
            id="wifi-proxy-selection"
            class="w-full"
          >
            <SelectValue :placeholder="t(prefix + 'selection')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="proxy in proxies"
              :key="proxy.id"
              :value="proxy.id"
              >{{ proxy.name }}</SelectItem
            >
          </SelectContent>
        </Select>
        <div class="flex flex-wrap gap-2">
          <Button
            type="button"
            size="sm"
            variant="outline"
            :disabled="!selectedProxy || checking || saving"
            @click="check"
          >
            {{ t(prefix + (checking ? 'checking' : 'check')) }}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            :disabled="!selectedProxy || Boolean(editor)"
            @click="edit(selectedProxy)"
          >
            {{ t(prefix + 'edit') }}
          </Button>
          <Button
            v-if="usingProxy && selectedProxy && selected !== savedProxyId"
            type="button"
            size="sm"
            :disabled="disabled || saving"
            @click="emit('update-proxy', selected)"
          >
            {{ t(prefix + 'switchProxy') }}
          </Button>
        </div>
      </div>

      <form
        v-if="editor"
        class="space-y-3"
        @submit.prevent="saveEditor"
      >
        <div class="space-y-1">
          <Label for="wifi-proxy-name">{{ t(prefix + 'name') }}</Label>
          <Input
            id="wifi-proxy-name"
            v-model="editor.settings.name"
            :placeholder="t(prefix + 'namePlaceholder')"
            required
          />
        </div>
        <div class="grid grid-cols-[minmax(0,1fr)_6rem] gap-2">
          <div class="space-y-1">
            <Label for="wifi-proxy-host">{{ t(prefix + 'host') }}</Label>
            <Input
              id="wifi-proxy-host"
              v-model="editor.settings.host"
              :placeholder="t(prefix + 'hostPlaceholder')"
              :aria-describedby="invalidPaste ? 'wifi-proxy-paste-error' : undefined"
              :disabled="saving"
              required
              @input="clearPasteError"
              @paste="pasteHost"
            />
          </div>
          <div class="space-y-1">
            <Label for="wifi-proxy-port">{{ t(prefix + 'port') }}</Label>
            <Input
              id="wifi-proxy-port"
              v-model="editor.settings.port"
              :placeholder="t(prefix + 'portPlaceholder')"
              type="number"
              min="1"
              max="65535"
            />
          </div>
        </div>
        <p
          v-if="invalidPaste"
          id="wifi-proxy-paste-error"
          role="alert"
          class="text-sm text-destructive"
        >
          {{ t(prefix + 'invalidPaste') }}
        </p>
        <div class="flex items-center gap-3">
          <Switch
            id="wifi-proxy-auth"
            v-model="editor.auth"
          />
          <Label for="wifi-proxy-auth">{{ t(prefix + 'auth') }}</Label>
        </div>
        <template v-if="editor.auth">
          <div class="space-y-1">
            <Label for="wifi-proxy-username">{{ t(prefix + 'username') }}</Label>
            <Input
              id="wifi-proxy-username"
              v-model="editor.settings.username"
              :placeholder="t(prefix + 'usernamePlaceholder')"
              required
            />
          </div>
          <div class="space-y-1">
            <Label for="wifi-proxy-password">{{ t(prefix + 'password') }}</Label>
            <Input
              id="wifi-proxy-password"
              v-model="editor.settings.password"
              type="password"
              autocomplete="new-password"
              :required="!editor.hasPassword"
              :placeholder="
                t(prefix + (editor.hasPassword ? 'passwordSaved' : 'passwordPlaceholder'))
              "
            />
          </div>
        </template>
        <div class="flex gap-2">
          <Button
            type="submit"
            size="sm"
            :disabled="saving"
            >{{ t(prefix + 'save') }}</Button
          >
          <Button
            type="button"
            size="sm"
            variant="outline"
            :disabled="saving"
            @click="editor = undefined"
            >{{ t(prefix + 'cancel') }}</Button
          >
        </div>
      </form>

      <p
        v-if="failure"
        role="alert"
        class="text-sm text-destructive"
      >
        {{ t(prefix + failure) }}
      </p>
      <Button
        v-if="failure === 'listFailed'"
        type="button"
        variant="outline"
        size="sm"
        @click="load"
        >{{ t(prefix + 'retry') }}</Button
      >

      <div
        v-if="result"
        aria-live="polite"
        class="space-y-2 text-sm"
      >
        <dl class="space-y-2 select-text">
          <div>
            <dt>{{ t(prefix + 'server') }}</dt>
            <dd class="break-all">
              {{ result.tcp.serverAddress ?? result.udp.serverAddress ?? '—' }}
            </dd>
          </div>
          <div>
            <dt>{{ t(prefix + 'tcp') }}</dt>
            <dd class="break-all">{{ result.tcp.ip ?? stepError(result.tcp) }}</dd>
          </div>
          <div>
            <dt>{{ t(prefix + 'relay') }}</dt>
            <dd class="break-all">{{ result.udp.relayAddress ?? '—' }}</dd>
          </div>
          <div>
            <dt>{{ t(prefix + 'udp') }}</dt>
            <dd class="break-all">{{ udpAddress(result.udp) }}</dd>
          </div>
          <div>
            <dt>{{ t(prefix + 'dns') }}</dt>
            <dd class="break-all">
              {{ result.dns.addresses?.join(', ') ?? stepError(result.dns) }}
            </dd>
          </div>
        </dl>
        <p class="text-muted-foreground">{{ t(prefix + 'scope') }}</p>
      </div>
    </CardContent>
  </Card>
</template>
