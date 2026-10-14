import { fetchJson, useFetch } from '@/lib/fetch'
import { sessionRevision } from '@/lib/authSession'
import type { CallRecord } from '@/types/call'
import type {
  CommunicationBadge,
  CommunicationTarget,
  Contact,
  SIMLine,
  ThreadPage,
  HistoryPage,
  ThreadMessage,
  MessageThread,
  CommunicationPreference,
  HistoryFilter,
} from '@/types/communication'

const value = async <T>(path: string, options?: RequestInit): Promise<T> => {
  const { data } = await fetchJson<T>(path, options)
  if (data.value === undefined) throw new Error('Empty response')
  return data.value
}
const query = (filter: HistoryFilter) =>
  new URLSearchParams(
    Object.entries(filter).filter(([, value]) => value !== undefined && value !== ''),
  ).toString()
const body = (method: string, data: unknown): RequestInit => ({
  method,
  body: JSON.stringify(data),
})

let directory:
  { revision: number; expires: number; request: Promise<{ items: SIMLine[] }> } | undefined
/** Discards historical labels and deletion state after a SIM mutation or inventory refresh. */
export const invalidateCommunicationDirectory = () => {
  directory = undefined
}
// Ordinary history pages only need archived metadata. Live inventory is reserved
// for the handover picker; the server verifies the target again on submission.
const lines = (cached = true): Promise<{ items: SIMLine[] }> => {
  if (!cached) return value('sim-profiles')
  const revision = sessionRevision()
  if (directory?.revision === revision && directory.expires > Date.now()) return directory.request
  const request = value<{ items: SIMLine[] }>('sim-profiles?cached=true')
  directory = { revision, expires: Infinity, request }
  void request.then(
    () => {
      if (directory?.request === request) directory.expires = Date.now() + 15_000
    },
    () => {
      if (directory?.request === request) directory = undefined
    },
  )
  return request
}
/** Requests inherit the existing backend authentication and error handling. */
export const communicationApi = {
  contacts: (q = '') => value<{ items: Contact[] }>(`contacts?q=${encodeURIComponent(q)}`),
  contact: (id: string) => value<Contact>(`contacts/${encodeURIComponent(id)}`),
  saveContact: (contact: Contact) =>
    value<Contact>(
      contact.id ? `contacts/${encodeURIComponent(contact.id)}` : 'contacts',
      body(contact.id ? 'PUT' : 'POST', contact),
    ),
  deleteContact: (id: string) =>
    fetchJson<void>(`contacts/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  avatar: async (id: string): Promise<Blob> => {
    const request = useFetch(`contacts/${encodeURIComponent(id)}/avatar`, {
      immediate: false,
    }).blob()
    await request.execute(true)
    if (!request.data.value) throw new Error('Empty avatar')
    return request.data.value
  },
  saveAvatar: (id: string, image: Blob) =>
    value<Contact>(`contacts/${encodeURIComponent(id)}/avatar`, {
      method: 'PUT',
      body: image,
      headers: { 'Content-Type': image.type },
    }),
  removeAvatar: (id: string) =>
    value<Contact>(`contacts/${encodeURIComponent(id)}/avatar`, { method: 'DELETE' }),
  lines,
  preference: (participant: string) =>
    value<CommunicationPreference>(
      `communication-preferences?participant=${encodeURIComponent(participant)}`,
    ),
  assign: async (source: string, target: string) => {
    await fetchJson<void>(
      `sim-profiles/${encodeURIComponent(source)}/history-assignment`,
      body('PUT', { targetProfileId: target }),
    )
    directory = undefined
  },
  threads: (filter: HistoryFilter = {}) => value<ThreadPage>(`message-threads?${query(filter)}`),
  thread: (id: string) => value<MessageThread>(`message-threads/${encodeURIComponent(id)}`),
  messages: (id: string, cursor = '', profileId = '') =>
    value<HistoryPage<ThreadMessage>>(
      `message-threads/${encodeURIComponent(id)}/messages?cursor=${encodeURIComponent(cursor)}&profileId=${encodeURIComponent(profileId)}`,
    ),
  read: (id: string, messageIds: number[]) =>
    value<CommunicationBadge>(
      `message-threads/${encodeURIComponent(id)}`,
      body('PATCH', { messageIds }),
    ),
  markUnread: (id: string) =>
    value<ThreadPage>(
      `message-threads/${encodeURIComponent(id)}`,
      body('PATCH', { markedUnread: true }),
    ),
  deleteThread: (id: string) =>
    fetchJson<void>(`message-threads/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  calls: (filter: HistoryFilter = {}) => value<HistoryPage<CallRecord>>(`calls?${query(filter)}`),
  deleteCall: (id: string) =>
    fetchJson<void>(`calls/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  send: (line: CommunicationTarget, to: string, text: string) =>
    value<{ to: string; conversationId: string }>(
      `modems/${encodeURIComponent(line.modemId)}/messages`,
      body('POST', { to, text, expectedProfileId: line.profileId }),
    ),
}
