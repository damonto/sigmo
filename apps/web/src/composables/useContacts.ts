import { computed, ref, watch } from 'vue'
import { communicationApi } from '@/apis/communication'
import { sessionRevision } from '@/lib/authSession'
import type { Contact } from '@/types/communication'

const contacts = ref<Contact[]>([])
const revision = ref(0)
let loadedAt: number | undefined
let pending: Promise<void> | undefined
/** Invalidates in-flight requests as well as cached private address book data. */
export const clearContacts = () => {
  revision.value++
  contacts.value = []
  loadedAt = undefined
  pending = undefined
}
// The cache lives for the application lifetime; old responses cannot restore it.
watch(sessionRevision, clearContacts, { flush: 'sync' })
/** The embedded Web client has one backend; names are never drawn from another server. */
export const useContacts = () => {
  const load = async (force = false) => {
    if (!force && pending) return pending
    if (!force && loadedAt !== undefined && Date.now() - loadedAt < 15_000) return
    const current = ++revision.value
    const token = sessionRevision()
    pending = (async () => {
      try {
        const response = await communicationApi.contacts()
        if (current === revision.value && token === sessionRevision()) {
          contacts.value = response.items
          loadedAt = Date.now()
        }
      } finally {
        if (current === revision.value) pending = undefined
      }
    })()
    return pending
  }
  const match = (number: string): Contact | undefined => {
    const matches = contacts.value.filter((contact) =>
      contact.numbers.some((phone) => phone.key === number || phone.number === number),
    )
    return matches.length === 1 ? matches[0] : undefined
  }
  return { contacts: computed(() => contacts.value), load, match }
}
