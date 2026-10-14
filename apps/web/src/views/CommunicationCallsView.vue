<script setup lang="ts">
import { computed, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { Trash2, UserPlus } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import CommunicationPageHeader from '@/components/communication/CommunicationPageHeader.vue'
import CommunicationSearchInput from '@/components/communication/CommunicationSearchInput.vue'
import CommunicationHistoryFilter from '@/components/communication/CommunicationHistoryFilter.vue'
import CallListSkeleton from '@/components/communication/CallListSkeleton.vue'
import CommunicationDialpad from '@/components/communication/CommunicationDialpad.vue'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import CallHistoryRow from '@/components/communication/CallHistoryRow.vue'
import CallHistoryFields from '@/components/communication/CallHistoryFields.vue'
import { useCallLabels } from '@/composables/useCallLabels'
import ContactQuickActions from '@/components/contacts/ContactQuickActions.vue'
import ContactAvatar from '@/components/contacts/ContactAvatar.vue'
import CommunicationComposer from '@/components/communication/CommunicationComposer.vue'
import CommunicationActionButton from '@/components/communication/CommunicationActionButton.vue'
import HistoryAssignments from '@/components/communication/HistoryAssignments.vue'
import { useCommunicationHistory } from '@/composables/useCommunicationHistory'
import type { CallRecord } from '@/types/call'
const { t } = useI18n()
const { stateLabel, routeLabel, callDurationLabel } = useCallLabels()
const {
  search,
  loading,
  error,
  cursor,
  visibleCalls,
  missedOnly,
  supported,
  load,
  deleteCurrent,
  match,
  sourceLabel,
  lineDeleted,
} = useCommunicationHistory('calls')
const dialpadRef = useTemplateRef<InstanceType<typeof CommunicationDialpad>>('dialpadRef')
const selection = ref<CallRecord>()
const action = ref<{ number: string; mode: 'message' | 'call'; modemId?: string }>()
const deleting = ref('')
const selectedNumbers = computed(() =>
  selection.value?.number ? [{ number: selection.value.number, label: '' }] : [],
)
const contactAction = (number: string, mode: 'message' | 'call') => {
  const modemId = selection.value?.modemId
  selection.value = undefined
  if (mode === 'call') void dialpadRef.value?.open(number, modemId)
  else action.value = { number, mode, modemId }
}

const remove = async () => {
  const id = deleting.value
  deleting.value = ''
  if (await deleteCurrent(id)) {
    if (selection.value?.callID === id) selection.value = undefined
  }
}
</script>
<template>
  <main
    class="flex min-w-0 flex-col gap-4 lg:h-(--modem-desktop-content-height) lg:min-h-0 lg:overflow-hidden"
  >
    <CommunicationPageHeader
      :title="t('communication.calls')"
      :subtitle="t('communication.callsDescription')"
    />
    <p
      v-if="error"
      role="alert"
      class="text-destructive"
    >
      {{ error }}
    </p>
    <template v-if="supported || loading">
      <HistoryAssignments
        v-if="supported"
        @changed="load()"
      />
      <div class="grid flex-1 gap-6 lg:min-h-0 lg:grid-cols-[minmax(0,1fr)_18rem] lg:items-stretch">
        <div class="min-w-0 space-y-4 lg:flex lg:min-h-0 lg:flex-col lg:gap-4 lg:space-y-0">
          <CommunicationSearchInput
            v-model="search"
            :placeholder="t('modemDetail.calls.searchPlaceholder')"
            :clear-label="t('modemDetail.calls.clearSearch')"
          />
          <CommunicationHistoryFilter
            v-model="missedOnly"
            :label="t('communication.filters')"
            :all-label="t('communication.all')"
            :filtered-label="t('communication.missed')"
          />
          <div
            class="scrollbar-none space-y-3 lg:min-h-0 lg:flex-1 lg:overflow-y-auto"
            :aria-busy="loading"
          >
            <CallListSkeleton v-if="loading && !visibleCalls.length" />
            <p
              v-else-if="!visibleCalls.length && !error"
              class="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground"
            >
              {{ t('communication.emptyCalls') }}
            </p>
            <div
              v-else
              class="space-y-3"
            >
              <CallHistoryRow
                v-for="call in visibleCalls"
                :key="call.callID"
                :call="call"
                :title="match(call.number)?.name || call.number"
                :source="sourceLabel(call.profileId || '', call.lineLabel)"
                :state="stateLabel(call.state)"
                :route="routeLabel(call.route)"
                :duration="callDurationLabel(call)"
                :callback-disabled="
                  !call.number || (call.state !== 'ended' && call.state !== 'failed')
                "
                @select="selection = call"
                @callback="dialpadRef?.open(call.number, call.modemId)"
              >
                <template #avatar>
                  <ContactAvatar
                    :id="match(call.number)?.id"
                    :name="match(call.number)?.name || call.number"
                    :has-avatar="match(call.number)?.hasAvatar"
                    :updated-at="match(call.number)?.updatedAt"
                  />
                </template>
              </CallHistoryRow>
            </div>
            <Button
              v-if="cursor"
              :disabled="loading"
              variant="outline"
              @click="load(true)"
              >{{ t('communication.more') }}</Button
            >
          </div>
        </div>
        <CommunicationDialpad
          v-if="supported"
          ref="dialpadRef"
          @dialed="load()"
        />
      </div>
    </template>
    <p
      v-else
      class="rounded-lg border border-dashed p-4 text-sm text-muted-foreground"
    >
      {{ t('communication.upgradeCalls') }}
    </p>
    <Dialog
      :open="!!selection"
      @update:open="
        (open: boolean) => {
          if (!open) selection = undefined
        }
      "
      ><DialogContent
        ><DialogHeader
          ><DialogTitle>{{ match(selection?.number || '')?.name || selection?.number }}</DialogTitle
          ><DialogDescription>{{
            selection ? stateLabel(selection.state) : ''
          }}</DialogDescription></DialogHeader
        >
        <CallHistoryFields
          v-if="selection"
          :call="selection"
          :state="stateLabel(selection.state)"
          :route="routeLabel(selection.route)"
          :duration="callDurationLabel(selection)"
        />
        <p>{{ sourceLabel(selection?.profileId || '', selection?.lineLabel) }}</p>
        <p v-if="lineDeleted(selection?.profileId || '')">{{ t('communication.deletedNumber') }}</p>
        <p v-if="selection?.profileId !== selection?.historyProfileId">
          {{ t('communication.originalLine') }}
        </p>
        <ContactQuickActions
          class="mx-auto"
          :numbers="selectedNumbers"
          :call-disabled="selection?.state !== 'ended' && selection?.state !== 'failed'"
          @action="contactAction"
        />
        <div class="flex flex-wrap justify-end gap-2 border-t pt-3">
          <CommunicationActionButton as-child
            ><RouterLink :to="{ name: 'contacts', query: { number: selection?.number } }"
              ><UserPlus class="size-4" />{{ t('communication.addContact') }}</RouterLink
            ></CommunicationActionButton
          ><CommunicationActionButton
            v-if="selection?.state === 'ended' || selection?.state === 'failed'"
            destructive
            @click="deleting = selection?.callID || ''"
            ><Trash2 class="size-4" />{{ t('communication.delete') }}</CommunicationActionButton
          >
        </div></DialogContent
      ></Dialog
    >
    <CommunicationComposer
      v-if="action"
      :participant="action.number"
      :mode="action.mode"
      :modem-id="action.modemId"
      @close="action = undefined"
      @sent="load()"
    />
    <Dialog
      :open="!!deleting"
      @update:open="
        (open: boolean) => {
          if (!open) deleting = ''
        }
      "
      ><DialogContent
        ><DialogHeader
          ><DialogTitle>{{ t('communication.delete') }}</DialogTitle
          ><DialogDescription>{{
            t('communication.deleteCallDescription')
          }}</DialogDescription></DialogHeader
        ><Button
          variant="destructive"
          @click="remove"
          >{{ t('communication.delete') }}</Button
        ></DialogContent
      ></Dialog
    >
  </main>
</template>
