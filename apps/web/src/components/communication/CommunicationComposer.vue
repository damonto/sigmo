<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import CommunicationModemSelect from '@/components/communication/CommunicationModemSelect.vue'
import { useCommunicationComposer } from '@/composables/useCommunicationComposer'
const props = defineProps<{
  participant: string
  mode: 'message' | 'call'
  modemId?: string
  /** Device pages must keep communication on this modem, even if it becomes unavailable. */
  fixedModemId?: string
}>()
const emit = defineEmits<{ close: []; sent: [] }>()
const { t } = useI18n()
const number = ref(props.participant)
const mode = computed(() => props.mode)
const { modems, selectedID, message, loading, busy, error, unavailable, canSubmit, submit } =
  useCommunicationComposer(
    number,
    mode,
    computed(() => props.modemId ?? ''),
    computed(() => props.fixedModemId ?? ''),
  )

const send = async () => {
  if (await submit()) {
    emit('sent')
    emit('close')
  }
}
</script>
<template>
  <Dialog
    :open="true"
    @update:open="
      (open: boolean) => {
        if (!open && !busy) emit('close')
      }
    "
  >
    <DialogContent
      ><DialogHeader
        ><DialogTitle>{{
          t(`communication.${mode === 'call' ? 'dial' : 'newMessage'}`)
        }}</DialogTitle
        ><DialogDescription>{{ t('communication.currentModem') }}</DialogDescription></DialogHeader
      >
      <label class="grid gap-2"
        >{{ t('communication.number')
        }}<Input
          v-model="number"
          type="tel"
          :disabled="busy"
      /></label>
      <CommunicationModemSelect
        v-model="selectedID"
        :modems="modems"
        :disabled="loading || busy"
        :unavailable="unavailable"
      />
      <label
        v-if="mode === 'message'"
        class="grid gap-2"
        >{{ t('communication.message')
        }}<Textarea
          v-model="message"
          :disabled="busy"
          rows="5"
      /></label>
      <p
        v-if="error"
        role="alert"
        class="text-destructive"
      >
        {{ error }}
      </p>
      <Button
        :disabled="!canSubmit"
        @click="send"
        >{{ t(`communication.${mode === 'call' ? 'dial' : 'send'}`) }}</Button
      >
    </DialogContent>
  </Dialog>
</template>
