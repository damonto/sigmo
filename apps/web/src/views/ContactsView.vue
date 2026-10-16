<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'
import { ContactRound, Plus } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import CommunicationPageHeader from '@/components/communication/CommunicationPageHeader.vue'
import CommunicationSearchInput from '@/components/communication/CommunicationSearchInput.vue'
import ContactListSkeleton from '@/components/contacts/ContactListSkeleton.vue'
import ContactDetailSkeleton from '@/components/contacts/ContactDetailSkeleton.vue'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import ContactDetails from '@/components/contacts/ContactDetails.vue'
import ContactAvatar from '@/components/contacts/ContactAvatar.vue'
import ContactEditor from '@/components/contacts/ContactEditor.vue'
import CommunicationComposer from '@/components/communication/CommunicationComposer.vue'
import { useContactsPage } from '@/composables/useContactsPage'
const { t } = useI18n()
const route = useRoute()
const detailRequested = computed(() => typeof route.params.contactId === 'string')
const {
  modemId,
  desktop,
  closeDetails,
  contactRoute,
  search,
  visible,
  selected,
  supported,
  loading,
  error,
  editor,
  deleting,
  create,
  saved,
  remove,
} = useContactsPage()
const action = ref<'message' | 'call'>()
const number = ref('')
const chooseAction = (participant: string, mode: 'message' | 'call') => {
  number.value = participant
  action.value = mode
}
watch([() => selected.value?.id, modemId], () => {
  number.value = ''
  action.value = undefined
})
const participant = computed(
  () =>
    number.value ||
    (selected.value?.numbers.length === 1 ? selected.value.numbers[0]?.number || '' : ''),
)
const mobileDetailsOpen = computed(
  () =>
    !desktop.value && detailRequested.value && !editor.value && !action.value && !deleting.value,
)
const contactsList = useTemplateRef<HTMLElement>('contactsList')
let contactLink: HTMLElement | undefined
const rememberContactLink = (event: MouseEvent) => {
  if (event.currentTarget instanceof HTMLElement) contactLink = event.currentTarget
}
const restoreListFocus = (event: Event) => {
  event.preventDefault()
  if (detailRequested.value || editor.value || action.value || deleting.value) return
  if (contactLink?.isConnected) contactLink.focus({ preventScroll: true })
  else contactsList.value?.querySelector('input')?.focus({ preventScroll: true })
}
</script>
<template>
  <main
    class="flex min-w-0 flex-col gap-4 lg:h-(--modem-desktop-content-height) lg:min-h-0 lg:overflow-hidden"
  >
    <p
      v-if="error"
      role="alert"
      class="text-sm text-destructive"
    >
      {{ error }}
    </p>
    <div
      class="grid min-w-0 gap-6 lg:min-h-0 lg:flex-1 lg:grid-cols-[22rem_minmax(0,1fr)] lg:overflow-hidden"
    >
      <section
        ref="contactsList"
        class="flex min-w-0 flex-col gap-4 lg:min-h-0 lg:rounded-xl lg:border lg:bg-card/40 lg:p-4"
      >
        <CommunicationPageHeader
          :title="t('communication.contacts')"
          :subtitle="t('communication.contactsDescription')"
        />
        <Button
          v-if="supported"
          class="w-full gap-2"
          @click="create"
        >
          <Plus class="size-4" />{{ t('communication.newContact') }}
        </Button>
        <CommunicationSearchInput
          v-model="search"
          :placeholder="t('communication.searchContacts')"
          :clear-label="t('modemDetail.messages.clearSearch')"
        />
        <p
          v-if="!loading && !supported"
          class="rounded-lg border border-dashed p-4 text-sm text-muted-foreground"
        >
          {{ t('communication.upgrade') }}
        </p>
        <ContactListSkeleton v-if="loading && !visible.length" />
        <p
          v-else-if="supported && !visible.length && !error"
          class="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground"
        >
          {{ t(search ? 'communication.noMatchingContacts' : 'communication.emptyContacts') }}
        </p>
        <div
          v-else-if="visible.length"
          class="divide-y overflow-hidden rounded-xl border bg-card lg:overflow-y-auto"
          :aria-busy="loading"
        >
          <RouterLink
            v-for="contact in visible"
            :key="contact.id"
            :to="{
              ...contactRoute(contact.id),
              query: $route.query.number ? { number: $route.query.number } : {},
            }"
            :aria-current="selected?.id === contact.id ? 'true' : undefined"
            :class="
              selected?.id === contact.id ? 'bg-primary/5 ring-1 ring-inset ring-primary/15' : ''
            "
            class="flex min-h-16 items-center gap-3 px-3 py-3 transition-colors first:rounded-t-[inherit] last:rounded-b-[inherit] hover:bg-muted/50 focus-visible:bg-muted/50"
            @click="rememberContactLink"
            ><ContactAvatar
              class="size-10 text-sm"
              :id="contact.id"
              :name="contact.name"
              :has-avatar="contact.hasAvatar"
              :updated-at="contact.updatedAt"
              :color="contact.avatarColor"
            /><span class="min-w-0 space-y-1">
              <span class="block truncate text-sm font-medium">{{
                contact.name || contact.numbers[0]?.number
              }}</span>
              <span
                v-if="contact.name && contact.numbers[0]"
                class="block truncate text-xs text-muted-foreground"
                >{{ contact.numbers[0].number }}</span
              >
            </span></RouterLink
          >
        </div>
      </section>
      <section
        v-if="desktop"
        class="flex min-h-0 min-w-0 flex-col overflow-y-auto"
      >
        <ContactDetailSkeleton v-if="loading && !selected" />
        <ContactDetails
          v-else-if="supported && selected"
          class="shrink-0 rounded-xl border bg-card p-6"
          :contact="selected"
          @edit="editor = selected"
          @delete="deleting = true"
          @action="chooseAction"
        />
        <div
          v-else-if="!loading"
          class="flex min-h-64 flex-1 flex-col items-center justify-center gap-3 rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground"
        >
          <ContactRound class="size-8" />
          <p>{{ t(supported ? 'communication.selectContact' : 'communication.upgrade') }}</p>
        </div>
      </section>
    </div>
    <Dialog
      v-if="!desktop"
      :open="mobileDetailsOpen"
      @update:open="
        (open: boolean) => {
          if (!open) closeDetails()
        }
      "
    >
      <DialogContent
        class="max-h-[calc(100dvh-2rem)] overflow-y-auto"
        :aria-describedby="undefined"
        @close-auto-focus="restoreListFocus"
      >
        <DialogHeader>
          <DialogTitle>{{ t('communication.contactDetails') }}</DialogTitle>
        </DialogHeader>
        <ContactDetailSkeleton
          v-if="loading && !selected"
          class="rounded-none border-0 p-0"
        />
        <ContactDetails
          v-else-if="supported && selected"
          :contact="selected"
          @edit="editor = selected"
          @delete="deleting = true"
          @action="chooseAction"
        />
        <p
          v-else
          class="text-sm text-muted-foreground"
        >
          {{ error || t(supported ? 'errors.communication_not_found' : 'communication.upgrade') }}
        </p>
      </DialogContent>
    </Dialog>
    <ContactEditor
      v-if="editor"
      :initial="editor"
      @close="editor = undefined"
      @saved="saved"
    />
    <CommunicationComposer
      v-if="action"
      :mode="action"
      :participant="participant"
      :fixed-modem-id="modemId"
      @close="action = undefined"
    />
    <Dialog v-model:open="deleting"
      ><DialogContent
        ><DialogHeader
          ><DialogTitle>{{ t('communication.delete') }}</DialogTitle
          ><DialogDescription>{{
            t('communication.deleteContactDescription')
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
