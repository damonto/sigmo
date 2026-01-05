<script setup lang="ts">
import { computed, ref } from 'vue'
import { Info } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
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

const titleClickCount = ref(0)
const backButtonRef = ref<HTMLElement | null>(null)
const { isStickyVisible } = useStickyTopBar(backButtonRef)
const topBarTitle = computed(() => {
  if (props.isLoading) return '...'
  return props.modem?.name ?? t('modemDetail.unknown')
})

const handleTitleClick = () => {
  if (!props.modem?.id || !props.modem.supportsEsim) return
  titleClickCount.value += 1
  if (titleClickCount.value < 7) return
  titleClickCount.value = 0
  void router.push({ name: 'modem-notifications', params: { id: props.modem.id } })
}
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
    </div>

    <header class="space-y-2">
      <div class="flex flex-wrap items-center gap-3">
        <h1
          v-if="!props.isLoading"
          class="text-3xl font-semibold tracking-tight text-foreground"
          @click="handleTitleClick"
        >
          {{ props.modem?.name ?? t('modemDetail.unknown') }}
        </h1>
        <Skeleton v-else class="h-9 w-48 rounded bg-muted" />
      </div>
      <p class="text-sm text-muted-foreground">
        {{ t('modemDetail.subtitle') }}
      </p>
    </header>

  </div>
</template>
