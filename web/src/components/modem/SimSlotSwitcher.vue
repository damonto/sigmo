<script setup lang="ts">
import type { AcceptableValue } from 'reka-ui'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ModemSignalStatus from '@/components/modem/ModemSignalStatus.vue'
import RegionFlag from '@/components/RegionFlag.vue'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import type { SlotInfo } from '@/types/modem'

const props = defineProps<{
  slots: SlotInfo[]
  registrationState?: string
  signalQuality?: number
  accessTechnology?: string | null
  registeredOperatorName?: string | null
  wifiCallingConnected?: boolean
  airplaneMode?: boolean
  onSwitch?: (slot: SlotInfo) => Promise<void>
}>()

const selectedSlot = defineModel<string>({ required: true })

const { t } = useI18n()

const pendingSlotNumber = ref<number | null>(null)
const dialogOpen = ref(false)
const isSwitching = ref(false)

const hasMultipleSlots = computed(() => props.slots.length > 1)
const showSignalStatus = computed(
  () => props.registrationState !== undefined && props.signalQuality !== undefined,
)

const slotValue = (slot: SlotInfo) => String(slot.slot)

const openDialog = (slot: SlotInfo) => {
  if (!hasMultipleSlots.value) return
  if (slotValue(slot) === selectedSlot.value) return
  pendingSlotNumber.value = slot.slot
  dialogOpen.value = true
}

const handleSelect = (payload: AcceptableValue) => {
  if (!hasMultipleSlots.value) return
  if (typeof payload !== 'string') return
  if (payload === selectedSlot.value) return
  const slot = props.slots.find((item) => slotValue(item) === payload)
  if (!slot) return
  openDialog(slot)
}

const closeDialog = () => {
  pendingSlotNumber.value = null
  dialogOpen.value = false
  isSwitching.value = false
}

const confirmSwitch = async () => {
  if (!pendingSlot.value) return
  if (isSwitching.value) return
  isSwitching.value = true
  try {
    if (props.onSwitch) {
      await props.onSwitch(pendingSlot.value)
    } else {
      selectedSlot.value = slotValue(pendingSlot.value)
    }
    closeDialog()
  } catch (err) {
    console.error('[SimSlotSwitcher] Failed to switch SIM slot:', err)
    closeDialog()
  } finally {
    isSwitching.value = false
  }
}

const getSlotLabel = (slot: SlotInfo) => {
  return `SIM ${slot.slot}`
}

const pendingSlot = computed(() => {
  if (pendingSlotNumber.value === null) return null
  return props.slots.find((slot) => slot.slot === pendingSlotNumber.value) ?? null
})

const pendingOperatorName = computed(() => pendingSlot.value?.operatorName ?? '')
const pendingIdentifierValue = computed(() => pendingSlot.value?.identifier ?? '')
const pendingRegionCode = computed(() => pendingSlot.value?.regionCode ?? '')

const confirmTitle = computed(() => {
  return t('modemDetail.sim.confirm', { sim: pendingSlot.value?.slot ?? '' })
})

const slotOptionClass = (slot: SlotInfo) => {
  if (!hasMultipleSlots.value) {
    return 'cursor-default text-muted-foreground'
  }
  if (slotValue(slot) === selectedSlot.value) {
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
      :model-value="selectedSlot"
      :disabled="!hasMultipleSlots"
      class="inline-flex min-w-0 shrink-0 items-center gap-1 overflow-x-auto"
      @update:model-value="handleSelect"
    >
      <div v-for="slot in slots" :key="slot.slot" class="relative flex items-center">
        <Label
          :for="`sim-slot-${slot.slot}`"
          class="inline-flex h-7 select-none items-center gap-2 rounded-md px-2 text-xs font-semibold uppercase transition-colors"
          :class="slotOptionClass(slot)"
        >
          <RadioGroupItem
            :id="`sim-slot-${slot.slot}`"
            :value="slotValue(slot)"
            class="size-3.5"
          />
          {{ getSlotLabel(slot) }}
        </Label>
      </div>
    </RadioGroup>

    <ModemSignalStatus
      v-if="showSignalStatus"
      :signal-quality="props.signalQuality ?? 0"
      :registration-state="props.registrationState ?? ''"
      :access-technology="props.accessTechnology"
      :registered-operator-name="props.registeredOperatorName"
      :wifi-calling-connected="props.wifiCallingConnected"
      :airplane-mode="props.airplaneMode"
      :show-signal-value="false"
      size="sm"
      class="ml-auto"
    />
  </div>

  <AlertDialog v-model:open="dialogOpen">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ confirmTitle }}</AlertDialogTitle>
        <AlertDialogDescription class="sr-only">
          {{ confirmTitle }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <div v-if="pendingSlot" class="flex min-w-0 items-center gap-2.5">
        <div
          class="flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30"
        >
          <RegionFlag :region-code="pendingRegionCode" class="rounded-sm text-base" />
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
