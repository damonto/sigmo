<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
import { Button } from '@/components/ui/button'
import { useStickyTopBar } from '@/composables/useStickyTopBar'

const { t } = useI18n()
const backButtonRef = ref<HTMLElement | null>(null)
const { isStickyVisible } = useStickyTopBar(backButtonRef)
</script>

<template>
  <header class="space-y-3 pb-3">
    <ModemStickyTopBar
      :show="isStickyVisible"
      :title="t('modemDetail.ussd.title')"
      :back-label="t('modemDetail.back')"
      back-to="/"
    />

    <div class="space-y-1">
      <div ref="backButtonRef" class="inline-flex" :class="{ invisible: isStickyVisible }">
        <Button as-child variant="ghost" size="sm" class="px-0 text-muted-foreground">
          <RouterLink to="/">
            &larr; {{ t('modemDetail.back') }}
          </RouterLink>
        </Button>
      </div>
      <h1 class="text-2xl font-semibold text-foreground">
        {{ t('modemDetail.ussd.title') }}
      </h1>
      <p class="text-sm text-muted-foreground">
        {{ t('modemDetail.ussd.description') }}
      </p>
    </div>
  </header>
</template>
