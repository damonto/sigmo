<script setup lang="ts">
import { Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import BackButton from '@/components/BackButton.vue'
import { Button } from '@/components/ui/button'

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
  <div
    class="grid grid-cols-[1fr_auto_1fr] items-center border-b border-border pb-3 lg:flex lg:justify-between lg:gap-3"
  >
    <BackButton
      class="justify-self-start lg:hidden"
      :label="t('modemDetail.back')"
      @click="emit('back')"
    />
    <h2
      class="min-w-0 text-center text-lg font-semibold text-foreground lg:flex-1 lg:truncate lg:text-left"
    >
      {{ props.title }}
    </h2>
    <Button
      variant="ghost"
      size="icon"
      type="button"
      class="justify-self-end"
      :aria-label="t('modemDetail.actions.delete')"
      :disabled="!props.canDelete"
      @click="emit('delete')"
    >
      <Trash2 class="size-4 text-muted-foreground" />
    </Button>
  </div>
</template>
