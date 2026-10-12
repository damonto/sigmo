<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import EsimLineItem from '@/components/esim/EsimLineItem.vue'
import EsimTransferSourceIcon from '@/components/esim/EsimTransferSourceIcon.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import type { EsimTransferProfile, EsimTransferSource } from '@/types/esim'

const props = defineProps<{
  selectedSource: EsimTransferSource | null
  selectedSourceDetail: string
  loading: boolean
  ready: boolean
  stageLabel: string
  profiles: EsimTransferProfile[]
  selectedProfileId?: string
  needsSourceImei: boolean
  errorMessage: string
}>()

const emit = defineEmits<{
  (event: 'select-profile', id: string): void
}>()

const sourceImei = defineModel<string>('sourceImei', { required: true })

const { t } = useI18n()
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto pr-1">
    <div
      v-if="props.selectedSource"
      class="flex shrink-0 items-center gap-3 rounded-lg border border-transparent bg-card px-4 py-3 shadow-sm"
    >
      <EsimTransferSourceIcon :type="props.selectedSource.type" />
      <span class="min-w-0">
        <span class="block text-xs font-medium text-muted-foreground">
          {{ t('modemDetail.esim.transferSource') }}
        </span>
        <span class="block truncate text-sm font-semibold text-foreground">
          {{ props.selectedSource.name }}
        </span>
        <span class="block truncate text-xs text-muted-foreground">
          {{ props.selectedSourceDetail }}
        </span>
      </span>
    </div>

    <div v-if="props.loading" class="flex shrink-0 items-center gap-3 rounded-lg bg-muted/40 p-4">
      <Spinner class="size-5" />
      <div class="min-w-0">
        <p class="text-sm font-medium">{{ props.stageLabel }}</p>
        <p class="text-xs text-muted-foreground">
          {{ t('modemDetail.esim.transferRunning') }}
        </p>
      </div>
    </div>

    <div v-if="props.ready" class="flex flex-col gap-5">
      <section v-if="props.profiles.length > 0" class="flex flex-col gap-3">
        <p class="shrink-0 text-sm font-medium">{{ t('modemDetail.esim.transferProfile') }}</p>
        <div class="space-y-3">
          <EsimLineItem
            v-for="profile in props.profiles"
            :key="profile.id"
            :profile="profile"
            :selected="props.selectedProfileId === profile.id"
            @select="emit('select-profile', $event)"
          />
        </div>
      </section>

      <section v-if="props.needsSourceImei && props.selectedProfileId" class="space-y-2">
        <Input
          v-model="sourceImei"
          :placeholder="t('modemDetail.esim.transferSourceImei')"
        />
      </section>

      <Alert v-if="props.profiles.length === 0" class="border-dashed">
        <AlertDescription>
          {{ t('modemDetail.esim.transferNoProfiles') }}
        </AlertDescription>
      </Alert>
    </div>

    <Alert v-if="props.errorMessage" variant="destructive">
      <AlertDescription>
        {{ props.errorMessage }}
      </AlertDescription>
    </Alert>
  </div>
</template>
