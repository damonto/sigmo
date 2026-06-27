<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import EsimProfileAvatar from '@/components/esim/EsimProfileAvatar.vue'
import {
  Dialog,
  DialogDescription,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { EsimProfile } from '@/types/esim'

const props = defineProps<{
  profile: EsimProfile | null
}>()

const open = defineModel<boolean>('open', { required: true })

const { t } = useI18n()
const focusTarget = ref<HTMLElement | null>(null)

type DetailRow = {
  label: string
  value: string
}

const valueOrEmpty = (value?: string) => {
  const normalized = value?.trim()
  return normalized ?? ''
}

const ownerRows = computed<DetailRow[]>(() => {
  const owner = props.profile?.profileOwner
  return [
    { label: t('modemDetail.esim.mcc'), value: valueOrEmpty(owner?.mcc) },
    { label: t('modemDetail.esim.mnc'), value: valueOrEmpty(owner?.mnc) },
    { label: t('modemDetail.esim.gid1'), value: valueOrEmpty(owner?.gid1) },
    { label: t('modemDetail.esim.gid2'), value: valueOrEmpty(owner?.gid2) },
  ]
})

const profileRows = computed<DetailRow[]>(() => {
  const profile = props.profile
  return [
    { label: t('modemDetail.esim.displayName'), value: valueOrEmpty(profile?.name) },
    {
      label: t('modemDetail.esim.serviceProvider'),
      value: valueOrEmpty(profile?.serviceProviderName),
    },
    { label: t('modemDetail.esim.profileName'), value: valueOrEmpty(profile?.profileName) },
    { label: t('modemDetail.esim.nickname'), value: valueOrEmpty(profile?.profileNickname) },
    { label: t('modemDetail.esim.iccid'), value: valueOrEmpty(profile?.iccid) },
    { label: t('modemDetail.esim.isdPAID'), value: valueOrEmpty(profile?.isdPAID) },
    { label: t('modemDetail.esim.state'), value: valueOrEmpty(profile?.profileStateName) },
    { label: t('modemDetail.esim.profileClass'), value: valueOrEmpty(profile?.profileClass) },
  ]
})

const focusDialogTitle = (event: Event) => {
  event.preventDefault()
  focusTarget.value?.focus({ preventScroll: true })
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent
      class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg"
      @open-auto-focus="focusDialogTitle"
    >
      <DialogHeader>
        <div ref="focusTarget" tabindex="-1" class="space-y-1 outline-none">
          <DialogTitle>{{ t('modemDetail.esim.profileDetailsTitle') }}</DialogTitle>
          <DialogDescription>
            {{ props.profile?.iccid ?? '' }}
          </DialogDescription>
        </div>
      </DialogHeader>

      <div v-if="props.profile" class="space-y-6">
        <div class="flex min-w-0 items-center gap-3">
          <EsimProfileAvatar
            :name="props.profile.name"
            :icon="props.profile.logoUrl"
            :region-code="props.profile.regionCode"
          />
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold text-foreground">{{ props.profile.name }}</p>
            <p class="truncate text-xs text-muted-foreground">
              {{ props.profile.serviceProviderName }}
            </p>
          </div>
        </div>

        <section class="space-y-2">
          <h2 class="text-base font-semibold text-foreground">
            {{ t('modemDetail.esim.profileDetailsTitle') }}
          </h2>
          <dl class="grid gap-3 text-sm">
            <div
              v-for="row in profileRows"
              :key="row.label"
              class="flex items-center justify-between gap-4"
            >
              <dt class="text-muted-foreground">{{ row.label }}</dt>
              <dd class="min-w-0 break-all text-right font-mono text-foreground">
                {{ row.value }}
              </dd>
            </div>
          </dl>
        </section>

        <section class="space-y-2">
          <h2 class="text-base font-semibold text-foreground">
            {{ t('modemDetail.esim.profileOwner') }}
          </h2>
          <dl class="grid gap-3 text-sm">
            <div
              v-for="row in ownerRows"
              :key="row.label"
              class="flex items-center justify-between gap-4"
            >
              <dt class="text-muted-foreground">{{ row.label }}</dt>
              <dd class="min-w-0 break-all text-right font-mono text-foreground">
                {{ row.value }}
              </dd>
            </div>
          </dl>
        </section>
      </div>
    </DialogContent>
  </Dialog>
</template>
