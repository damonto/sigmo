<script setup lang="ts">
import { ref } from 'vue'
import { Plus } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import CommunicationComposer from '@/components/communication/CommunicationComposer.vue'
import CommunicationPageHeader from '@/components/communication/CommunicationPageHeader.vue'
import CommunicationHistoryFilter from '@/components/communication/CommunicationHistoryFilter.vue'
import HistoryAssignments from '@/components/communication/HistoryAssignments.vue'
import MessageConversationRow from '@/components/communication/MessageConversationRow.vue'
import MessageListSkeleton from '@/components/communication/MessageListSkeleton.vue'
import ContactAvatar from '@/components/contacts/ContactAvatar.vue'
import ModemMessagesFab from '@/components/modem/messages/ModemMessagesFab.vue'
import ModemMessagesSearch from '@/components/modem/messages/ModemMessagesSearch.vue'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { useCommunicationHistory } from '@/composables/useCommunicationHistory'
import { formatListTimestamp } from '@/lib/datetime'

const props = defineProps<{ selectedThread?: string }>()
const { t } = useI18n()
const router = useRouter()
const {
  search,
  loading,
  error,
  cursor,
  visibleThreads: threads,
  unreadOnly,
  supported,
  load,
  deleteCurrent,
  match,
  sourceLabel,
} = useCommunicationHistory('messages')
const composing = ref(false)
const deleting = ref('')
const remove = async () => {
  const id = deleting.value
  deleting.value = ''
  if (await deleteCurrent(id)) {
    if (id === props.selectedThread) await router.replace({ name: 'messages' })
  }
}
</script>

<template>
  <section
    class="flex min-w-0 flex-col gap-4 lg:h-full lg:min-h-0"
    :aria-label="t('communication.messages')"
  >
    <CommunicationPageHeader
      :title="t('communication.messages')"
      :subtitle="t('communication.messagesDescription')"
    />
    <Button
      v-if="supported"
      class="hidden w-full gap-2 lg:inline-flex"
      @click="composing = true"
    >
      <Plus class="size-4" />{{ t('modemDetail.messages.newConversation') }}
    </Button>
    <ModemMessagesSearch v-model="search" />
    <CommunicationHistoryFilter
      v-if="supported"
      v-model="unreadOnly"
      :label="t('communication.filters')"
      :all-label="t('modemDetail.messages.all')"
      :filtered-label="t('modemDetail.messages.unread')"
    />
    <p
      v-if="error"
      role="alert"
      class="text-sm text-destructive"
    >
      {{ error }}
    </p>
    <HistoryAssignments
      v-if="supported"
      @changed="load()"
    />
    <div
      class="scrollbar-none space-y-3 lg:min-h-0 lg:flex-1 lg:overflow-y-auto"
      :aria-busy="loading"
    >
      <MessageListSkeleton v-if="loading && !threads.length" />
      <p
        v-else-if="!supported"
        class="rounded-lg border border-dashed p-4 text-sm text-muted-foreground"
      >
        {{ t('communication.upgrade') }}
      </p>
      <template v-else>
        <p
          v-if="!threads.length && !error"
          class="rounded-lg border border-dashed p-4 text-sm text-muted-foreground"
        >
          {{ t('communication.emptyMessages') }}
        </p>
        <MessageConversationRow
          v-for="thread in threads"
          :key="thread.id"
          :to="{ name: 'message-thread', params: { threadId: thread.id } }"
          :name="match(thread.participant)?.name || thread.participant"
          :preview="thread.latestMessage.text"
          :line="sourceLabel(thread.latestMessage.profileId, thread.latestMessage.lineLabel)"
          :timestamp="formatListTimestamp(thread.latestMessage.timestamp)"
          :unread-count="thread.unreadCount"
          :marked-unread="thread.markedUnread"
          :selected="thread.id === props.selectedThread"
          @delete="deleting = thread.id"
        >
          <template #avatar>
            <ContactAvatar
              :id="match(thread.participant)?.id"
              :name="match(thread.participant)?.name || thread.participant"
              :has-avatar="match(thread.participant)?.hasAvatar"
              :updated-at="match(thread.participant)?.updatedAt"
            />
          </template>
        </MessageConversationRow>
        <MessageListSkeleton
          v-if="loading && cursor"
          :count="2"
        />
        <Button
          v-if="cursor"
          variant="outline"
          class="w-full"
          :disabled="loading"
          @click="load(true)"
          >{{ t('communication.more') }}</Button
        >
      </template>
    </div>
    <ModemMessagesFab
      v-if="supported && !props.selectedThread"
      class="lg:hidden"
      :disabled="false"
      @click="composing = true"
    />
    <CommunicationComposer
      v-if="composing"
      participant=""
      mode="message"
      @close="composing = false"
      @sent="load()"
    />
    <Dialog
      :open="!!deleting"
      @update:open="
        (open: boolean) => {
          if (!open) deleting = ''
        }
      "
    >
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
