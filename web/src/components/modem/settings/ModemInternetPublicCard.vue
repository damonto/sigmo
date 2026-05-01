<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { InternetPublicResponse } from '@/types/internet'

const props = defineProps<{
  publicInfo: InternetPublicResponse | null
}>()

const { t } = useI18n()

const publicIPLabel = computed(() => props.publicInfo?.ip || t('modemDetail.settings.internetNone'))
const publicRegionLabel = computed(() => {
  const country = props.publicInfo?.country?.trim().toUpperCase()
  const flag = countryFlag(country)
  if (!flag && !country) return t('modemDetail.settings.internetNone')
  return [flag, country].filter(Boolean).join(' ')
})
const publicOrganizationLabel = computed(() => {
  return props.publicInfo?.organization || t('modemDetail.settings.internetNone')
})

const countryFlag = (country?: string) => {
  if (!country || !/^[A-Z]{2}$/.test(country)) return ''
  return String.fromCodePoint(
    ...country.split('').map((letter) => 0x1f1e6 + letter.charCodeAt(0) - 65),
  )
}
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
        <span class="break-all text-right font-medium text-foreground">{{
          publicRegionLabel
        }}</span>
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
