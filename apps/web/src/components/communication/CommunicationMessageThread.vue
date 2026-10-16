<script setup lang="ts">
import { ref, useTemplateRef, watch } from 'vue'
import { MessageSquare, Phone, UserPlus } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRouter } from 'vue-router'

import CommunicationComposer from '@/components/communication/CommunicationComposer.vue'
import HistoryMessage from '@/components/communication/HistoryMessage.vue'
import MessageThreadSkeleton from '@/components/communication/MessageThreadSkeleton.vue'
import ModemMessageThreadHeader from '@/components/modem/messages/ModemMessageThreadHeader.vue'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { useCommunicationHistory } from '@/composables/useCommunicationHistory'

const { t } = useI18n()
const router = useRouter()
const {
  loading,
  error,
  cursor,
  messages,
  threadID,
  participant,
  participantLabel,
  originalModemID,
  supported,
  load,
  deleteCurrent,
  acknowledge,
  match,
  lineDeleted,
  sourceLabel,
} = useCommunicationHistory('thread')
const action = ref<'message' | 'call'>()
const deleting = ref(false)
const messagesContainer = useTemplateRef<HTMLElement>('messagesContainer')
const atBottom = ref(true)
const updateScrollPosition = () => {
  const element = messagesContainer.value
  if (element) atBottom.value = element.scrollHeight - element.scrollTop - element.clientHeight < 80
}
watch(
  () => messages.value[messages.value.length - 1]?.id,
  (id, previous) => {
    const element = messagesContainer.value
    if (element && id !== undefined && (previous === undefined || atBottom.value)) {
      element.scrollTop = element.scrollHeight
    }
  },
  { flush: 'post' },
)
const remove = async () => {
  if (await deleteCurrent(threadID.value)) {
    await router.replace({ name: 'messages' })
    window.dispatchEvent(new Event('sigmo:communication-changed'))
  }
  deleting.value = false
}
</script>

<template>
  <section
    class="flex min-h-0 min-w-0 flex-col gap-3 overflow-hidden bg-background lg:rounded-xl lg:border lg:bg-card/40 lg:p-4"
  >
    <ModemMessageThreadHeader
      :title="match(participant)?.name || participantLabel || t('communication.messages')"
      :can-delete="supported && !!participant"
      @back="router.push({ name: 'messages' })"
      @delete="deleting = true"
    >
      <template
        v-if="supported"
        #actions="{ actionComponent, activateEvent }"
      >
        <component
          :is="actionComponent"
          :disabled="!participant"
          @[activateEvent]="action = 'call'"
        >
          <Phone class="size-4" />{{ t('communication.dial') }}
        </component>
        <component
          :is="actionComponent"
          v-if="participant"
          as-child
        >
          <RouterLink :to="{ name: 'contacts', query: { number: participant } }">
            <UserPlus class="size-4" />{{ t('communication.addContact') }}
          </RouterLink>
        </component>
      </template>
    </ModemMessageThreadHeader>
    <p
      v-if="error"
      role="alert"
      class="text-sm text-destructive"
    >
      {{ error }}
    </p>
    <div
      ref="messagesContainer"
      class="scrollbar-none min-h-0 flex-1 space-y-3 overflow-y-auto py-3"
      :aria-busy="loading"
      @scroll="updateScrollPosition"
    >
      <MessageThreadSkeleton v-if="loading && !messages.length" />
      <p
        v-else-if="!supported"
        class="text-sm text-muted-foreground"
      >
        {{ t('communication.upgrade') }}
      </p>
      <template v-else>
        <Button
          v-if="cursor"
          variant="outline"
          class="w-full"
          :disabled="loading"
          @click="load(true)"
          >{{ t('communication.older') }}</Button
        >
        <MessageThreadSkeleton
          v-if="loading && cursor"
          :count="2"
        />
        <HistoryMessage
          v-for="message in messages"
          :key="message.id"
          :message="message"
          :source="sourceLabel(message.profileId, message.lineLabel)"
          :deleted="lineDeleted(message.profileId)"
          @visible="acknowledge"
        />
      </template>
    </div>
    <div
      v-if="supported"
      class="border-t pt-3"
    >
      <Button
        class="w-full gap-2"
        :disabled="!participant"
        @click="action = 'message'"
        ><MessageSquare class="size-4" />{{ t('communication.newMessage') }}</Button
      >
    </div>
    <CommunicationComposer
      v-if="action"
      :participant="participant"
      :modem-id="originalModemID"
      :mode="action"
      @close="action = undefined"
      @sent="load()"
    />
    <Dialog v-model:open="deleting">
      <DialogContent>
        <DialogHeader
          ><DialogTitle>{{ t('communication.delete') }}</DialogTitle
          ><DialogDescription>{{
            t('communication.deleteThreadDescription')
          }}</DialogDescription></DialogHeader
        >
        <Button
          variant="destructive"
          @click="remove"
          >{{ t('communication.delete') }}</Button
        >
      </DialogContent>
    </Dialog>
  </section>
</template>
