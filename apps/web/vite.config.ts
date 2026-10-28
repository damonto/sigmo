import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import { defineConfig, loadEnv } from 'vite'
import { VitePWA } from 'vite-plugin-pwa'
import vueDevTools from 'vite-plugin-vue-devtools'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, fileURLToPath(new URL('.', import.meta.url)), 'SIGMO_')
  return {
    server: {
      proxy: {
        '/api': {
          target: env.SIGMO_DEV_BACKEND || 'http://127.0.0.1:9527',
          // Preserve Host and Origin so the backend applies its normal CSRF checks.
          changeOrigin: false,
          ws: true,
        },
      },
    },
    build: {
      rolldownOptions: {
        output: {
          codeSplitting: {
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
        integration: {
          configureCustomSWViteBuild(config) {
            const output = config.build?.rollupOptions?.output
            if (!output || Array.isArray(output)) return

            // vite-plugin-pwa 2 sets this legacy option for the service worker build.
            delete output.inlineDynamicImports
            output.codeSplitting = false
          },
        },
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
  }
})
