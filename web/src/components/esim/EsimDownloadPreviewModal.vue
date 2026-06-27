<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ChevronDown, ChevronUp } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import EsimPersistentDialogContent from '@/components/esim/EsimPersistentDialogContent.vue'
import RegionFlag from '@/components/RegionFlag.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { EsimDownloadPreview } from '@/types/esim'

const props = defineProps<{
  open: boolean
  title: string
  hint: string
  profile: EsimDownloadPreview | null
  confirmLabel: string
  cancelLabel: string
}>()

const emit = defineEmits<{
  (event: 'confirm'): void
  (event: 'cancel'): void
}>()

const { t } = useI18n()
const detailsOpen = ref(false)

const profileName = computed(() => {
  return props.profile?.profileName || props.profile?.serviceProviderName || ''
})

const profileSubtitle = computed(() => props.profile?.iccid ?? '')

const logoUrl = computed(() => props.profile?.icon ?? '')
const regionCode = computed(() => props.profile?.regionCode ?? '')

const unavailable = computed(() => t('modemDetail.esim.detailUnavailable'))

const valueOrUnavailable = (value?: string) => {
  const normalized = value?.trim()
  return normalized ? normalized : unavailable.value
}

const profileOwnerText = computed(() => {
  const owner = props.profile?.profileOwner
  const mcc = valueOrUnavailable(owner?.mcc)
  const mnc = valueOrUnavailable(owner?.mnc)
  return `${t('modemDetail.esim.mcc')}: ${mcc} / ${t('modemDetail.esim.mnc')}: ${mnc}`
})

const detailRows = computed(() => [
  {
    label: t('modemDetail.esim.serviceProvider'),
    value: valueOrUnavailable(props.profile?.serviceProviderName),
  },
  {
    label: t('modemDetail.esim.profileName'),
    value: valueOrUnavailable(props.profile?.profileName),
  },
  {
    label: t('modemDetail.esim.profileOwner'),
    value: profileOwnerText.value,
  },
])

const handleOpenChange = (nextOpen: boolean) => {
  if (!nextOpen) emit('cancel')
}

watch(
  () => [props.open, props.profile?.iccid],
  () => {
    detailsOpen.value = false
  },
)
</script>

<template>
  <Dialog :open="props.open" @update:open="handleOpenChange">
    <EsimPersistentDialogContent class="sm:max-w-sm">
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
        <DialogDescription>{{ hint }}</DialogDescription>
      </DialogHeader>
      <Card class="border-0 py-0 shadow-sm">
        <CardContent class="flex items-center gap-3 p-3">
          <div
            class="flex size-12 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30"
          >
            <img v-if="logoUrl" :src="logoUrl" class="size-7 object-contain" />
            <RegionFlag v-else :region-code="regionCode" class="rounded-sm text-base" />
          </div>
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold text-foreground">{{ profileName }}</p>
            <p class="truncate text-xs text-muted-foreground">{{ profileSubtitle }}</p>
          </div>
        </CardContent>
      </Card>
      <div v-if="profile" class="space-y-2">
        <Button
          variant="ghost"
          type="button"
          class="h-8 w-full justify-between px-2 text-xs"
          @click="detailsOpen = !detailsOpen"
        >
          <span>
            {{
              t(detailsOpen ? 'modemDetail.esim.hideDetails' : 'modemDetail.esim.showDetails')
            }}
          </span>
          <ChevronUp v-if="detailsOpen" class="size-4" />
          <ChevronDown v-else class="size-4" />
        </Button>
        <dl v-if="detailsOpen" class="rounded-md border border-border bg-muted/20 p-3 text-xs">
          <div
            v-for="row in detailRows"
            :key="row.label"
            class="grid grid-cols-[7rem_minmax(0,1fr)] gap-3 py-1"
          >
            <dt class="text-muted-foreground">{{ row.label }}</dt>
            <dd class="min-w-0 wrap-break-word font-medium text-foreground">{{ row.value }}</dd>
          </div>
        </dl>
      </div>
      <DialogFooter class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Button type="button" class="order-1 w-full sm:order-2" @click="emit('confirm')">
          {{ confirmLabel }}
        </Button>
        <Button
          variant="ghost"
          type="button"
          class="order-2 w-full sm:order-1"
          @click="emit('cancel')"
        >
          {{ cancelLabel }}
        </Button>
      </DialogFooter>
    </EsimPersistentDialogContent>
  </Dialog>
</template>
