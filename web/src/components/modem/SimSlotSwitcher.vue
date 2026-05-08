<script setup lang="ts">
import type { AcceptableValue } from 'reka-ui'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ModemSignalStatus from '@/components/modem/ModemSignalStatus.vue'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import { useModemDisplay } from '@/composables/useModemDisplay'
import type { SlotInfo } from '@/types/modem'

const props = defineProps<{
  slots: SlotInfo[]
  registrationState?: string
  signalQuality?: number
  onSwitch?: (identifier: string) => Promise<void>
}>()

const selectedIdentifier = defineModel<string>({ required: true })

const { t } = useI18n()
const { flagClass } = useModemDisplay()

const pendingIdentifier = ref<string | null>(null)
const dialogOpen = ref(false)
const isSwitching = ref(false)

const hasMultipleSlots = computed(() => props.slots.length > 1)
const showSignalStatus = computed(
  () => props.registrationState !== undefined && props.signalQuality !== undefined,
)

const openDialog = (identifier: string) => {
  if (!hasMultipleSlots.value) return
  if (identifier === selectedIdentifier.value) return
  pendingIdentifier.value = identifier
  dialogOpen.value = true
}

const handleSelect = (payload: AcceptableValue) => {
  if (!hasMultipleSlots.value) return
  if (typeof payload !== 'string') return
  if (payload === selectedIdentifier.value) return
  openDialog(payload)
}

const closeDialog = () => {
  pendingIdentifier.value = null
  dialogOpen.value = false
  isSwitching.value = false
}

const confirmSwitch = async () => {
  if (!pendingIdentifier.value) return
  if (isSwitching.value) return
  isSwitching.value = true
  try {
    if (props.onSwitch) {
      await props.onSwitch(pendingIdentifier.value)
    } else {
      selectedIdentifier.value = pendingIdentifier.value
    }
    closeDialog()
  } catch (err) {
    console.error('[SimSlotSwitcher] Failed to switch SIM slot:', err)
    closeDialog()
  } finally {
    isSwitching.value = false
  }
}

const getSlotLabel = (index: number) => {
  return `SIM ${index + 1}`
}

const pendingSlot = computed(() => {
  if (!pendingIdentifier.value) return null
  return props.slots.find((slot) => slot.identifier === pendingIdentifier.value)
})

const pendingSlotIndex = computed(() => {
  if (!pendingIdentifier.value) return -1
  return props.slots.findIndex((slot) => slot.identifier === pendingIdentifier.value)
})

const pendingOperatorName = computed(() => pendingSlot.value?.operatorName ?? '')
const pendingIdentifierValue = computed(() => pendingSlot.value?.identifier ?? '')
const pendingRegionCode = computed(() => pendingSlot.value?.regionCode ?? '')
const pendingRegionFlagClass = computed(() => flagClass(pendingRegionCode.value))

const confirmTitle = computed(() => {
  return t('modemDetail.sim.confirm', { sim: pendingSlotIndex.value + 1 })
})

const slotOptionClass = (slot: SlotInfo) => {
  if (!hasMultipleSlots.value) {
    return 'cursor-default text-muted-foreground'
  }
  if (slot.identifier === selectedIdentifier.value) {
    return 'cursor-default text-primary'
  }
  return 'cursor-pointer text-muted-foreground hover:text-foreground'
}
</script>

<template>
  <div
    class="flex min-w-0 items-center gap-2 rounded-lg bg-card/90 px-3 py-2 shadow-sm backdrop-blur-xl dark:bg-card/70 dark:shadow-none"
  >
    <RadioGroup
      :model-value="selectedIdentifier"
      :disabled="!hasMultipleSlots"
      class="inline-flex min-w-0 shrink-0 items-center gap-1 overflow-x-auto"
      @update:model-value="handleSelect"
    >
      <div v-for="(slot, index) in slots" :key="slot.identifier" class="relative flex items-center">
        <Label
          :for="`sim-slot-${slot.identifier}`"
          class="inline-flex h-7 select-none items-center gap-2 rounded-md px-2 text-xs font-semibold uppercase transition-colors"
          :class="slotOptionClass(slot)"
        >
          <RadioGroupItem
            :id="`sim-slot-${slot.identifier}`"
            :value="slot.identifier"
            class="size-3.5"
          />
          {{ getSlotLabel(index) }}
        </Label>
      </div>
    </RadioGroup>

    <ModemSignalStatus
      v-if="showSignalStatus"
      :signal-quality="props.signalQuality ?? 0"
      :registration-state="props.registrationState ?? ''"
      size="sm"
      class="ml-auto"
    />
  </div>

  <AlertDialog v-model:open="dialogOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ confirmTitle }}</AlertDialogTitle>
      </AlertDialogHeader>
      <div v-if="pendingSlot" class="flex min-w-0 items-center gap-2.5">
        <div
          class="flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30"
        >
          <span class="rounded-sm text-base">
            <span v-if="pendingRegionFlagClass" :class="pendingRegionFlagClass" />
            <span v-else class="text-xs font-semibold text-muted-foreground">
              {{ pendingRegionCode }}
            </span>
          </span>
        </div>
        <div class="min-w-0">
          <p class="truncate text-sm font-semibold leading-tight text-foreground">
            {{ pendingOperatorName }}
          </p>
          <p class="truncate text-xs leading-tight text-muted-foreground">
            {{ pendingIdentifierValue }}
          </p>
        </div>
      </div>
      <AlertDialogFooter>
        <AlertDialogCancel @click="closeDialog" :disabled="isSwitching">
          {{ t('modemDetail.actions.cancel') }}
        </AlertDialogCancel>
        <Button type="button" @click="confirmSwitch" :disabled="isSwitching">
          <span v-if="isSwitching" class="inline-flex items-center gap-2">
            <Spinner class="size-4" />
            {{ t('modemDetail.actions.confirm') }}
          </span>
          <span v-else>{{ t('modemDetail.actions.confirm') }}</span>
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
