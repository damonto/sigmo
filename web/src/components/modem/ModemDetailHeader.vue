<script setup lang="ts">
import { Info } from 'lucide-vue-next'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
import ModemTitleSwitcher from '@/components/modem/ModemTitleSwitcher.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useModems } from '@/composables/useModems'
import { useStickyTopBar } from '@/composables/useStickyTopBar'

import type { Modem } from '@/types/modem'

const props = withDefaults(
  defineProps<{
    modem: Modem | null
    isLoading: boolean
    showDetailsAction?: boolean
  }>(),
  {
    showDetailsAction: false,
  },
)

const emit = defineEmits<{
  (event: 'open-details'): void
}>()

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const { modems, fetchModems } = useModems()

const titleClickWindowMs = 1200
const titleClickCount = ref(0)
const lastTitleClickAt = ref(0)
const backButtonRef = ref<HTMLElement | null>(null)
const { isStickyVisible } = useStickyTopBar(backButtonRef)
const topBarTitle = computed(() => {
  if (props.isLoading) return '...'
  return props.modem?.name ?? t('modemDetail.unknown')
})
const currentModemId = computed(() => props.modem?.id ?? '')

const handleTitleClick = () => {
  if (!props.modem?.id || !props.modem.supportsEsim) return
  const now = Date.now()
  titleClickCount.value =
    now - lastTitleClickAt.value > titleClickWindowMs ? 1 : titleClickCount.value + 1
  lastTitleClickAt.value = now
  if (titleClickCount.value < 7) return
  titleClickCount.value = 0
  lastTitleClickAt.value = 0
  void router.push({ name: 'modem-notifications', params: { id: props.modem.id } })
}

watch(currentModemId, () => {
  titleClickCount.value = 0
  lastTitleClickAt.value = 0
})

const switchRouteName = computed(() => {
  if (route.name === 'modem-message-thread') return 'modem-messages'
  if (typeof route.name === 'string' && route.name.startsWith('modem-')) return route.name
  return 'modem-detail'
})

const handleModemSwitch = (modem: Modem) => {
  if (modem.id === currentModemId.value) return
  void router.push({ name: switchRouteName.value, params: { id: modem.id } })
}

onMounted(() => {
  if (modems.value.length > 0) return
  void fetchModems()
})
</script>

<template>
  <div class="space-y-4">
    <ModemStickyTopBar
      :show="isStickyVisible"
      :title="topBarTitle"
      :back-label="t('modemDetail.back')"
      back-to="/"
    >
      <template #right>
        <Button
          v-if="props.showDetailsAction"
          variant="ghost"
          size="icon"
          type="button"
          :aria-label="t('modemDetail.tabs.detail')"
          :title="t('modemDetail.tabs.detail')"
          @click="emit('open-details')"
        >
          <Info class="size-4 text-muted-foreground" />
        </Button>
      </template>
    </ModemStickyTopBar>

    <div class="flex items-center justify-between gap-3" :class="{ invisible: isStickyVisible }">
      <div ref="backButtonRef" class="inline-flex">
        <Button
          variant="ghost"
          size="sm"
          type="button"
          class="px-0 text-muted-foreground"
          @click="router.push('/')"
        >
          &larr; {{ t('modemDetail.back') }}
        </Button>
      </div>
      <div v-if="props.showDetailsAction" class="flex items-center gap-1.5">
        <Button
          variant="ghost"
          size="icon"
          type="button"
          :aria-label="t('modemDetail.tabs.detail')"
          :title="t('modemDetail.tabs.detail')"
          @click="emit('open-details')"
        >
          <Info class="size-4 text-muted-foreground" />
        </Button>
      </div>
    </div>

    <header class="space-y-3">
      <div class="flex flex-wrap items-center gap-3">
        <template v-if="!props.isLoading">
          <ModemTitleSwitcher
            :current-modem="props.modem"
            :modems="modems"
            @select="handleModemSwitch"
            @title-click="handleTitleClick"
          />
        </template>
        <Skeleton v-else class="h-9 w-48 rounded bg-muted" />
      </div>
      <p class="text-sm text-muted-foreground">
        {{ t('modemDetail.subtitle') }}
      </p>
    </header>
  </div>
</template>
