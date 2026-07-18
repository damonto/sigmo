import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import { defineConfig } from 'vite'
import { VitePWA } from 'vite-plugin-pwa'
import vueDevTools from 'vite-plugin-vue-devtools'

// https://vite.dev/config/
export default defineConfig({
  build: {
    rolldownOptions: {
      output: {
        advancedChunks: {
          groups: [
            {
              name: 'vue-vendor',
              test: /[\\/]node_modules[\\/](?:@intlify[\\/]|@vue[\\/]|pinia[\\/]|vue(?:-i18n|-router)?[\\/])/,
            },
            {
              name: 'ui-vendor',
              test: /[\\/]node_modules[\\/](?:@floating-ui[\\/]|@vueuse[\\/]|aria-hidden[\\/]|class-variance-authority[\\/]|clsx[\\/]|reka-ui[\\/]|tailwind-merge[\\/]|vue-sonner[\\/])/,
            },
            {
              name: 'phone-vendor',
              test: /[\\/]node_modules[\\/]libphonenumber-js[\\/]/,
            },
          ],
        },
      },
    },
  },
  plugins: [
    vue(),
    vueJsx(),
    vueDevTools(),
    tailwindcss(),
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
      injectRegister: null,
      manifest: false,
      injectManifest: {
        globPatterns: [
          '**/*.{css,html,js,woff2}',
          'favicon.png',
          'icons/*.png',
          'manifest.webmanifest',
        ],
      },
      devOptions: {
        enabled: true,
        type: 'module',
        navigateFallback: 'index.html',
      },
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
})
