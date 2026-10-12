import { fetchJson } from '@/lib/fetch'
import type {
  WebPushOverviewResponse,
  WebPushRegisterPayload,
  WebPushSubscriptionResponse,
} from '@/types/webPush'

export const useWebPushApi = () => {
  const getOverview = () => fetchJson<WebPushOverviewResponse>('web-push')

  const updateEnabled = (enabled: boolean) =>
    fetchJson<WebPushOverviewResponse>('web-push', {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    })

  const registerSubscription = (payload: WebPushRegisterPayload) =>
    fetchJson<WebPushSubscriptionResponse>('web-push/subscriptions', {
      method: 'POST',
      body: JSON.stringify(payload),
    })

  const renameSubscription = (id: string, label: string) =>
    fetchJson<WebPushSubscriptionResponse>(`web-push/subscriptions/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ label }),
    })

  const deleteSubscription = (id: string) =>
    fetchJson<void>(`web-push/subscriptions/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })

  return {
    getOverview,
    updateEnabled,
    registerSubscription,
    renameSubscription,
    deleteSubscription,
  }
}
