import './assets/main.css'
import 'flag-icons/css/flag-icons.min.css'

import { createPinia } from 'pinia'
import { createApp } from 'vue'

import App from './App.vue'
import { useTheme } from './composables/useTheme'
import i18n from './i18n'
import { bootstrapWebPush } from './lib/webPush'
import router from './router'

useTheme()

const app = createApp(App)

app.use(createPinia())
app.use(i18n)
app.use(router)

app.mount('#app')

void bootstrapWebPush()
