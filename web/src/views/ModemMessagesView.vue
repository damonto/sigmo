<script setup lang="ts">
import { refDebounced } from '@vueuse/core'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import ModemMessagesDeleteDialog from '@/components/modem/messages/ModemMessagesDeleteDialog.vue'
import ModemMessagesFab from '@/components/modem/messages/ModemMessagesFab.vue'
import ModemMessagesHeader from '@/components/modem/messages/ModemMessagesHeader.vue'
import ModemMessagesList from '@/components/modem/messages/ModemMessagesList.vue'
import ModemMessagesSearch from '@/components/modem/messages/ModemMessagesSearch.vue'
import { useModemPhoneCountry } from '@/composables/useModemPhoneCountry'
import { useModemMessages, type ConversationItem } from '@/composables/useModemMessages'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const modemId = computed(() => (route.params.id ?? 'unknown') as string)
const { phoneCountry } = useModemPhoneCountry(modemId)

const searchQuery = ref('')
const normalizedSearchQuery = computed(() => searchQuery.value.trim())
const debouncedSearchQuery = refDebounced(normalizedSearchQuery, 250)
const { items, count, isLoading, deleteConversation } = useModemMessages(
  modemId,
  phoneCountry,
  debouncedSearchQuery,
)
const deleteOpen = ref(false)
const deleteLoading = ref(false)
const deleteTarget = ref<ConversationItem | null>(null)

const deleteTargetLabel = computed(() => deleteTarget.value?.participantLabel ?? '')
const isFabDisabled = computed(() => modemId.value === 'unknown')
const isSearching = computed(() => debouncedSearchQuery.value.length > 0)
const emptyLabel = computed(() =>
  isSearching.value ? t('modemDetail.messages.noSearchResults') : t('modemDetail.messages.empty'),
)

const openDeleteDialog = (item: ConversationItem) => {
  deleteTarget.value = item
  deleteOpen.value = true
}

const closeDeleteDialog = () => {
  deleteOpen.value = false
  deleteTarget.value = null
}

const confirmDelete = async () => {
  if (!deleteTarget.value) return
  deleteLoading.value = true
  try {
    await deleteConversation(deleteTarget.value.participantValue)
  } catch (err) {
    console.error('[ModemMessagesView] Failed to delete messages:', err)
  } finally {
    deleteLoading.value = false
    closeDeleteDialog()
  }
}

const startConversation = async () => {
  if (!modemId.value || modemId.value === 'unknown') return
  await router.push({
    name: 'modem-message-thread',
    params: { id: modemId.value, participant: 'new' },
    query: { new: '1' },
  })
}
</script>

<template>
  <div class="space-y-6">
    <ModemMessagesHeader :count="count" :is-loading="isLoading" />

    <ModemMessagesSearch v-model="searchQuery" />

    <ModemMessagesList
      :items="items"
      :modem-id="modemId"
      :is-loading="isLoading"
      :empty-label="emptyLabel"
      @delete="openDeleteDialog"
    />
  </div>

  <ModemMessagesFab :disabled="isFabDisabled" @click="startConversation" />

  <ModemMessagesDeleteDialog
    v-model:open="deleteOpen"
    :target-label="deleteTargetLabel"
    :is-deleting="deleteLoading"
    @confirm="confirmDelete"
  />
</template>
