<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  Circle,
  Keyboard,
  Mic,
  Pause,
  PhoneCall,
  PhoneIncoming,
  PhoneOff,
  Play,
  Square,
} from 'lucide-vue-next'

import ContactAvatar from '@/components/contacts/ContactAvatar.vue'
import { useContacts } from '@/composables/useContacts'
import ModemCallAudioDevices from '@/components/modem/ModemCallAudioDevices.vue'
import ModemCallRecordingNotice from '@/components/modem/ModemCallRecordingNotice.vue'
import { Button } from '@/components/ui/button'
import type { ModemCallSession } from '@/composables/useModemCallSession'
import type { CallRecord } from '@/types/call'
import { ownsCall } from '@/lib/callClient'

const props = defineProps<{
  session: ModemCallSession
}>()

const { match } = useContacts()
const contact = computed(() =>
  match((props.session.incomingCall.value ?? props.session.activeCall.value)?.number ?? ''),
)

const dtmfKeys = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '*', '0', '#']
const imsCallRoutes = new Set<CallRecord['route']>(['wifi_calling', 'volte'])
const dtmfCallStates = new Set<CallRecord['state']>(['early_media', 'active', 'confirmed'])
const dtmfOpen = ref(false)
const recording = computed(() => props.session.callRecording)
const recordingErrorKey = computed(() =>
  recording.value.error.value ? `modemDetail.calls.recording.${recording.value.error.value}` : '',
)
const recordingActionKey = computed(() =>
  recording.value.isRecording.value
    ? 'modemDetail.calls.recording.stop'
    : 'modemDetail.calls.recording.start',
)
const recordingDisabled = computed(
  () =>
    recording.value.isBusy.value ||
    (!recording.value.canStart.value && !recording.value.isRecording.value),
)

const toggleRecording = () => {
  if (recording.value.isRecording.value) {
    recording.value.stop()
    return
  }
  void recording.value.start()
}

const dtmfCall = computed(() => props.session.activeCall.value)
const viewingCall = computed(() => !!dtmfCall.value && !ownsCall(dtmfCall.value))
const isIMSCall = computed(() => {
  const call = dtmfCall.value
  return !!call && imsCallRoutes.has(call.route)
})
const dtmfAvailable = computed(() => {
  const call = dtmfCall.value
  return (
    !!call &&
    !viewingCall.value &&
    isIMSCall.value &&
    dtmfCallStates.has(call.state) &&
    !props.session.isLocallyHeld(call) &&
    !props.session.isRemotelyHeld(call)
  )
})

watch(dtmfAvailable, (available) => {
  if (!available) {
    dtmfOpen.value = false
  }
})

const sendDTMF = (digit: string) => {
  const call = dtmfCall.value
  if (!call || !dtmfAvailable.value) return
  void props.session.sendDTMF(call, digit)
}
</script>

<template>
  <div
    v-if="props.session.incomingCall.value"
    class="fixed inset-x-0 top-3 z-40 mx-auto flex w-[calc(100%-1.5rem)] max-w-2xl flex-col items-stretch justify-between gap-3 rounded-xl border bg-background/95 px-4 py-3 shadow-lg backdrop-blur sm:flex-row sm:items-center"
  >
    <div class="flex min-w-0 items-center gap-3">
      <ContactAvatar
        v-if="contact"
        :id="contact.id"
        :name="contact.name"
        :has-avatar="contact.hasAvatar"
        :updated-at="contact.updatedAt"
      />
      <span
        class="grid size-10 shrink-0 place-items-center rounded-full bg-emerald-100 text-emerald-700"
      >
        <PhoneIncoming class="size-5" />
      </span>
      <div class="min-w-0">
        <p class="truncate text-sm font-semibold">
          {{ props.session.primaryLine(props.session.incomingCall.value) }}
        </p>
        <p class="text-xs text-muted-foreground">
          {{ props.session.routeLabel(props.session.incomingCall.value.route) }}
        </p>
        <p
          v-if="props.session.audioMessage.value"
          class="truncate text-xs text-destructive"
        >
          {{ props.session.audioMessage.value }}
        </p>
      </div>
    </div>
    <div
      class="flex w-full shrink-0 items-center justify-end gap-2 border-t pt-3 sm:w-auto sm:border-0 sm:pt-0"
    >
      <ModemCallAudioDevices
        v-if="props.session.usesBrowserAudio(props.session.incomingCall.value)"
        :call="props.session.incomingCall.value"
        :session="props.session"
      />
      <Button
        size="icon"
        variant="destructive"
        :aria-label="$t('modemDetail.calls.reject')"
        @click="props.session.reject(props.session.incomingCall.value)"
      >
        <PhoneOff class="size-4" />
      </Button>
      <Button
        size="icon"
        class="bg-emerald-600 hover:bg-emerald-700"
        :aria-label="$t('modemDetail.calls.answer')"
        @click="props.session.answerIncoming(props.session.incomingCall.value)"
      >
        <PhoneCall class="size-4" />
      </Button>
    </div>
  </div>

  <div
    v-else-if="
      props.session.activeCall.value &&
      !props.session.terminalStates.has(props.session.activeCall.value.state)
    "
    class="fixed inset-x-0 top-3 z-40 mx-auto flex w-[calc(100%-1.5rem)] max-w-2xl flex-col items-stretch gap-3 rounded-xl border bg-background/95 px-4 py-3 shadow-lg backdrop-blur"
  >
    <div class="flex flex-col items-stretch justify-between gap-3 sm:flex-row sm:items-center">
      <div class="flex min-w-0 items-center gap-3">
        <ContactAvatar
          v-if="contact"
          :id="contact.id"
          :name="contact.name"
          :has-avatar="contact.hasAvatar"
          :updated-at="contact.updatedAt"
        />
        <span
          class="grid size-10 shrink-0 place-items-center rounded-full bg-primary/10 text-primary"
        >
          <Mic class="size-5" />
        </span>
        <div class="min-w-0">
          <p class="truncate text-sm font-semibold">
            {{ props.session.primaryLine(props.session.activeCall.value) }}
          </p>
          <p class="truncate text-xs text-muted-foreground">
            {{ props.session.stateLabel(props.session.activeCall.value.state) }} ·
            {{ props.session.routeLabel(props.session.activeCall.value.route) }}
            <template v-if="props.session.holdLabel(props.session.activeCall.value.hold)">
              · {{ props.session.holdLabel(props.session.activeCall.value.hold) }}
            </template>
          </p>
          <p
            v-if="viewingCall"
            class="text-xs text-muted-foreground"
            role="status"
          >
            {{ $t('modemDetail.calls.inUseElsewhere') }}
          </p>
          <p
            v-if="props.session.activeCallDurationLabel.value"
            class="truncate text-xs font-medium text-foreground"
          >
            {{ $t('modemDetail.calls.duration') }} ·
            {{ props.session.activeCallDurationLabel.value }}
          </p>
          <p
            v-if="props.session.audioMessage.value"
            class="truncate text-xs text-destructive"
          >
            {{ props.session.audioMessage.value }}
          </p>
          <p
            v-if="recording.isRecording.value"
            class="flex items-center gap-1.5 text-xs text-destructive"
          >
            <span
              class="size-2 rounded-full bg-destructive"
              aria-hidden="true"
            />
            {{ $t('modemDetail.calls.recording.active') }} · {{ recording.durationLabel.value }}
          </p>
          <p
            v-else-if="recording.isBusy.value"
            class="text-xs text-muted-foreground"
            role="status"
          >
            {{ $t(`modemDetail.calls.recording.${recording.status.value}`) }}
          </p>
          <p
            v-if="isIMSCall && !recording.isSupported.value"
            class="text-xs text-muted-foreground"
          >
            {{ $t('modemDetail.calls.recording.unsupported') }}
          </p>
          <p
            v-else-if="isIMSCall && !viewingCall && !props.session.callAudio.localStream.value"
            class="text-xs text-muted-foreground"
          >
            {{ $t('modemDetail.calls.recording.receiveOnly') }}
          </p>
        </div>
      </div>
      <div
        v-if="!viewingCall"
        class="flex w-full shrink-0 flex-wrap items-center justify-end gap-2 border-t pt-3 sm:w-auto sm:border-0 sm:pt-0"
      >
        <ModemCallAudioDevices
          v-if="props.session.usesBrowserAudio(props.session.activeCall.value)"
          :call="props.session.activeCall.value"
          :session="props.session"
        />
        <Button
          v-if="isIMSCall"
          size="icon"
          variant="outline"
          :disabled="recordingDisabled"
          :aria-pressed="recording.isRecording.value"
          :aria-label="$t(recordingActionKey)"
          :title="$t(recordingActionKey)"
          @click="toggleRecording"
        >
          <Square
            v-if="recording.isRecording.value"
            class="size-4 fill-destructive text-destructive"
          />
          <Circle
            v-else
            class="size-4 text-destructive"
          />
        </Button>
        <Button
          v-if="dtmfAvailable"
          size="icon"
          variant="outline"
          :aria-pressed="dtmfOpen"
          :aria-label="
            dtmfOpen
              ? $t('modemDetail.calls.closeInCallDialpad')
              : $t('modemDetail.calls.openInCallDialpad')
          "
          @click="dtmfOpen = !dtmfOpen"
        >
          <Keyboard class="size-4" />
        </Button>
        <Button
          v-if="isIMSCall"
          size="icon"
          variant="outline"
          :aria-label="
            props.session.isLocallyHeld(props.session.activeCall.value)
              ? $t('modemDetail.calls.resume')
              : $t('modemDetail.calls.hold')
          "
          @click="props.session.toggleHold(props.session.activeCall.value)"
        >
          <Play
            v-if="props.session.isLocallyHeld(props.session.activeCall.value)"
            class="size-4"
          />
          <Pause
            v-else
            class="size-4"
          />
        </Button>
        <Button
          size="icon"
          variant="destructive"
          :aria-label="$t('modemDetail.calls.hangup')"
          @click="props.session.hangup(props.session.activeCall.value)"
        >
          <PhoneOff class="size-4" />
        </Button>
      </div>
    </div>

    <div
      v-if="dtmfOpen && dtmfAvailable"
      class="grid grid-cols-3 gap-2 border-t pt-3"
    >
      <button
        v-for="key in dtmfKeys"
        :key="key"
        type="button"
        class="flex h-12 touch-manipulation items-center justify-center rounded-full bg-muted text-base font-semibold transition hover:bg-muted/70 active:scale-95"
        :aria-label="$t('modemDetail.calls.sendDtmf', { digit: key })"
        @click="sendDTMF(key)"
      >
        {{ key }}
      </button>
    </div>
  </div>

  <ModemCallRecordingNotice
    :filename="recording.filename.value"
    :error-message="recordingErrorKey ? $t(recordingErrorKey) : ''"
    @download="recording.download()"
    @dismiss="recording.dismiss()"
  />
</template>
