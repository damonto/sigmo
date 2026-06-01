<script setup lang="ts">
import { Keyboard, Mic, Pause, PhoneCall, PhoneIncoming, PhoneOff, Play } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'

import { Button } from '@/components/ui/button'
import type { ModemCallSession } from '@/composables/useModemCallSession'

const props = defineProps<{
  session: ModemCallSession
}>()

const dtmfKeys = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '*', '0', '#']
const dtmfOpen = ref(false)

const dtmfCall = computed(() => props.session.activeCall.value)
const dtmfAvailable = computed(() => {
  const call = dtmfCall.value
  return (
    !!call &&
    call.route === 'wifi_calling' &&
    (call.state === 'active' || call.state === 'confirmed') &&
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
    class="fixed inset-x-0 top-3 z-40 mx-auto flex w-[calc(100%-1.5rem)] max-w-2xl items-center justify-between gap-3 rounded-xl border bg-background/95 px-4 py-3 shadow-lg backdrop-blur"
  >
    <div class="flex min-w-0 items-center gap-3">
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
      </div>
    </div>
    <div class="flex shrink-0 items-center gap-2">
      <Button
        size="icon"
        variant="destructive"
        :aria-label="$t('modemDetail.phone.reject')"
        @click="props.session.reject(props.session.incomingCall.value)"
      >
        <PhoneOff class="size-4" />
      </Button>
      <Button
        size="icon"
        class="bg-emerald-600 hover:bg-emerald-700"
        :aria-label="$t('modemDetail.phone.answer')"
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
    <div class="flex items-center justify-between gap-3">
      <div class="flex min-w-0 items-center gap-3">
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
            v-if="props.session.activeCallDurationLabel.value"
            class="truncate text-xs font-medium text-foreground"
          >
            {{ $t('modemDetail.phone.duration') }} ·
            {{ props.session.activeCallDurationLabel.value }}
          </p>
          <p v-if="props.session.audioMessage.value" class="truncate text-xs text-destructive">
            {{ props.session.audioMessage.value }}
          </p>
        </div>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <Button
          v-if="dtmfAvailable"
          size="icon"
          variant="outline"
          :aria-pressed="dtmfOpen"
          :aria-label="
            dtmfOpen
              ? $t('modemDetail.phone.closeInCallDialpad')
              : $t('modemDetail.phone.openInCallDialpad')
          "
          @click="dtmfOpen = !dtmfOpen"
        >
          <Keyboard class="size-4" />
        </Button>
        <Button
          v-if="props.session.activeCall.value.route === 'wifi_calling'"
          size="icon"
          variant="outline"
          :aria-label="
            props.session.isLocallyHeld(props.session.activeCall.value)
              ? $t('modemDetail.phone.resume')
              : $t('modemDetail.phone.hold')
          "
          @click="props.session.toggleHold(props.session.activeCall.value)"
        >
          <Play v-if="props.session.isLocallyHeld(props.session.activeCall.value)" class="size-4" />
          <Pause v-else class="size-4" />
        </Button>
        <Button
          size="icon"
          variant="destructive"
          :aria-label="$t('modemDetail.phone.hangup')"
          @click="props.session.hangup(props.session.activeCall.value)"
        >
          <PhoneOff class="size-4" />
        </Button>
      </div>
    </div>

    <div v-if="dtmfOpen && dtmfAvailable" class="grid grid-cols-3 gap-2 border-t pt-3">
      <button
        v-for="key in dtmfKeys"
        :key="key"
        type="button"
        class="flex h-12 touch-manipulation items-center justify-center rounded-full bg-muted text-base font-semibold transition hover:bg-muted/70 active:scale-95"
        :aria-label="$t('modemDetail.phone.sendDtmf', { digit: key })"
        @click="sendDTMF(key)"
      >
        {{ key }}
      </button>
    </div>
  </div>
</template>
