import { createRouter, createWebHistory } from 'vue-router'

import { getStoredToken } from '@/lib/authStorage'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
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

router.beforeEach(async (to) => {
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
})

export default router
