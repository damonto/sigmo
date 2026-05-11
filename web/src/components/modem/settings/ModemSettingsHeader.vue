<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import BackButton from '@/components/BackButton.vue'
import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
import { useStickyTopBar } from '@/composables/useStickyTopBar'

const { t } = useI18n()
const backButtonRef = ref<HTMLElement | null>(null)
const { isStickyVisible } = useStickyTopBar(backButtonRef)
</script>

<template>
  <header class="space-y-3">
    <ModemStickyTopBar
      :show="isStickyVisible"
      :title="t('modemDetail.settings.title')"
      :back-label="t('modemDetail.back')"
      back-to="/"
    />

    <div class="space-y-1">
      <div ref="backButtonRef" class="inline-flex" :class="{ invisible: isStickyVisible }">
        <BackButton to="/" :label="t('modemDetail.back')" />
      </div>
      <h1 class="text-2xl font-semibold text-foreground">
        {{ t('modemDetail.settings.title') }}
      </h1>
      <p class="text-sm text-muted-foreground">
        {{ t('modemDetail.settings.subtitle') }}
      </p>
    </div>
  </header>
</template>
