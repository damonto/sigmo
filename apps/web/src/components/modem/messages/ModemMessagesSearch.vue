<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import ModemSearchInput from '@/components/modem/ModemSearchInput.vue'

const model = defineModel<string>({ required: true })
const { t } = useI18n()
</script>

<template>
  <ModemSearchInput
    v-model="model"
    :placeholder="t('modemDetail.messages.searchPlaceholder')"
    :clear-label="t('modemDetail.messages.clearSearch')"
  />
</template>
