<script setup lang="ts">
import { refDebounced } from '@vueuse/core'
import { Plus } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import ModemMessagesDeleteDialog from '@/components/modem/messages/ModemMessagesDeleteDialog.vue'
import ModemMessagesFab from '@/components/modem/messages/ModemMessagesFab.vue'
import ModemMessagesHeader from '@/components/modem/messages/ModemMessagesHeader.vue'
import ModemMessagesList from '@/components/modem/messages/ModemMessagesList.vue'
import ModemMessagesSearch from '@/components/modem/messages/ModemMessagesSearch.vue'
import { Button } from '@/components/ui/button'
import { useModemPhoneCountry } from '@/composables/useModemPhoneCountry'
import { useModemMessages, type ConversationItem } from '@/composables/useModemMessages'

const props = withDefaults(
  defineProps<{
    modemId: string
    selectedParticipant?: string
    compact?: boolean
  }>(),
  {
    selectedParticipant: '',
    compact: false,
  },
)

const router = useRouter()
const { t } = useI18n()
const modemIdRef = computed(() => props.modemId)
const { phoneCountry } = useModemPhoneCountry(modemIdRef)

const searchQuery = ref('')
const normalizedSearchQuery = computed(() => searchQuery.value.trim())
const debouncedSearchQuery = refDebounced(normalizedSearchQuery, 250)
const { items, count, isLoading, deleteConversation } = useModemMessages(
  modemIdRef,
  phoneCountry,
  debouncedSearchQuery,
)

const deleteOpen = ref(false)
const deleteLoading = ref(false)
const deleteTarget = ref<ConversationItem | null>(null)

const deleteTargetLabel = computed(() => deleteTarget.value?.participantLabel ?? '')
const isFabDisabled = computed(() => props.modemId === 'unknown')
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
    console.error('[ModemMessagesPanel] delete messages:', err)
  } finally {
    deleteLoading.value = false
    closeDeleteDialog()
  }
}

const startConversation = async () => {
  if (!props.modemId || props.modemId === 'unknown') return
  await router.push({
    name: 'modem-message-thread',
    params: { id: props.modemId, participant: 'new' },
    query: { new: '1' },
  })
}
</script>

<template>
  <div
    class="flex min-w-0 flex-col lg:h-full lg:min-h-0"
    :class="props.compact ? 'gap-3' : 'gap-6'"
  >
    <ModemMessagesHeader v-if="!props.compact" :count="count" :is-loading="isLoading" />

    <div v-else class="flex items-center justify-between gap-3">
      <div class="min-w-0">
        <h2 class="truncate text-base font-semibold text-foreground">
          {{ t('modemDetail.messages.title') }}
        </h2>
        <p class="truncate text-xs text-muted-foreground">
          {{ t('modemDetail.messages.subtitle') }}
        </p>
      </div>
      <span class="shrink-0 rounded-full border px-2 py-0.5 text-xs text-muted-foreground">
        {{ isLoading ? '...' : count }}
      </span>
    </div>

    <Button
      type="button"
      class="hidden w-full gap-2 lg:inline-flex"
      :disabled="isFabDisabled"
      @click="startConversation"
    >
      <Plus class="size-4" />
      {{ t('modemDetail.messages.newConversation') }}
    </Button>

    <ModemMessagesSearch v-model="searchQuery" />

    <div class="scrollbar-none lg:min-h-0 lg:flex-1 lg:overflow-y-auto">
      <ModemMessagesList
        :items="items"
        :modem-id="props.modemId"
        :is-loading="isLoading"
        :empty-label="emptyLabel"
        :selected-participant="props.selectedParticipant"
        @delete="openDeleteDialog"
      />
    </div>
  </div>

  <ModemMessagesFab class="lg:hidden" :disabled="isFabDisabled" @click="startConversation" />

  <ModemMessagesDeleteDialog
    v-model:open="deleteOpen"
    :target-label="deleteTargetLabel"
    :is-deleting="deleteLoading"
    @confirm="confirmDelete"
  />
</template>
