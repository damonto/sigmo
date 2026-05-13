<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import RegionFlag from '@/components/RegionFlag.vue'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { InternetPublicResponse } from '@/types/internet'

const props = defineProps<{
  publicInfo: InternetPublicResponse | null
}>()

const { t } = useI18n()

const publicIPLabel = computed(() => props.publicInfo?.ip || t('modemDetail.settings.internetNone'))
const publicCountry = computed(() => props.publicInfo?.country?.trim().toUpperCase() ?? '')
const publicRegionMissing = computed(() => !publicCountry.value)
const publicOrganizationLabel = computed(() => {
  return props.publicInfo?.organization || t('modemDetail.settings.internetNone')
})
</script>

<template>
  <Card class="gap-4 rounded-2xl border-0 py-4 shadow-sm">
    <CardHeader class="px-4">
      <CardTitle class="text-base">
        {{ t('modemDetail.settings.internetPublicInfoTitle') }}
      </CardTitle>
    </CardHeader>

    <CardContent class="space-y-2 px-4 text-sm">
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.internetPublicIP') }}</span>
        <span class="break-all text-right font-medium text-foreground">{{ publicIPLabel }}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{ t('modemDetail.settings.internetRegion') }}</span>
        <span
          v-if="publicRegionMissing"
          class="break-all text-right font-medium text-foreground"
        >
          {{ t('modemDetail.settings.internetNone') }}
        </span>
        <span
          v-else
          class="flex items-center justify-end gap-2 text-right font-medium text-foreground"
        >
          <RegionFlag :region-code="publicCountry" class="rounded-sm text-base" />
        </span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="text-muted-foreground">{{
          t('modemDetail.settings.internetOrganization')
        }}</span>
        <span class="break-all text-right font-medium text-foreground">{{
          publicOrganizationLabel
        }}</span>
      </div>
    </CardContent>
  </Card>
</template>
