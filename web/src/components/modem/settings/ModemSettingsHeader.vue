<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import BackButton from '@/components/BackButton.vue'
import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
import { useStickyTopBar } from '@/composables/useStickyTopBar'

type BackRoute = string | { name: string; params?: Record<string, string> }

const props = withDefaults(
  defineProps<{
    title?: string
    subtitle?: string
    backTo?: BackRoute
    backLabel?: string
  }>(),
  {
    title: '',
    subtitle: '',
    backTo: '/',
    backLabel: '',
  },
)

const { t } = useI18n()
const backButtonRef = ref<HTMLElement | null>(null)
const { isStickyVisible } = useStickyTopBar(backButtonRef)

const title = props.title || t('modemDetail.settings.title')
const subtitle = props.subtitle || t('modemDetail.settings.subtitle')
const backLabel = props.backLabel || t('modemDetail.back')
</script>

<template>
  <header class="space-y-3">
    <ModemStickyTopBar
      :show="isStickyVisible"
      :title="title"
      :back-label="backLabel"
      :back-to="props.backTo"
    />

    <div class="space-y-1">
      <div ref="backButtonRef" class="inline-flex" :class="{ invisible: isStickyVisible }">
        <BackButton :to="props.backTo" :label="backLabel" />
      </div>
      <h1 class="text-2xl font-semibold text-foreground">
        {{ title }}
      </h1>
      <p class="text-sm text-muted-foreground">
        {{ subtitle }}
      </p>
    </div>
  </header>
</template>
