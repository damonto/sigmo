<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

type TransferUserInput = {
  text: string
  acceptLabel?: string
  rejectLabel?: string
  freeText: boolean
  freeTextHint?: string
}

const props = defineProps<{
  input: TransferUserInput | null
}>()

const emit = defineEmits<{
  (event: 'submit', accept: boolean): void
}>()

const response = defineModel<string>('response', { required: true })

const { t } = useI18n()
</script>

<template>
  <div class="space-y-3">
    <p class="text-sm text-muted-foreground">{{ props.input?.text }}</p>
    <Input
      v-if="props.input?.freeText"
      v-model="response"
      :placeholder="props.input?.freeTextHint || t('modemDetail.validation.required')"
    />
    <div class="grid grid-cols-2 gap-3">
      <Button variant="outline" @click="emit('submit', false)">
        {{ props.input?.rejectLabel || t('modemDetail.actions.cancel') }}
      </Button>
      <Button @click="emit('submit', true)">
        {{ props.input?.acceptLabel || t('modemDetail.actions.confirm') }}
      </Button>
    </div>
  </div>
</template>
