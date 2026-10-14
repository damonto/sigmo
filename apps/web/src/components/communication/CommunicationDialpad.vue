<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { Phone } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import CommunicationModemSelect from '@/components/communication/CommunicationModemSelect.vue'
import DraggableFab from '@/components/fab/DraggableFab.vue'
import ModemDialpad from '@/components/modem/ModemDialpad.vue'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { useCommunicationComposer } from '@/composables/useCommunicationComposer'
import { useDialpadInput } from '@/composables/useDialpadInput'
import { isCallableDialString } from '@/lib/phoneNumberInput'

const emit = defineEmits<{ dialed: [] }>()
const { t } = useI18n()
const desktop = useMediaQuery('(min-width: 1024px)')
const dialogOpen = ref(false)
const desktopRef = useTemplateRef<InstanceType<typeof ModemDialpad>>('desktopRef')
const mobileRef = useTemplateRef<InstanceType<typeof ModemDialpad>>('mobileRef')
const phoneCountry = ref('')
const originalModemID = ref('')
const {
  digits,
  normalizedDigits,
  keys,
  dialInputClass,
  setDigits,
  appendKey,
  backspace,
  startPlusLongPress,
  clearPlusLongPress,
} = useDialpadInput(phoneCountry)
const { modems, selectedID, loading, busy, error, unavailable, canSubmit, submit, resetSelection } =
  useCommunicationComposer(normalizedDigits, ref('call'), originalModemID)

const canDial = computed(() => canSubmit.value && isCallableDialString(normalizedDigits.value))

watch(desktop, () => {
  dialogOpen.value = false
})

const open = async (number?: string, modemID = '') => {
  originalModemID.value = modemID
  resetSelection()
  if (number !== undefined) setDigits(number)
  if (desktop.value) {
    desktopRef.value?.focus()
  } else {
    dialogOpen.value = true
    await nextTick()
    mobileRef.value?.focus()
  }
}
const startDial = async () => {
  if (!canDial.value || !(await submit())) return
  setDigits('')
  dialogOpen.value = false
  emit('dialed')
}

defineExpose({ open })
</script>

<template>
  <aside class="scrollbar-none hidden min-h-0 lg:block lg:h-full lg:overflow-y-auto">
    <div class="rounded-xl border bg-card/60 p-4 shadow-sm">
      <ModemDialpad
        ref="desktopRef"
        :digits="digits"
        :keys="keys"
        :input-class="dialInputClass"
        :can-dial="canDial"
        :is-dialing="busy"
        density="compact"
        show-header
        @update:digits="setDigits"
        @backspace="backspace"
        @append-key="appendKey"
        @start-plus="startPlusLongPress"
        @clear-plus="clearPlusLongPress"
        @dial="startDial"
      >
        <template #description>{{ t('communication.currentModem') }}</template>
        <template #line>
          <CommunicationModemSelect
            v-model="selectedID"
            :modems="modems"
            :disabled="loading || busy"
            :unavailable="unavailable"
          />
          <p
            v-if="error"
            role="alert"
            class="text-sm text-destructive"
          >
            {{ error }}
          </p>
        </template>
      </ModemDialpad>
    </div>
  </aside>
  <DraggableFab
    class="lg:hidden"
    :ariaLabel="t('modemDetail.calls.openDialpad')"
    @click="open()"
    ><Phone class="size-5"
  /></DraggableFab>
  <Dialog v-model:open="dialogOpen">
    <DialogContent class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-sm">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.calls.dialpad') }}</DialogTitle>
        <DialogDescription>{{ t('communication.currentModem') }}</DialogDescription>
      </DialogHeader>
      <ModemDialpad
        ref="mobileRef"
        :digits="digits"
        :keys="keys"
        :input-class="dialInputClass"
        :can-dial="canDial"
        :is-dialing="busy"
        @update:digits="setDigits"
        @backspace="backspace"
        @append-key="appendKey"
        @start-plus="startPlusLongPress"
        @clear-plus="clearPlusLongPress"
        @dial="startDial"
      >
        <template #line>
          <CommunicationModemSelect
            v-model="selectedID"
            :modems="modems"
            :disabled="loading || busy"
            :unavailable="unavailable"
          />
          <p
            v-if="error"
            role="alert"
            class="text-sm text-destructive"
          >
            {{ error }}
          </p>
        </template>
      </ModemDialpad>
    </DialogContent>
  </Dialog>
</template>
