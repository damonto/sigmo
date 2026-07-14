<script setup lang="ts">
import { Mic, Volume2 } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { ModemCallSession } from '@/composables/useModemCallSession'
import type { CallRecord } from '@/types/call'

const props = defineProps<{
  call: CallRecord
  session: ModemCallSession
}>()

const { t } = useI18n()

const defaultDeviceValue = 'default'

const selectedInputValue = computed(
  () => props.session.callAudio.selectedInputDeviceID.value || defaultDeviceValue,
)
const selectedOutputValue = computed(
  () => props.session.callAudio.selectedOutputDeviceID.value || defaultDeviceValue,
)

const inputDeviceLabel = (deviceID: string, label: string, index: number) => {
  if (!deviceID) return t('modemDetail.phone.audioDevices.systemDefault')
  return label || t('modemDetail.phone.audioDevices.microphoneFallback', { index: index + 1 })
}

const outputDeviceLabel = (deviceID: string, label: string, index: number) => {
  if (!deviceID) return t('modemDetail.phone.audioDevices.systemDefault')
  return label || t('modemDetail.phone.audioDevices.outputFallback', { index: index + 1 })
}

const selectedInputLabel = computed(() => {
  const deviceID = props.session.callAudio.selectedInputDeviceID.value
  const index = props.session.callAudio.inputDevices.value.findIndex(
    (device) => device.deviceId === deviceID,
  )
  const device = props.session.callAudio.inputDevices.value[index]
  return inputDeviceLabel(deviceID, device?.label ?? '', Math.max(index, 0))
})

const selectedOutputLabel = computed(() => {
  const deviceID = props.session.callAudio.selectedOutputDeviceID.value
  const index = props.session.callAudio.outputDevices.value.findIndex(
    (device) => device.deviceId === deviceID,
  )
  const device = props.session.callAudio.outputDevices.value[index]
  return outputDeviceLabel(deviceID, device?.label ?? '', Math.max(index, 0))
})

const inputItemDisabled = computed(
  () =>
    props.session.callAudio.isSwitchingInput.value ||
    props.session.callAudio.isRefreshingDevices.value ||
    props.session.callAudio.mediaStatus.value === 'preparing' ||
    props.session.callAudio.mediaStatus.value === 'connecting',
)
const inputButtonDisabled = computed(
  () =>
    props.session.callAudio.isSwitchingInput.value ||
    props.session.callAudio.mediaStatus.value === 'connecting',
)

const handleInputOpen = (open: boolean) => {
  if (!open) return
  void props.session.prepareAudioDevices(props.call)
}

const handleOutputOpen = (open: boolean) => {
  if (!open) return
  void props.session.callAudio.refreshDevices()
}

const selectInput = (deviceID: unknown) => {
  if (typeof deviceID !== 'string') return
  void props.session.callAudio.selectInputDevice(deviceID)
}

const selectOutput = (deviceID: unknown) => {
  if (typeof deviceID !== 'string') return
  void props.session.callAudio.selectOutputDevice(deviceID)
}
</script>

<template>
  <DropdownMenu @update:open="handleInputOpen">
    <DropdownMenuTrigger as-child>
      <Button
        size="icon"
        variant="outline"
        :disabled="inputButtonDisabled"
        :aria-label="
          props.session.callAudio.mediaStatus.value === 'preparing'
            ? t('modemDetail.phone.audioDevices.preparing')
            : t('modemDetail.phone.audioDevices.selectMicrophone', { device: selectedInputLabel })
        "
      >
        <Mic class="size-4" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="w-64">
      <DropdownMenuLabel>
        {{ t('modemDetail.phone.audioDevices.microphone') }}
      </DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuRadioGroup :model-value="selectedInputValue" @update:model-value="selectInput">
        <DropdownMenuRadioItem :value="defaultDeviceValue" :disabled="inputItemDisabled">
          {{ t('modemDetail.phone.audioDevices.systemDefault') }}
        </DropdownMenuRadioItem>
        <DropdownMenuRadioItem
          v-for="(device, index) in props.session.callAudio.inputDevices.value"
          :key="device.deviceId"
          :value="device.deviceId"
          :disabled="inputItemDisabled"
          :title="inputDeviceLabel(device.deviceId, device.label, index)"
        >
          <span class="truncate">
            {{ inputDeviceLabel(device.deviceId, device.label, index) }}
          </span>
        </DropdownMenuRadioItem>
      </DropdownMenuRadioGroup>
    </DropdownMenuContent>
  </DropdownMenu>

  <DropdownMenu @update:open="handleOutputOpen">
    <DropdownMenuTrigger as-child>
      <Button
        size="icon"
        variant="outline"
        :disabled="
          !props.session.callAudio.outputSelectionSupported.value ||
          props.session.callAudio.isSwitchingOutput.value
        "
        :aria-label="
          props.session.callAudio.outputSelectionSupported.value
            ? t('modemDetail.phone.audioDevices.selectOutput', { device: selectedOutputLabel })
            : t('modemDetail.phone.audioDevices.outputUnsupported')
        "
        :title="
          props.session.callAudio.outputSelectionSupported.value
            ? undefined
            : t('modemDetail.phone.audioDevices.outputUnsupported')
        "
      >
        <Volume2 class="size-4" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="w-64">
      <DropdownMenuLabel>
        {{ t('modemDetail.phone.audioDevices.output') }}
      </DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuRadioGroup :model-value="selectedOutputValue" @update:model-value="selectOutput">
        <DropdownMenuRadioItem
          :value="defaultDeviceValue"
          :disabled="props.session.callAudio.isSwitchingOutput.value"
        >
          {{ t('modemDetail.phone.audioDevices.systemDefault') }}
        </DropdownMenuRadioItem>
        <DropdownMenuRadioItem
          v-for="(device, index) in props.session.callAudio.outputDevices.value"
          :key="device.deviceId"
          :value="device.deviceId"
          :disabled="props.session.callAudio.isSwitchingOutput.value"
          :title="outputDeviceLabel(device.deviceId, device.label, index)"
        >
          <span class="truncate">
            {{ outputDeviceLabel(device.deviceId, device.label, index) }}
          </span>
        </DropdownMenuRadioItem>
      </DropdownMenuRadioGroup>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
