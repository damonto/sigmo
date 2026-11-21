<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import BackButton from '@/components/BackButton.vue'
import ModemSignalStatus from '@/components/modem/ModemSignalStatus.vue'
import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
import ModemTitleSwitcher from '@/components/modem/ModemTitleSwitcher.vue'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { useModems } from '@/composables/useModems'
import { useStickyTopBar } from '@/composables/useStickyTopBar'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'

import type { Modem } from '@/types/modem'

const props = withDefaults(
  defineProps<{
    modem: Modem | null
    isLoading: boolean
    variant?: 'mobile' | 'desktop'
  }>(),
  {
    variant: 'mobile',
  },
)

defineSlots<{
  actions?(): unknown
}>()

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const { modems } = useModems()

const titleClickWindowMs = 1200
const titleClickCount = ref(0)
const lastTitleClickAt = ref(0)
const backButtonRef = useTemplateRef<HTMLElement>('backButtonRef')
const { isStickyVisible } = useStickyTopBar(backButtonRef)
const topBarTitle = computed(() => {
  if (props.isLoading) return '...'
  return props.modem?.name ?? t('modemDetail.unknown')
})
const subtitle = computed(() => {
  if (!props.modem || props.modem.simKind !== 'physical') return t('modemDetail.subtitle')
  return t('modemDetail.subtitlePhysical')
})
const currentModemId = computed(() => props.modem?.id ?? '')
const title = computed(() => {
  if (!props.modem) return t('modemDetail.unknown')
  return props.modem.name.trim() || props.modem.sim.operatorName || props.modem.id
})
const simKindLabel = computed(() => {
  if (props.modem?.simKind === 'euicc') return t('labels.esim')
  if (props.modem?.simKind === 'physical') return t('labels.psim')
  return t('labels.simUnknown')
})
const phoneNumber = computed(() => {
  const modem = props.modem
  if (!modem?.number?.trim()) return ''
  return formatPhoneDisplay(modem.number)
})

const handleTitleClick = () => {
  if (route.name !== 'modem-detail' || !props.modem?.id || props.modem.simKind !== 'euicc') return
  const now = Date.now()
  titleClickCount.value =
    now - lastTitleClickAt.value > titleClickWindowMs ? 1 : titleClickCount.value + 1
  lastTitleClickAt.value = now
  if (titleClickCount.value < 7) return
  titleClickCount.value = 0
  lastTitleClickAt.value = 0
  void router.push({ name: 'modem-notifications', params: { id: props.modem.id } })
}

watch([currentModemId, () => route.name], () => {
  titleClickCount.value = 0
  lastTitleClickAt.value = 0
})

const handleBack = () => {
  void router.push('/')
}

const switchRouteName = computed(() => {
  if (route.name === 'modem-message-thread') return 'modem-messages'
  if (typeof route.name === 'string' && route.name.startsWith('modem-')) return route.name
  return 'modem-detail'
})

const handleModemSwitch = (modem: Modem) => {
  if (modem.id === currentModemId.value) return
  void router.push({ name: switchRouteName.value, params: { id: modem.id } })
}
</script>

<template>
  <header
    v-if="props.variant === 'desktop'"
    class="min-w-0 space-y-1.5"
  >
    <Skeleton
      v-if="props.isLoading"
      class="h-9 w-48 rounded bg-muted"
    />
    <template v-else>
      <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
        <h1
          class="min-w-0 text-2xl font-semibold wrap-anywhere tracking-tight"
          @click="handleTitleClick"
        >
          {{ title }}
        </h1>
        <Badge
          v-if="props.modem"
          variant="outline"
          >{{ simKindLabel }}</Badge
        >
      </div>
      <div
        v-if="props.modem"
        class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground"
      >
        <ModemSignalStatus
          :signal-quality="props.modem.signalQuality"
          :registration-state="props.modem.registrationState"
          :access-technology="props.modem.accessTechnology"
          :wifi-calling-connected="props.modem.wifiCallingConnected"
          :airplane-mode="props.modem.airplaneMode"
          :show-signal-value="false"
        />
        <span v-if="phoneNumber">{{ phoneNumber }}</span>
      </div>
    </template>
  </header>
  <div
    v-else
    class="space-y-4 lg:hidden"
  >
    <ModemStickyTopBar
      :show="isStickyVisible"
      :title="topBarTitle"
      :back-label="t('modemDetail.back')"
      back-to="/"
    >
      <template #right>
        <slot name="actions" />
      </template>
    </ModemStickyTopBar>

    <div
      class="flex items-center justify-between gap-3"
      :class="{ invisible: isStickyVisible }"
    >
      <div
        ref="backButtonRef"
        class="inline-flex"
      >
        <BackButton
          :label="t('modemDetail.back')"
          @click="handleBack"
        />
      </div>
      <slot name="actions" />
    </div>

    <header class="min-w-0 space-y-3">
      <div class="flex min-w-0 max-w-full flex-wrap items-center gap-3">
        <div
          v-if="!props.isLoading"
          class="w-full min-w-0 max-w-full"
        >
          <ModemTitleSwitcher
            :current-modem="props.modem"
            :modems="modems"
            @select="handleModemSwitch"
            @title-click="handleTitleClick"
          />
        </div>
        <Skeleton
          v-else
          class="h-9 w-48 rounded bg-muted"
        />
      </div>
      <p class="text-sm text-muted-foreground">
        {{ subtitle }}
      </p>
    </header>
  </div>
</template>
