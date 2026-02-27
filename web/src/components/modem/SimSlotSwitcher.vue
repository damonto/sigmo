<script setup lang="ts">
import type { AcceptableValue } from 'reka-ui'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import { useModemDisplay } from '@/composables/useModemDisplay'
import type { SlotInfo } from '@/types/modem'

const props = defineProps<{
  slots: SlotInfo[]
  onSwitch?: (identifier: string) => Promise<void>
}>()

const selectedIdentifier = defineModel<string>({ required: true })

const { t } = useI18n()
const { flagClass } = useModemDisplay()

const pendingIdentifier = ref<string | null>(null)
const dialogOpen = ref(false)
const isSwitching = ref(false)

const hasMultipleSlots = computed(() => props.slots.length > 1)

const openDialog = (identifier: string) => {
  if (identifier === selectedIdentifier.value) return
  pendingIdentifier.value = identifier
  dialogOpen.value = true
}

const handleSelect = (payload: AcceptableValue) => {
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

const getSlotLabel = (slot: SlotInfo, index: number) => {
  return slot.active ? 'Active' : `SIM ${index + 1}`
}

const pendingSlot = computed(() => {
  if (!pendingIdentifier.value) return null
  return props.slots.find((slot) => slot.identifier === pendingIdentifier.value)
})

const pendingSlotIndex = computed(() => {
  if (!pendingIdentifier.value) return -1
  return props.slots.findIndex((slot) => slot.identifier === pendingIdentifier.value)
})

const pendingSimLabel = computed(() => {
  const index = pendingSlotIndex.value
  if (index < 0) return 'SIM'
  return `SIM ${index + 1}`
})

const pendingOperatorName = computed(() => pendingSlot.value?.operatorName || pendingSimLabel.value)
const pendingIdentifierValue = computed(() => pendingSlot.value?.identifier ?? '')
const pendingRegionCode = computed(() => pendingSlot.value?.regionCode ?? '')
const pendingRegionFlagClass = computed(() => flagClass(pendingRegionCode.value))

const confirmTitle = computed(() => {
  return `请确认是否切换至 "${pendingSimLabel.value}"`
})
</script>

<template>
  <div v-if="hasMultipleSlots && slots.length > 0">
    <!-- SIM Slot Switcher -->
    <RadioGroup
      :model-value="selectedIdentifier"
      class="flex flex-wrap gap-4"
      @update:model-value="handleSelect"
    >
      <div v-for="(slot, index) in slots" :key="slot.identifier" class="flex items-center gap-2">
        <RadioGroupItem :id="`sim-slot-${slot.identifier}`" :value="slot.identifier" />
        <Label
          :for="`sim-slot-${slot.identifier}`"
          class="text-[10px] font-semibold uppercase tracking-[0.16em]"
        >
          {{ getSlotLabel(slot, index) }}
        </Label>
      </div>
    </RadioGroup>

    <!-- Confirmation Dialog -->
    <AlertDialog v-model:open="dialogOpen">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{{ confirmTitle }}</AlertDialogTitle>
        </AlertDialogHeader>
        <Card v-if="pendingSlot" class="border-0 shadow-sm">
          <CardContent class="flex items-center gap-3 p-3">
            <div
              class="flex size-12 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30"
            >
              <span class="rounded-sm text-[18px]">
                <span v-if="pendingRegionFlagClass" :class="pendingRegionFlagClass" />
                <span v-else class="text-xs font-semibold text-muted-foreground">
                  {{ pendingRegionCode }}
                </span>
              </span>
            </div>
            <div class="min-w-0">
              <p class="truncate text-sm font-semibold text-foreground">
                {{ pendingOperatorName }}
              </p>
              <p class="truncate text-xs text-muted-foreground">{{ pendingIdentifierValue }}</p>
            </div>
          </CardContent>
        </Card>
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
  </div>
</template>
