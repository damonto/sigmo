import { ref, toRaw } from 'vue'
import { communicationApi } from '@/apis/communication'
import type { Contact } from '@/types/communication'

/** Upload the edited image only after metadata has a stable contact identity. */
export const useContactEditor = (initial: Contact) => {
  const contact = ref<Contact>(structuredClone(toRaw(initial)))
  const saving = ref(false)
  const error = ref('')
  const avatar = ref<Blob>()
  const avatarRevision = ref(0)
  const removeAvatar = ref(false)
  const save = async (): Promise<Contact | undefined> => {
    saving.value = true
    error.value = ''
    try {
      contact.value = await communicationApi.saveContact(contact.value)
      if (avatar.value)
        contact.value = await communicationApi.saveAvatar(contact.value.id, avatar.value)
      else if (removeAvatar.value)
        contact.value = await communicationApi.removeAvatar(contact.value.id)
      return contact.value
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
      return undefined
    } finally {
      saving.value = false
    }
  }
  const clearAvatar = () => {
    avatarRevision.value++
    removeAvatar.value = true
    avatar.value = undefined
    contact.value.hasAvatar = false
  }
  return { avatarRevision, clearAvatar, contact, saving, error, avatar, removeAvatar, save }
}
