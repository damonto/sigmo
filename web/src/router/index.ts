import {
  createRouter,
  createWebHistory,
  type NavigationGuardReturn,
  type RouteLocationNormalized,
} from 'vue-router'

import { getStoredToken } from '@/lib/authStorage'
import { useAuthStore } from '@/stores/auth'
import { useLicenseStore } from '@/stores/license'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/activate',
      name: 'activate',
      component: () => import('@/views/ActivateView.vue'),
    },
    {
      path: '/unavailable',
      name: 'license-unavailable',
      component: () => import('@/views/LicenseUnavailableView.vue'),
    },
    {
      path: '/auth',
      name: 'auth',
      component: () => import('@/views/AuthVerifyView.vue'),
    },
    {
      path: '/',
      name: 'home',
      component: () => import('@/views/HomeView.vue'),
    },
    {
      path: '/settings',
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
          path: 'phone',
          name: 'modem-phone',
          component: () => import('@/views/ModemPhoneView.vue'),
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
const LICENSE_UNAVAILABLE_ROUTE_NAME = 'license-unavailable'

export const enforceRouteAccess = async (
  to: Pick<RouteLocationNormalized, 'name'>,
): Promise<NavigationGuardReturn> => {
  const licenseStore = useLicenseStore()
  await licenseStore.check(true)
  if (licenseStore.unavailable) {
    if (to.name !== LICENSE_UNAVAILABLE_ROUTE_NAME) {
      return { name: LICENSE_UNAVAILABLE_ROUTE_NAME }
    }
    return
  }
  if (to.name === LICENSE_UNAVAILABLE_ROUTE_NAME) return { name: 'home' }
  if (licenseStore.activationRequired) {
    if (to.name !== ACTIVATE_ROUTE_NAME) return { name: ACTIVATE_ROUTE_NAME }
    return
  }
  if (to.name === ACTIVATE_ROUTE_NAME) return { name: 'home' }

  const token = getStoredToken()
  if (token && to.name === AUTH_ROUTE_NAME) {
    return { name: 'home' }
  }

  if (token) {
    return
  }

  const authStore = useAuthStore()
  const otpRequired = await authStore.fetchOtpRequirement()
  if (!otpRequired) {
    if (to.name === AUTH_ROUTE_NAME) {
      return { name: 'home' }
    }
    return
  }

  if (to.name !== AUTH_ROUTE_NAME) {
    return { name: AUTH_ROUTE_NAME }
  }
}

router.beforeEach(enforceRouteAccess)

export default router
