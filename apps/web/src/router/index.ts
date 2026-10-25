import {
  createRouter,
  createWebHistory,
  type NavigationGuardReturn,
  type RouteLocationNormalized,
} from 'vue-router'

import { useAuthStore } from '@/stores/auth'
import { useLicenseStore } from '@/stores/license'
import { useCapabilities } from '@/composables/useCapabilities'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/activate',
      name: 'activate',
      component: () => import('@/views/ActivateView.vue'),
    },
    {
      path: '/auth',
      name: 'auth',
      component: () => import('@/views/AuthVerifyView.vue'),
    },
    {
      path: '/',
      component: () => import('@/layouts/CommunicationLayout.vue'),
      children: [
        { path: '', name: 'home', component: () => import('@/views/HomeView.vue') },
        { path: 'contacts', name: 'contacts', component: () => import('@/views/ContactsView.vue') },
        {
          path: 'contacts/:contactId',
          name: 'contact',
          component: () => import('@/views/ContactsView.vue'),
        },
        {
          path: 'messages',
          name: 'messages',
          component: () => import('@/views/CommunicationMessagesView.vue'),
        },
        {
          path: 'messages/:threadId',
          name: 'message-thread',
          component: () => import('@/views/CommunicationMessagesView.vue'),
        },
        {
          path: 'calls',
          name: 'calls',
          component: () => import('@/views/CommunicationCallsView.vue'),
        },
        {
          path: 'settings',
          component: () => import('@/layouts/SettingsLayout.vue'),
          children: [
            {
              path: 'updates',
              name: 'settings-updates',
              component: () => import('@/views/SettingsUpdatesView.vue'),
            },
            {
              path: '',
              name: 'settings',
              component: () => import('@/views/SettingsView.vue'),
            },
            {
              path: 'auth',
              name: 'settings-auth',
              component: () => import('@/views/SettingsAuthView.vue'),
            },
            {
              path: 'proxy',
              name: 'settings-proxy',
              component: () => import('@/views/SettingsProxyView.vue'),
            },
            {
              path: 'web-push',
              name: 'settings-web-push',
              component: () => import('@/views/SettingsWebPushView.vue'),
            },
            {
              path: 'notifications',
              name: 'settings-notifications',
              component: () => import('@/views/SettingsNotificationsView.vue'),
            },
            {
              path: 'mcp',
              name: 'settings-mcp',
              component: () => import('@/views/SettingsMCPView.vue'),
            },
          ],
        },
      ],
    },
    {
      path: '/modems/:id',
      component: () => import('@/layouts/ModemLayout.vue'),
      children: [
        {
          path: '',
          name: 'modem-detail',
          component: () => import('@/views/ModemDetailView.vue'),
        },
        {
          path: 'messages',
          name: 'modem-messages',
          component: () => import('@/views/ModemMessagesView.vue'),
        },
        {
          path: 'messages/:participant',
          name: 'modem-message-thread',
          component: () => import('@/views/ModemMessageThreadView.vue'),
        },
        {
          path: 'notifications',
          name: 'modem-notifications',
          component: () => import('@/views/ModemNotificationsView.vue'),
        },
        {
          path: 'calls',
          name: 'modem-calls',
          component: () => import('@/views/ModemCallsView.vue'),
        },
        {
          path: 'contacts',
          name: 'modem-contacts',
          component: () => import('@/views/ContactsView.vue'),
        },
        {
          path: 'contacts/:contactId',
          name: 'modem-contact',
          component: () => import('@/views/ContactsView.vue'),
        },
        {
          // Existing bookmarks and delivered notifications still resolve to Calls.
          path: 'phone',
          redirect: (to) => ({
            name: 'modem-calls',
            params: { id: to.params.id },
            query: to.query,
            hash: to.hash,
          }),
        },
        {
          path: 'settings',
          name: 'modem-settings',
          component: () => import('@/views/ModemSettingsView.vue'),
        },
        {
          path: 'settings/network',
          name: 'modem-settings-network',
          component: () => import('@/views/ModemSettingsNetworkView.vue'),
        },
        {
          path: 'settings/internet',
          name: 'modem-settings-internet',
          component: () => import('@/views/ModemSettingsInternetView.vue'),
        },
        {
          path: 'settings/device',
          name: 'modem-settings-device',
          component: () => import('@/views/ModemSettingsDeviceView.vue'),
        },
        {
          path: 'settings/wifi-calling',
          name: 'modem-settings-wifi-calling',
          component: () => import('@/views/ModemSettingsWiFiCallingView.vue'),
        },
        {
          path: 'settings/volte',
          name: 'modem-settings-volte',
          component: () => import('@/views/ModemSettingsVoLTEView.vue'),
        },
      ],
    },
  ],
})

const AUTH_ROUTE_NAME = 'auth'
const ACTIVATE_ROUTE_NAME = 'activate'

export const enforceRouteAccess = async (
  to: Pick<RouteLocationNormalized, 'name'>,
): Promise<NavigationGuardReturn> => {
  const authStore = useAuthStore()
  const authenticated = await authStore.check()
  if (!authenticated) {
    if (to.name !== AUTH_ROUTE_NAME) return { name: AUTH_ROUTE_NAME }
    return
  }
  if (to.name === AUTH_ROUTE_NAME) return { name: 'home' }

  const licenseStore = useLicenseStore()
  const previousMode = licenseStore.mode
  await licenseStore.check(true)
  if (previousMode !== licenseStore.mode) await useCapabilities().fetchCapabilities(true)
  if (to.name === ACTIVATE_ROUTE_NAME && licenseStore.authorized) return { name: 'home' }
}

router.beforeEach(enforceRouteAccess)

export default router
