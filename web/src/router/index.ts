import { createRouter, createWebHistory } from 'vue-router'

import { getStoredToken } from '@/lib/auth-storage'
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
      path: '/messages',
      name: 'messages',
      component: () => import('@/views/MessagesView.vue'),
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
          path: 'ussd',
          name: 'modem-ussd',
          component: () => import('@/views/ModemUssdView.vue'),
        },
        {
          path: 'settings',
          name: 'modem-settings',
          component: () => import('@/views/ModemSettingsView.vue'),
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
