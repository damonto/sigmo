<script setup lang="ts">
import { RouterView } from 'vue-router'
import 'vue-sonner/style.css'

import ModemCallProvider from '@/components/modem/ModemCallProvider.vue'
import { Toaster } from '@/components/ui/sonner'
</script>

<template>
  <ModemCallProvider>
    <RouterView />
  </ModemCallProvider>
  <Toaster
    position="top-center"
    rich-colors
  />
</template>
