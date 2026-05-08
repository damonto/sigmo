<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'

const message = defineModel<string>('message', { required: true })
const recipient = defineModel<string>('recipient')

const props = defineProps<{
  isNewConversation: boolean
  isSending: boolean
  isLoading: boolean
}>()

const emit = defineEmits<{
  (event: 'submit'): void
}>()

const { t } = useI18n()

const messageLength = computed(() => message.value.length)
const hasMessage = computed(() => message.value.trim().length > 0)
const isSendDisabled = computed(() => props.isSending || props.isLoading || !hasMessage.value)
</script>

<template>
  <form
    class="mt-auto shrink-0 space-y-2 border-t border-border px-1 pt-3 pb-3"
    @submit.prevent="emit('submit')"
  >
    <div v-if="props.isNewConversation" class="flex items-stretch gap-2">
      <Input
        v-model="recipient"
        type="tel"
        inputmode="tel"
        autocomplete="tel"
        class="h-10"
        :placeholder="t('modemDetail.messages.newRecipientPlaceholder')"
        :aria-label="t('modemDetail.messages.newRecipientPlaceholder')"
        :disabled="props.isSending || props.isLoading"
      />
    </div>
    <div class="flex items-stretch gap-2">
      <Textarea
        v-model="message"
        rows="1"
        class="min-h-10 flex-1 resize-none"
        :placeholder="t('modemDetail.messages.replyPlaceholder')"
        :disabled="props.isSending || props.isLoading"
      />
      <Button class="h-10" type="submit" :disabled="isSendDisabled">
        <span v-if="props.isSending" class="inline-flex items-center gap-2">
          <Spinner class="size-4" />
          {{ t('modemDetail.messages.send') }}
        </span>
        <span v-else>{{ t('modemDetail.messages.send') }}</span>
      </Button>
    </div>
    <div class="text-xs text-muted-foreground">
      {{ t('modemDetail.messages.characters', { count: messageLength }) }}
    </div>
  </form>
</template>
