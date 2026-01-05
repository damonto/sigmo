<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useStickyTopBar } from '@/composables/useStickyTopBar'

const props = defineProps<{
  count: number
  isLoading: boolean
}>()

const { t } = useI18n()

const badgeLabel = computed(() => (props.isLoading ? '...' : String(props.count)))
const backButtonRef = ref<HTMLElement | null>(null)
const { isStickyVisible } = useStickyTopBar(backButtonRef)
</script>

<template>
  <header class="space-y-3 pb-3">
    <ModemStickyTopBar
      :show="isStickyVisible"
      :title="t('modemDetail.messages.title')"
      :back-label="t('modemDetail.back')"
      back-to="/"
    >
      <template #right>
        <Badge variant="outline" class="text-[10px] uppercase tracking-[0.2em]">
          {{ badgeLabel }}
        </Badge>
      </template>
    </ModemStickyTopBar>

    <div class="flex items-center justify-between gap-3">
      <div class="space-y-1">
        <div ref="backButtonRef" class="inline-flex" :class="{ invisible: isStickyVisible }">
          <Button as-child variant="ghost" size="sm" class="px-0 text-muted-foreground">
            <RouterLink to="/">
              &larr; {{ t('modemDetail.back') }}
            </RouterLink>
          </Button>
        </div>
        <div class="space-y-1">
          <h1 class="text-2xl font-semibold text-foreground">
            {{ t('modemDetail.messages.title') }}
          </h1>
          <p class="text-sm text-muted-foreground">
            {{ t('modemDetail.messages.subtitle') }}
          </p>
        </div>
      </div>
      <Badge variant="outline" class="text-[10px] uppercase tracking-[0.2em]">
        {{ badgeLabel }}
      </Badge>
    </div>

  </header>
</template>
