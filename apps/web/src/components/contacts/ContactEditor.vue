<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import ContactAvatar from '@/components/contacts/ContactAvatar.vue'
import AvatarCrop from '@/components/contacts/AvatarCrop.vue'
import { useContactEditor } from '@/composables/useContactEditor'
import type { Contact } from '@/types/communication'
const props = defineProps<{ initial: Contact }>()
const emit = defineEmits<{ close: []; saved: [contact: Contact] }>()
const { t } = useI18n()
const { contact, saving, error, avatar, removeAvatar, save, clearAvatar, avatarRevision } =
  useContactEditor(props.initial)
const submit = async () => {
  const saved = await save()
  if (saved) emit('saved', saved)
}
</script>
<template>
  <Dialog
    :open="true"
    @update:open="
      (open: boolean) => {
        if (!open && !saving) emit('close')
      }
    "
    ><DialogContent
      class="max-h-[85dvh] overflow-y-auto"
      :aria-describedby="undefined"
      ><DialogHeader
        ><DialogTitle>{{ t('communication.editContact') }}</DialogTitle></DialogHeader
      >
      <form
        class="grid gap-5"
        @submit.prevent="submit"
      >
        <label class="grid gap-2"
          >{{ t('communication.name')
          }}<Input
            v-model="contact.name"
            autocomplete="name"
            maxlength="512"
        /></label>
        <div
          v-for="(phone, index) in contact.numbers"
          :key="index"
          class="grid gap-2 rounded-xl border p-3"
        >
          <label>{{ t('communication.label') }}<Input v-model="phone.label" /></label>
          <label
            >{{ t('communication.number')
            }}<Input
              v-model="phone.number"
              type="tel"
              required
          /></label>
          <Button
            type="button"
            variant="ghost"
            @click="contact.numbers.splice(index, 1)"
            >{{ t('communication.remove') }}</Button
          >
        </div>
        <Button
          type="button"
          variant="outline"
          @click="contact.numbers.push({ label: '', number: '', region: '', key: '' })"
          >{{ t('communication.addNumber') }}</Button
        >
        <label class="grid gap-2"
          >{{ t('communication.notes')
          }}<Textarea
            v-model="contact.notes"
            rows="4"
        /></label>
        <ContactAvatar
          v-if="contact.hasAvatar && !avatar && !removeAvatar"
          :id="contact.id"
          :name="contact.name"
          :has-avatar="true"
          :updated-at="contact.updatedAt"
        />
        <AvatarCrop
          :key="avatarRevision"
          @selected="
            (image: Blob) => {
              avatar = image
              removeAvatar = false
            }
          "
        />
        <Button
          v-if="contact.hasAvatar || avatar"
          type="button"
          variant="outline"
          @click="clearAvatar"
          >{{ t('communication.removeAvatar') }}</Button
        >
        <p
          v-if="error"
          role="alert"
          class="text-destructive"
        >
          {{ error }}
        </p>
        <Button
          type="submit"
          :disabled="saving || (!contact.name.trim() && !contact.numbers.length)"
          >{{ t('communication.save') }}</Button
        >
      </form>
    </DialogContent></Dialog
  >
</template>
