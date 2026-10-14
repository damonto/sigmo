import { computed, onScopeDispose, ref, toRaw, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { communicationApi } from '@/apis/communication'
import { useContacts } from '@/composables/useContacts'
import { useCapabilities } from '@/composables/useCapabilities'
import { sessionRevision } from '@/lib/authSession'
import type { Contact } from '@/types/communication'

export const emptyContact = (number = ''): Contact => ({
  id: '',
  name: '',
  notes: '',
  numbers: number ? [{ label: '', number, region: '', key: '' }] : [],
  hasAvatar: false,
  avatarColor: '',
  updatedAt: '',
})
/** Keeps route identity separate from unsaved contact drafts. */
export const useContactsPage = () => {
  const route = useRoute()
  const router = useRouter()
  const modemId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''))
  const listRoute = computed(() =>
    modemId.value
      ? { name: 'modem-contacts', params: { id: modemId.value } }
      : { name: 'contacts' },
  )
  const contactRoute = (contactId: string) =>
    modemId.value
      ? { name: 'modem-contact', params: { id: modemId.value, contactId } }
      : { name: 'contact', params: { contactId } }
  const { contacts, load, match } = useContacts()
  const capability = useCapabilities()
  const supported = computed(() => capability.hasFeature('contacts'))
  const search = ref('')
  const error = ref('')
  const loading = ref(true)
  const editor = ref<Contact>()
  const deleting = ref(false)
  let generation = 0
  let disposed = false
  watch(
    [modemId, sessionRevision],
    () => {
      generation++
      editor.value = undefined
      deleting.value = false
      error.value = ''
    },
    { flush: 'sync' },
  )
  onScopeDispose(() => {
    disposed = true
    generation++
  })
  const desktop = useMediaQuery('(min-width: 1024px)')
  const visible = computed(() =>
    contacts.value.filter((contact) =>
      [contact.name, ...contact.numbers.flatMap((phone) => [phone.number, phone.key])].some(
        (value) => value.toLocaleLowerCase().includes(search.value.toLocaleLowerCase()),
      ),
    ),
  )
  const selected = computed(() =>
    route.params.contactId
      ? contacts.value.find((contact) => contact.id === route.params.contactId)
      : desktop.value
        ? visible.value[0]
        : undefined,
  )
  const refresh = async (force = false) => {
    if (disposed) return false
    const current = ++generation
    loading.value = true
    error.value = ''
    try {
      await capability.fetchCapabilities()
      if (current !== generation) return false
      if (supported.value) await load(force)
      return current === generation
    } catch (reason) {
      if (current === generation)
        error.value = reason instanceof Error ? reason.message : String(reason)
      return false
    } finally {
      if (current === generation) loading.value = false
    }
  }
  watch(
    () => [modemId.value, route.params.contactId, route.query.number, sessionRevision()],
    async (_, __, cleanup) => {
      let cancelled = false
      cleanup(() => {
        cancelled = true
        generation++
      })
      const refreshed = await refresh()
      if (cancelled || !refreshed) return
      const number = typeof route.query.number === 'string' ? route.query.number : ''
      if (number && route.params.contactId && selected.value) {
        editor.value = structuredClone(toRaw(selected.value))
        if (!editor.value.numbers.some((phone) => phone.number === number || phone.key === number))
          editor.value.numbers.push({ label: '', number, region: '', key: '' })
      }
    },
    { immediate: true },
  )
  const create = () => {
    editor.value = emptyContact(typeof route.query.number === 'string' ? route.query.number : '')
  }
  const closeDetails = () => router.replace({ ...listRoute.value, query: route.query })
  const saved = async (contact: Contact) => {
    editor.value = undefined
    if (await refresh(true)) await router.replace(contactRoute(contact.id))
  }
  const remove = async () => {
    if (!selected.value) return
    const current = generation
    try {
      await communicationApi.deleteContact(selected.value.id)
      if (current !== generation) return
      deleting.value = false
      if (await refresh(true)) await router.replace(listRoute.value)
    } catch (reason) {
      if (current === generation)
        error.value = reason instanceof Error ? reason.message : String(reason)
    }
  }
  return {
    modemId,
    desktop,
    closeDetails,
    listRoute,
    contactRoute,
    search,
    visible,
    selected,
    supported,
    loading,
    error,
    editor,
    deleting,
    refresh,
    create,
    saved,
    remove,
    match,
  }
}
