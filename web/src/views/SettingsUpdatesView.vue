<script setup lang="ts">
import { CircleAlert, Download, RefreshCw, ShieldCheck } from 'lucide-vue-next'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from 'vue-sonner'

import { useAppApi } from '@/apis/app'
import { useUpdateApi } from '@/apis/update'
import SettingsHeader from '@/components/settings/SettingsHeader.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { localizeErrorMessage } from '@/lib/errorHandler'
import type { UpdateChannel, UpdateSnapshot } from '@/types/update'

const { t, locale } = useI18n()
const appApi = useAppApi()
const api = useUpdateApi()
const snapshot = ref<UpdateSnapshot>()
const loading = ref(true)
const loadFailed = ref(false)
const saving = ref(false)
const action = ref<'check' | 'install' | ''>('')
let pollTimer: number | undefined
let active = false
let installationStarted = false
let readinessDelay = 1000

const busy = computed(() =>
  ['checking', 'downloading', 'verifying', 'restarting'].includes(snapshot.value?.state ?? ''),
)
const isPro = computed(() => snapshot.value?.current.edition === 'pro')
const isContainer = computed(() => snapshot.value?.current.distribution === 'container')
const upToDate = computed(
  () =>
    Boolean(snapshot.value?.checkedAt) &&
    snapshot.value?.state === 'idle' &&
    !snapshot.value.updateAvailable &&
    !snapshot.value.error,
)
const serverVersion = computed(
  () => snapshot.value?.latest?.version ?? t('settings.updates.notChecked'),
)
const statusText = computed(() => t(`settings.updates.status.${snapshot.value?.state ?? 'idle'}`))
const licenseStatus = computed(() =>
  snapshot.value?.license?.status === 'active'
    ? t('settings.updates.licenseActive')
    : (snapshot.value?.license?.status ?? ''),
)
const snapshotError = computed(() => {
  if (!snapshot.value?.error) return ''
  return localizeErrorMessage(
    { error_code: snapshot.value.errorCode, message: snapshot.value.error },
    snapshot.value.error,
  )
})
const unsupportedDescription = computed(() =>
  snapshot.value?.unsupportedReason === 'developer_build'
    ? t('settings.updates.developerBuildDescription')
    : t('settings.updates.releaseKeyMissingDescription'),
)
const formatDate = (value?: string) =>
  value
    ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(
        new Date(value),
      )
    : t('settings.updates.never')

const load = async () => {
  loading.value = true
  loadFailed.value = false
  try {
    const { data } = await api.settings()
    if (active) snapshot.value = data.value
  } catch {
    if (active) loadFailed.value = true
  } finally {
    if (active) loading.value = false
  }
}

const save = async (automatic: boolean, channel: UpdateChannel) => {
  if (!snapshot.value || saving.value) return
  const channelChanged = snapshot.value.settings.channel !== channel
  saving.value = true
  try {
    const { data } = await api.saveSettings({ automatic, channel })
    if (!active) return
    snapshot.value = data.value
    toast.success(t('settings.updates.saved'))
    if (channelChanged) await check()
  } catch {
    // The shared fetch layer already presents the localized API error.
  } finally {
    saving.value = false
  }
}

const setAutomatic = (value: boolean) => {
  if (snapshot.value) void save(value, snapshot.value.settings.channel)
}
const setChannel = (value: unknown) => {
  if (snapshot.value && (value === 'stable' || value === 'dev'))
    void save(snapshot.value.settings.automatic, value)
}

const check = async () => {
  action.value = 'check'
  try {
    const { data } = await api.check()
    if (active) snapshot.value = data.value
  } catch {
    // The shared fetch layer already presents the localized API error.
  } finally {
    if (active) action.value = ''
  }
}

const stopPolling = () => {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer)
  pollTimer = undefined
}
const scheduleReadinessCheck = () => {
  pollTimer = window.setTimeout(waitForRestart, readinessDelay)
  readinessDelay = Math.min(readinessDelay * 2, 5000)
}
const waitForRestart = async () => {
  pollTimer = undefined
  if (!active) return
  try {
    if (await appApi.ready()) {
      window.location.reload()
      return
    }
  } catch {
    // The reverse proxy has no healthy upstream while the process restarts.
  }
  if (active) scheduleReadinessCheck()
}
const pollInstallation = async () => {
  pollTimer = undefined
  if (!active) return
  try {
    const next = await api.installation()
    if (!active) return
    snapshot.value = next
    if (installationStarted && snapshot.value?.state === 'idle') {
      window.location.reload()
      return
    }
    if (snapshot.value?.state === 'failed' || snapshot.value?.state === 'idle') {
      action.value = ''
      installationStarted = false
      return
    }
  } catch {
    if (snapshot.value) snapshot.value = { ...snapshot.value, state: 'restarting' }
    readinessDelay = 1000
    scheduleReadinessCheck()
    return
  }
  if (active) pollTimer = window.setTimeout(pollInstallation, 1000)
}
const install = async () => {
  action.value = 'install'
  installationStarted = false
  readinessDelay = 1000
  try {
    const { data } = await api.install()
    if (!active) return
    snapshot.value = data.value
    installationStarted = true
    pollTimer = window.setTimeout(pollInstallation, 500)
  } catch {
    if (active) action.value = ''
  }
}

onMounted(() => {
  active = true
  void load()
})
onUnmounted(() => {
  active = false
  stopPolling()
})
</script>

<template>
  <div class="space-y-4 pb-8">
    <SettingsHeader
      :title="t('settings.updates.title')"
      :description="t('settings.updates.description')"
    />
    <div
      v-if="loading"
      class="flex justify-center py-24"
    >
      <Spinner class="size-6" />
    </div>
    <div
      v-else-if="loadFailed"
      class="flex flex-col items-center gap-3 py-24 text-center"
    >
      <p class="text-sm text-muted-foreground">{{ t('settings.updates.loadFailed') }}</p>
      <Button
        variant="outline"
        @click="load"
        ><RefreshCw class="size-4" />{{ t('settings.updates.retry') }}</Button
      >
    </div>
    <template v-else-if="snapshot">
      <Alert v-if="isContainer">
        <Download />
        <AlertTitle>{{ t('settings.updates.containerTitle') }}</AlertTitle>
        <AlertDescription class="space-y-2">
          <p>{{ t('settings.updates.containerDescription') }}</p>
          <pre class="overflow-x-auto rounded bg-muted p-2 text-xs">
docker compose pull &amp;&amp; docker compose up -d</pre
          >
        </AlertDescription>
      </Alert>

      <Alert v-if="!snapshot.selfUpdateSupported && !isContainer">
        <CircleAlert />
        <AlertTitle>{{ t('settings.updates.selfUpdateUnavailableTitle') }}</AlertTitle>
        <AlertDescription>{{ unsupportedDescription }}</AlertDescription>
      </Alert>

      <Card>
        <CardHeader>
          <CardTitle>{{ t('settings.updates.preferences') }}</CardTitle>
          <CardDescription>{{ t('settings.updates.preferencesDescription') }}</CardDescription>
        </CardHeader>
        <CardContent class="space-y-5">
          <div class="flex items-center justify-between gap-4 rounded-lg border p-3">
            <div>
              <Label>{{ t('settings.updates.automatic') }}</Label>
              <p class="text-xs text-muted-foreground">
                {{ t('settings.updates.automaticDescription') }}
              </p>
            </div>
            <Switch
              data-testid="automatic-updates"
              :model-value="snapshot.settings.automatic"
              :disabled="saving || !snapshot.selfUpdateSupported"
              @update:model-value="setAutomatic($event === true)"
            />
          </div>
          <div class="space-y-2">
            <Label>{{ t('settings.updates.channel') }}</Label>
            <Select
              :model-value="snapshot.settings.channel"
              :disabled="saving || !isPro"
              @update:model-value="setChannel"
            >
              <SelectTrigger
                data-testid="update-channel"
                class="w-full"
                ><SelectValue
              /></SelectTrigger>
              <SelectContent>
                <SelectItem value="stable">Stable</SelectItem>
                <SelectItem
                  v-if="isPro"
                  value="dev"
                  >Dev</SelectItem
                >
              </SelectContent>
            </Select>
            <p class="text-xs text-muted-foreground">
              {{
                isPro
                  ? t('settings.updates.channelProDescription')
                  : t('settings.updates.channelCommunityDescription')
              }}
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="flex-row items-start justify-between gap-3">
          <div>
            <CardTitle>{{ t('settings.updates.versionTitle') }}</CardTitle>
            <CardDescription>{{
              t('settings.updates.checkedAt', { value: formatDate(snapshot.checkedAt) })
            }}</CardDescription>
          </div>
          <Badge :variant="snapshot.updateAvailable ? 'default' : 'secondary'">{{
            snapshot.updateAvailable
              ? t('settings.updates.available')
              : t('settings.updates.current')
          }}</Badge>
        </CardHeader>
        <CardContent class="space-y-4">
          <dl class="grid gap-3 text-sm sm:grid-cols-2">
            <div class="rounded-lg border p-3">
              <dt class="text-muted-foreground">{{ t('settings.updates.installedVersion') }}</dt>
              <dd class="mt-1 font-mono">{{ snapshot.current.version }}</dd>
            </div>
            <div class="rounded-lg border p-3">
              <dt class="text-muted-foreground">{{ t('settings.updates.serverVersion') }}</dt>
              <dd class="mt-1 font-mono">{{ serverVersion }}</dd>
            </div>
            <div class="rounded-lg border p-3">
              <dt class="text-muted-foreground">{{ t('settings.updates.target') }}</dt>
              <dd class="mt-1 font-mono">{{ snapshot.current.target }}</dd>
            </div>
            <div class="rounded-lg border p-3">
              <dt class="text-muted-foreground">{{ t('settings.updates.state') }}</dt>
              <dd class="mt-1">{{ statusText }}</dd>
            </div>
          </dl>
          <div
            v-if="snapshot.latest?.notes"
            class="space-y-2"
          >
            <Label>{{ t('settings.updates.notes') }}</Label>
            <pre
              class="max-h-72 overflow-auto whitespace-pre-wrap wrap-break-word rounded-lg bg-muted p-3 font-sans text-sm"
              >{{ snapshot.latest.notes }}</pre
            >
          </div>
          <p
            v-if="snapshot.error"
            class="text-sm text-destructive"
          >
            {{ snapshotError }}
          </p>
          <p
            v-if="upToDate"
            data-testid="up-to-date-message"
            class="flex items-center gap-2 text-sm text-primary"
          >
            <ShieldCheck class="size-4" />
            {{ t('settings.updates.upToDate') }}
          </p>
          <div class="flex flex-wrap gap-2">
            <Button
              variant="outline"
              :disabled="saving || busy || action !== ''"
              @click="check"
              ><Spinner
                v-if="action === 'check'"
                class="size-4"
              /><RefreshCw
                v-else
                class="size-4"
              />{{ t('settings.updates.checkNow') }}</Button
            >
            <Button
              :disabled="
                saving ||
                !snapshot.updateAvailable ||
                !snapshot.selfUpdateSupported ||
                busy ||
                action !== ''
              "
              @click="install"
              ><Spinner
                v-if="action === 'install'"
                class="size-4"
              /><Download
                v-else
                class="size-4"
              />{{ t('settings.updates.installNow') }}</Button
            >
          </div>
        </CardContent>
      </Card>

      <Card v-if="snapshot.license">
        <CardHeader
          ><CardTitle class="flex items-center gap-2"
            ><ShieldCheck class="size-4" />{{ t('settings.updates.licensedTo') }}</CardTitle
          ></CardHeader
        >
        <CardContent class="space-y-1 text-sm">
          <p class="font-medium">
            {{ snapshot.license.displayName }}
            <span
              v-if="snapshot.license.username"
              class="text-muted-foreground"
              >@{{ snapshot.license.username }}</span
            >
          </p>
          <p class="text-muted-foreground">Telegram ID: {{ snapshot.license.telegramId }}</p>
          <p class="text-muted-foreground">
            {{ t('settings.updates.licenseStatus') }}: {{ licenseStatus }}
          </p>
          <p class="text-muted-foreground">
            {{ t('settings.updates.licenseExpires') }}:
            {{
              snapshot.license.expiresAt
                ? formatDate(snapshot.license.expiresAt)
                : t('settings.updates.licenseNeverExpires')
            }}
          </p>
          <p
            v-if="snapshot.license.offlineUntil"
            class="text-muted-foreground"
          >
            {{ t('settings.updates.licenseOfflineUntil') }}:
            {{ formatDate(snapshot.license.offlineUntil) }}
          </p>
        </CardContent>
      </Card>
    </template>
  </div>
</template>
