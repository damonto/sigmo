<script setup lang="ts">
import { Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import BackButton from '@/components/BackButton.vue'
import CommunicationActionButton from '@/components/communication/CommunicationActionButton.vue'

const props = defineProps<{
  title: string
  canDelete: boolean
}>()

const emit = defineEmits<{
  (event: 'back'): void
  (event: 'delete'): void
}>()

const { t } = useI18n()
</script>

<template>
  <div class="flex flex-wrap items-center gap-3 border-b border-border pb-3">
    <BackButton
      class="justify-self-start lg:hidden"
      :label="t('modemDetail.back')"
      @click="emit('back')"
    />
    <h2 class="min-w-0 flex-1 truncate text-lg font-semibold text-foreground">
      {{ props.title }}
    </h2>
    <div class="ml-auto flex flex-wrap items-center justify-end gap-2">
      <slot name="actions" />
      <CommunicationActionButton
        destructive
        type="button"
        class="justify-self-end"
        :aria-label="t('modemDetail.actions.delete')"
        :disabled="!props.canDelete"
        @click="emit('delete')"
      >
        <Trash2 class="size-4" />
        {{ t('modemDetail.actions.delete') }}
      </CommunicationActionButton>
    </div>
  </div>
</template>
