<script setup lang="ts">
import { Pencil, Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import CommunicationActionButton from '@/components/communication/CommunicationActionButton.vue'
import ContactAvatar from '@/components/contacts/ContactAvatar.vue'
import ContactQuickActions from '@/components/contacts/ContactQuickActions.vue'
import { Button } from '@/components/ui/button'
import { DialogFooter } from '@/components/ui/dialog'
import { Separator } from '@/components/ui/separator'
import type { Contact } from '@/types/communication'

defineProps<{ contact: Contact }>()
const emit = defineEmits<{
  edit: []
  delete: []
  action: [number: string, mode: 'message' | 'call']
}>()
const { t } = useI18n()
</script>

<template>
  <div class="grid min-w-0 gap-4">
    <div class="space-y-4">
      <div class="flex flex-col items-center gap-3 lg:flex-row lg:gap-4">
        <ContactAvatar
          class="size-20 text-2xl lg:size-14 lg:text-lg"
          :id="contact.id"
          :name="contact.name"
          :has-avatar="contact.hasAvatar"
          :updated-at="contact.updatedAt"
          :color="contact.avatarColor"
        />
        <h2
          id="contact-name"
          class="min-w-0 max-w-full break-words text-center text-lg font-semibold lg:text-left"
        >
          {{ contact.name || contact.numbers[0]?.number }}
        </h2>
      </div>
      <ContactQuickActions
        class="justify-center lg:justify-start"
        :numbers="contact.numbers"
        @action="(number, mode) => emit('action', number, mode)"
      >
        <template #label="{ label }">
          <span class="sr-only lg:not-sr-only">{{ label }}</span>
        </template>
      </ContactQuickActions>
    </div>
    <dl
      v-if="contact.numbers.length || contact.notes"
      class="grid gap-4"
    >
      <div
        v-for="(phone, index) in contact.numbers"
        :key="index"
        class="grid gap-1 lg:grid-cols-[8rem_minmax(0,1fr)] lg:gap-4"
      >
        <dt class="break-words text-sm text-muted-foreground">
          {{ phone.label || t('communication.number') }}
        </dt>
        <dd class="break-words text-sm tabular-nums">{{ phone.number }}</dd>
      </div>
      <div
        v-if="contact.notes"
        class="grid gap-1 lg:grid-cols-[8rem_minmax(0,1fr)] lg:gap-4"
      >
        <dt class="text-sm text-muted-foreground">{{ t('communication.notes') }}</dt>
        <dd class="whitespace-pre-wrap break-words text-sm leading-relaxed">{{ contact.notes }}</dd>
      </div>
    </dl>
    <Separator />
    <DialogFooter class="flex-col sm:flex-col lg:flex-row">
      <Button @click="emit('edit')">
        <Pencil class="size-4" />{{ t('communication.editContact') }}
      </Button>
      <CommunicationActionButton
        destructive
        @click="emit('delete')"
      >
        <Trash2 class="size-4" />{{ t('communication.delete') }}
      </CommunicationActionButton>
    </DialogFooter>
  </div>
</template>
