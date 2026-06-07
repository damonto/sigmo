<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import ModemMessageComposer from '@/components/modem/messages/ModemMessageComposer.vue'
import ModemMessagesPanel from '@/components/modem/messages/ModemMessagesPanel.vue'
import ModemMessageThreadDeleteDialog from '@/components/modem/messages/ModemMessageThreadDeleteDialog.vue'
import ModemMessageThreadHeader from '@/components/modem/messages/ModemMessageThreadHeader.vue'
import ModemMessageThreadList from '@/components/modem/messages/ModemMessageThreadList.vue'
import { useModemMessageThread } from '@/composables/useModemMessageThread'
import { useModemPhoneCountry } from '@/composables/useModemPhoneCountry'

const route = useRoute()
const router = useRouter()

const modemId = computed(() => (route.params.id ?? 'unknown') as string)
const participant = computed(() => (route.params.participant ?? '') as string)
const isNewConversation = computed(
  () => route.query.new === '1' || participant.value.trim() === 'new',
)
const { phoneCountry } = useModemPhoneCountry(modemId)

const {
  items,
  isLoading,
  isSending,
  isDeleting,
  messageDraft,
  newRecipient,
  participantLabel,
  sendMessage,
  deleteThread,
} = useModemMessageThread({
  modemId,
  participant,
  isNewConversation,
  defaultCountry: phoneCountry,
})

const deleteOpen = ref(false)
const messagesContainerRef = ref<HTMLElement | null>(null)

const canDelete = computed(() => !isNewConversation.value && participant.value.trim().length > 0)

const scrollToBottom = () => {
  nextTick(() => {
    if (messagesContainerRef.value) {
      messagesContainerRef.value.scrollTop = messagesContainerRef.value.scrollHeight
    }
  })
}

const handleBack = () => {
  router.back()
}

const openDeleteDialog = () => {
  if (!canDelete.value) return
  deleteOpen.value = true
}

const confirmDelete = async () => {
  await deleteThread()
  deleteOpen.value = false
}

watch(
  () => [items.value, isLoading.value],
  () => {
    if (!isLoading.value && items.value.length > 0) {
      scrollToBottom()
    }
  },
  { flush: 'post' },
)
</script>

<template>
  <div
    class="grid h-[calc(100dvh-6.5rem)] gap-6 overflow-hidden lg:h-[var(--modem-desktop-content-height)] lg:grid-cols-[22rem_minmax(0,1fr)]"
  >
    <div class="hidden min-h-0 overflow-hidden rounded-xl border bg-card/40 p-4 lg:block">
      <ModemMessagesPanel :modem-id="modemId" :selected-participant="participant" compact />
    </div>

    <section
      class="flex min-h-0 flex-col overflow-hidden rounded-none bg-background lg:rounded-xl lg:border lg:bg-card/40 lg:p-4"
    >
      <ModemMessageThreadHeader
        :title="participantLabel"
        :can-delete="canDelete"
        @back="handleBack"
        @delete="openDeleteDialog"
      />

      <div ref="messagesContainerRef" class="scrollbar-none min-h-0 flex-1 overflow-y-auto py-3">
        <ModemMessageThreadList
          :items="items"
          :is-loading="isLoading"
          :participant-label="participantLabel"
        />
      </div>

      <ModemMessageComposer
        v-model:message="messageDraft"
        v-model:recipient="newRecipient"
        :is-new-conversation="isNewConversation"
        :is-sending="isSending"
        :is-loading="isLoading"
        :default-country="phoneCountry"
        @submit="sendMessage"
      />
    </section>
  </div>

  <ModemMessageThreadDeleteDialog
    v-model:open="deleteOpen"
    :participant-label="participantLabel"
    :is-deleting="isDeleting"
    @confirm="confirmDelete"
  />
</template>
