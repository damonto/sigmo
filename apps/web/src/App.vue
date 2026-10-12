<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { RouterView } from 'vue-router'
import 'vue-sonner/style.css'

import ModemCallProvider from '@/components/modem/ModemCallProvider.vue'
import { Toaster } from '@/components/ui/sonner'
import { useLicenseStore } from '@/stores/license'

const { businessEnabled } = storeToRefs(useLicenseStore())
</script>

<template>
  <ModemCallProvider v-if="businessEnabled">
    <RouterView />
  </ModemCallProvider>
  <RouterView v-else />
  <Toaster
    position="top-center"
    rich-colors
  />
</template>
