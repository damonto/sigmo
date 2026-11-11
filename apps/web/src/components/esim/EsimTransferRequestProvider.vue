<script setup lang="ts">
import { computed } from 'vue'
import {
  ArrowDownToLine,
  ArrowRightLeft,
  ArrowUpRight,
  CardSim,
  Check,
  ChevronDown,
  Circle,
  CircleCheck,
  CircleDot,
  CircleX,
  Link2,
  Link2Off,
  LoaderCircle,
  Network,
  RadioTower,
  Router,
  ShieldCheck,
  TriangleAlert,
  Unplug,
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'

import EsimPersistentDialogContent from '@/components/esim/EsimPersistentDialogContent.vue'
import EsimProfileAvatar from '@/components/esim/EsimProfileAvatar.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Dialog, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { useEsimTransferRequests } from '@/composables/useEsimTransferRequests'
import { useEsimTransferRequestSelection } from '@/composables/useEsimTransferRequestSelection'
import { transferStageLabelKey } from '@/constants/esimTransfer'
import { isTransferApproval } from '@/types/esimPeer'

const { t } = useI18n()
const { current, busy, error, decide, dismiss } = useEsimTransferRequests()
const approval = computed(() => (current.value ? isTransferApproval(current.value) : false))
const {
  sourceKey,
  profileID,
  sourceImei,
  source: chosenSource,
  profile: chosenProfile,
  profiles: sourceProfiles,
  selection,
  requiresSourceImei,
} = useEsimTransferRequestSelection(current)
const displayProfile = computed(() => current.value?.profile ?? chosenProfile.value)
const displaySourceName = computed(
  () => current.value?.sourceName ?? chosenSource.value?.source.name,
)
const running = computed(() => current.value?.phase === 'running')
const ended = computed(() => current.value?.phase === 'ended')
const uncertain = computed(() => ended.value && current.value?.outcome === 'uncertain')
const completed = computed(() => ended.value && current.value?.outcome === 'completed')

const presentation = computed(() => {
  if (approval.value) {
    return {
      title: 'sourceSelectionTitle',
      hint: 'sourceSelectionHint',
      badge: 'requestBadge',
      icon: ShieldCheck,
    }
  }
  if (running.value) {
    return { title: 'runningTitle', hint: 'running', badge: 'runningBadge', icon: ArrowRightLeft }
  }
  if (completed.value) {
    return { title: 'completedTitle', hint: 'completed', badge: 'completedBadge', icon: Check }
  }
  if (uncertain.value) {
    return {
      title: 'uncertainTitle',
      hint: 'uncertainDescription',
      badge: 'uncertainBadge',
      icon: Unplug,
    }
  }
  return { title: 'endedTitle', hint: 'failed', badge: 'endedBadge', icon: CircleX }
})
const tone = computed(() => {
  if (completed.value) return 'text-emerald-700 dark:text-emerald-300'
  if (uncertain.value) return 'text-amber-800 dark:text-amber-200'
  if (ended.value) return 'text-muted-foreground'
  return 'text-primary'
})
const targetSuffix = computed(() => {
  const target = current.value?.target
  if (!target) return ''
  return [
    t('esimPeer.identifierTail', { identifier: 'IMEI', suffix: target.imei.slice(-4) }),
    t('esimPeer.identifierTail', { identifier: 'EID', suffix: target.eid.slice(-4) }),
  ].join(' · ')
})
const stageHint = computed(() =>
  t(transferStageLabelKey[current.value?.stage ?? ''] ?? 'esimPeer.keepConnected'),
)
const closeLabel = computed(() =>
  t(
    running.value
      ? 'esimPeer.minimize'
      : uncertain.value
        ? 'esimPeer.acknowledge'
        : 'esimPeer.done',
  ),
)
const footerHint = computed(() => {
  if (approval.value) return t('esimPeer.transferSessionHint')
  if (running.value) return t('esimPeer.minimizeHint')
  return ''
})
const handleOpenChange = (open: boolean) => {
  if (!open && !busy.value) dismiss()
}
</script>

<template>
  <slot />
  <Dialog
    :open="!!current"
    @update:open="handleOpenChange"
  >
    <EsimPersistentDialogContent
      v-if="current"
      :show-close-button="false"
      class="flex max-h-[calc(100dvh-2rem)] flex-col gap-0 overflow-hidden rounded-3xl bg-popover p-0 text-popover-foreground sm:max-w-lg"
    >
      <div class="flex shrink-0 items-center justify-between gap-3 px-6 pt-6 sm:px-7">
        <div class="flex min-w-0 items-center gap-2 text-xs font-semibold">
          <RadioTower
            class="size-4 shrink-0 text-primary"
            aria-hidden="true"
          />
          <span>Sigmo</span>
          <span
            class="mx-1 hidden h-3 w-px bg-border sm:block"
            aria-hidden="true"
          />
          <span class="hidden text-xs font-normal text-muted-foreground sm:inline">{{
            t('esimPeer.transferLabel')
          }}</span>
        </div>
        <span
          class="flex shrink-0 items-center gap-1 text-xs font-medium"
          :class="tone"
          role="status"
        >
          <component
            :is="presentation.icon"
            class="size-3.5"
            aria-hidden="true"
          />
          {{ t('esimPeer.' + presentation.badge) }}
        </span>
      </div>

      <div class="min-h-0 overflow-y-auto px-6 pt-6 pb-2 sm:px-7">
        <DialogHeader class="gap-2 text-left">
          <DialogTitle class="text-2xl leading-snug font-semibold tracking-tight">
            {{ t('esimPeer.' + presentation.title) }}
          </DialogTitle>
          <DialogDescription class="text-sm leading-relaxed">
            {{ t('esimPeer.' + presentation.hint) }}
          </DialogDescription>
        </DialogHeader>

        <div
          v-if="approval"
          class="mt-5 space-y-4"
        >
          <div class="space-y-2">
            <Label for="peer-source">{{ t('esimPeer.sourceDevice') }}</Label>
            <Select
              v-model="sourceKey"
              :disabled="busy"
            >
              <SelectTrigger
                id="peer-source"
                class="w-full"
              >
                <SelectValue :placeholder="t('esimPeer.chooseSource')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="entry in current.sources"
                  :key="entry.source.type + ':' + entry.source.id"
                  :value="entry.source.type + ':' + entry.source.id"
                >
                  {{ entry.source.name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="space-y-2">
            <Label for="peer-profile">{{ t('esimPeer.sourceProfile') }}</Label>
            <Select
              v-model="profileID"
              :disabled="busy || !chosenSource"
            >
              <SelectTrigger
                id="peer-profile"
                class="w-full"
              >
                <SelectValue :placeholder="t('esimPeer.chooseProfile')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="profile in sourceProfiles"
                  :key="profile.id"
                  :value="profile.id"
                >
                  {{ profile.name }} ·
                  {{
                    t('esimPeer.identifierTail', {
                      identifier: 'ICCID',
                      suffix: profile.iccid.slice(-4),
                    })
                  }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div
            v-if="requiresSourceImei"
            class="space-y-2"
          >
            <Label for="peer-source-imei">{{ t('modemDetail.esim.transferSourceImei') }}</Label>
            <Input
              id="peer-source-imei"
              v-model="sourceImei"
              inputmode="numeric"
              maxlength="15"
              :disabled="busy"
              autocomplete="off"
            />
          </div>
          <p
            v-if="!current.sources?.length"
            class="text-sm text-muted-foreground"
          >
            {{ t('esimPeer.noSourceOptions') }}
          </p>
        </div>

        <div class="mt-6 overflow-hidden rounded-2xl border bg-muted/15">
          <div
            v-if="displayProfile"
            class="flex items-center gap-3 border-b p-4"
          >
            <EsimProfileAvatar
              v-if="displayProfile.icon || displayProfile.regionCode"
              :name="displayProfile.name"
              :icon="displayProfile.icon"
              :region-code="displayProfile.regionCode"
              aria-hidden="true"
            />
            <span
              v-else
              class="flex h-13 w-11 shrink-0 items-center justify-center rounded-xl border border-primary/20 bg-primary/5 text-primary"
              aria-hidden="true"
            >
              <CardSim class="size-6" />
            </span>
            <div class="min-w-0 flex-1">
              <p class="text-lg leading-snug font-semibold wrap-anywhere">
                {{ displayProfile.name }}
              </p>
              <p class="mt-1 text-xs text-muted-foreground tabular-nums">
                {{
                  t('esimPeer.identifierTail', {
                    identifier: 'ICCID',
                    suffix: displayProfile.iccid.slice(-4),
                  })
                }}
              </p>
            </div>
            <span class="shrink-0 rounded-md bg-muted px-2 py-1 text-xs text-muted-foreground">
              {{
                displayProfile.type === 'physical'
                  ? t('modemDetail.esim.transferPhysicalCard')
                  : 'eSIM'
              }}
            </span>
          </div>
          <div class="space-y-4 p-4">
            <div
              v-if="displaySourceName"
              class="relative flex gap-3 after:absolute after:top-9 after:-bottom-3 after:left-3.5 after:w-px after:bg-border"
            >
              <span
                class="flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground"
                aria-hidden="true"
              >
                <Router class="size-4" />
              </span>
              <div class="min-w-0">
                <p class="text-xs text-muted-foreground">{{ t('esimPeer.fromService') }}</p>
                <p class="mt-1 text-sm font-medium wrap-anywhere">{{ displaySourceName }}</p>
              </div>
            </div>
            <div class="flex gap-3">
              <span
                class="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"
                aria-hidden="true"
              >
                <ArrowDownToLine class="size-4" />
              </span>
              <div class="min-w-0">
                <p class="text-xs text-muted-foreground">{{ t('esimPeer.target') }}</p>
                <p class="mt-1 text-sm font-medium wrap-anywhere">{{ current.target.name }}</p>
                <p
                  class="mt-1 text-xs leading-relaxed text-muted-foreground tabular-nums wrap-anywhere"
                >
                  {{ targetSuffix }}
                </p>
              </div>
            </div>
          </div>
        </div>

        <div
          v-if="approval || uncertain"
          class="mt-4 flex gap-2.5 rounded-xl bg-amber-50 p-3 text-amber-900 dark:bg-amber-950/40 dark:text-amber-200"
        >
          <TriangleAlert
            class="mt-0.5 size-4 shrink-0"
            aria-hidden="true"
          />
          <div>
            <p class="text-sm font-medium">
              {{ t(uncertain ? 'esimPeer.checkDevicesTitle' : 'esimPeer.sourceInvalidTitle') }}
            </p>
            <p class="mt-1 text-xs leading-relaxed">
              {{ t(uncertain ? 'esimPeer.uncertain' : 'esimPeer.deletionConsent') }}
            </p>
          </div>
        </div>

        <ol
          v-if="running"
          class="mt-5 space-y-4"
          :aria-label="t('esimPeer.progressLabel')"
        >
          <li
            class="relative flex gap-2.5 after:absolute after:top-6 after:-bottom-3 after:left-2 after:w-px after:bg-border"
          >
            <CircleCheck
              class="mt-0.5 size-4 shrink-0 text-emerald-700 dark:text-emerald-300"
              aria-hidden="true"
            />
            <div>
              <p class="text-sm font-medium">{{ t('esimPeer.approvalsComplete') }}</p>
              <p class="mt-1 text-xs text-muted-foreground">{{ t('esimPeer.approvedProfile') }}</p>
            </div>
          </li>
          <li
            class="relative flex gap-2.5 after:absolute after:top-6 after:-bottom-3 after:left-2 after:w-px after:bg-border"
            aria-current="step"
          >
            <CircleDot
              class="mt-0.5 size-4 shrink-0 text-primary"
              aria-hidden="true"
            />
            <div>
              <p class="text-sm font-medium">{{ t('esimPeer.runningTitle') }}</p>
              <p
                class="mt-1 text-xs leading-relaxed text-muted-foreground"
                role="status"
              >
                {{ stageHint }}
              </p>
            </div>
          </li>
          <li class="flex gap-2.5 text-muted-foreground">
            <Circle
              class="mt-0.5 size-4 shrink-0"
              aria-hidden="true"
            />
            <p class="text-sm">{{ t('esimPeer.endAfterTransfer') }}</p>
          </li>
        </ol>

        <div
          v-if="ended"
          class="mt-4 flex gap-2.5 rounded-xl bg-muted/60 p-3"
        >
          <Link2Off
            class="mt-0.5 size-4 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <div>
            <p class="text-sm font-medium">{{ t('esimPeer.endedTitle') }}</p>
            <p class="mt-1 text-xs leading-relaxed text-muted-foreground">
              {{ t(uncertain ? 'esimPeer.noReplay' : 'esimPeer.ended') }}
            </p>
          </div>
        </div>

        <p class="mt-4 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
          <Network
            class="size-3.5 shrink-0"
            aria-hidden="true"
          />
          <span>{{ t('esimPeer.requestFrom') }}</span>
          <span class="text-foreground tabular-nums wrap-anywhere">{{
            current.remoteAddress
          }}</span>
        </p>
        <details
          :key="current.id"
          class="group mt-3"
        >
          <summary
            class="inline-flex min-h-9 cursor-pointer list-none items-center gap-1 text-xs text-muted-foreground outline-offset-4 focus-visible:outline-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden"
          >
            {{ t('esimPeer.deviceDetails') }}
            <ChevronDown
              class="size-3.5 transition-transform group-open:rotate-180 motion-reduce:transition-none"
              aria-hidden="true"
            />
          </summary>
          <dl
            class="mt-2 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 rounded-xl bg-muted/60 p-3 text-xs leading-relaxed"
          >
            <dt class="text-muted-foreground">IMEI</dt>
            <dd class="font-mono tabular-nums wrap-anywhere">{{ current.target.imei }}</dd>
            <dt class="text-muted-foreground">EID</dt>
            <dd class="font-mono tabular-nums wrap-anywhere">{{ current.target.eid }}</dd>
            <template v-if="displayProfile">
              <dt class="text-muted-foreground">ICCID</dt>
              <dd class="font-mono tabular-nums wrap-anywhere">{{ displayProfile.iccid }}</dd>
            </template>
          </dl>
        </details>
        <p
          v-if="error"
          class="mt-3 text-sm text-destructive"
          role="alert"
        >
          {{ error }}
        </p>
      </div>

      <div
        class="shrink-0 px-6 pt-4 pb-6 sm:px-7"
        :aria-busy="busy"
      >
        <div
          v-if="approval"
          class="flex gap-2.5"
        >
          <Button
            data-testid="reject-transfer-request"
            variant="outline"
            class="h-auto min-h-11 min-w-20 rounded-xl"
            :disabled="busy"
            @click="decide(current, false)"
          >
            {{ t('esimPeer.reject') }}
          </Button>
          <Button
            data-testid="approve-transfer-request"
            class="h-auto min-h-11 min-w-0 flex-1 rounded-xl py-2.5 whitespace-normal dark:bg-rose-400 dark:text-rose-950 dark:hover:bg-rose-300"
            :disabled="busy || !selection"
            @click="decide(current, true, selection)"
          >
            <LoaderCircle
              v-if="busy"
              class="size-4 animate-spin motion-reduce:animate-none"
              aria-hidden="true"
            />
            <span class="min-w-0">{{ t('esimPeer.allowTransfer') }}</span>
            <ArrowUpRight
              v-if="!busy"
              class="size-4"
              aria-hidden="true"
            />
          </Button>
        </div>
        <Button
          v-else
          :variant="running ? 'secondary' : 'default'"
          class="h-11 w-full rounded-xl"
          @click="dismiss"
        >
          {{ closeLabel }}
        </Button>
        <p
          v-if="footerHint"
          class="mt-3 flex items-start justify-center gap-1.5 text-center text-xs leading-relaxed text-muted-foreground"
        >
          <Link2
            class="mt-0.5 size-3.5 shrink-0"
            aria-hidden="true"
          />
          {{ footerHint }}
        </p>
      </div>
    </EsimPersistentDialogContent>
  </Dialog>
</template>
