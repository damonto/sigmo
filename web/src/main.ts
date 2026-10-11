import './assets/main.css'
import 'flag-icons/css/flag-icons.min.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'

import App from './App.vue'
import { useTheme } from './composables/useTheme'
import i18n from './i18n'
import { bootstrapWebPush } from './lib/webPush'
import { prepareCallClient } from './lib/callClient'
import router from './router'
import { useLicenseStore } from './stores/license'

useTheme()

const app = createApp(App)

app.use(createPinia())
app.use(i18n)
app.use(router)

void prepareCallClient().then(async () => {
  app.mount('#app')
  await router.isReady()
  if (useLicenseStore().businessEnabled) await bootstrapWebPush()
})
