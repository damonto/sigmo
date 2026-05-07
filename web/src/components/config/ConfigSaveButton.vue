<script setup lang="ts">
import { Save } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'

defineProps<{
  disabled?: boolean
  saving?: boolean
}>()

const emit = defineEmits<{
  save: []
}>()

const { t } = useI18n()
</script>

<template>
  <Button type="button" :disabled="disabled" @click="emit('save')">
    <span v-if="saving" class="inline-flex items-center gap-2">
      <Spinner class="size-4" />
      {{ t('config.save') }}
    </span>
    <span v-else class="inline-flex items-center gap-2">
      <Save class="size-4" />
      {{ t('config.save') }}
    </span>
  </Button>
</template>
