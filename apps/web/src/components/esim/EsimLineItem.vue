<script setup lang="ts">
import { Ban } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import EsimProfileAvatar from '@/components/esim/EsimProfileAvatar.vue'
import type { EsimTransferProfile } from '@/types/esim'

const props = defineProps<{
  profile: EsimTransferProfile
  selected: boolean
}>()

const emit = defineEmits<{
  (event: 'select', id: string): void
}>()

const { t } = useI18n()
</script>

<template>
  <button
    type="button"
    class="flex w-full items-center justify-between gap-3 rounded-lg border bg-card px-4 py-3 text-left shadow-sm transition"
    :class="[
      props.selected ? 'border-primary/40 bg-primary/5' : 'border-transparent',
      props.profile.supported ? 'hover:bg-muted/40' : 'cursor-not-allowed opacity-50',
    ]"
    :disabled="!props.profile.supported"
    @click="emit('select', props.profile.id)"
  >
    <span class="flex min-w-0 items-center gap-3">
      <EsimProfileAvatar
        :name="props.profile.name"
        :icon="props.profile.icon"
        :region-code="props.profile.regionCode"
      />
      <span class="min-w-0">
        <span class="block truncate text-sm font-semibold text-foreground">
          {{ props.profile.name }}
        </span>
        <span class="block truncate text-xs text-muted-foreground">
          {{
            props.profile.type === 'physical'
              ? t('modemDetail.esim.transferPhysicalCard')
              : props.profile.iccid
          }}
        </span>
      </span>
    </span>
    <span
      v-if="!props.profile.supported"
      class="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground"
      :aria-label="t('modemDetail.esim.transferUnsupported')"
      :title="t('modemDetail.esim.transferUnsupported')"
    >
      <Ban class="size-4" />
    </span>
  </button>
</template>
