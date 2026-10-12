<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import HomeModemCard from '@/components/home/HomeModemCard.vue'
import HomeModemSkeletonList from '@/components/home/HomeModemSkeletonList.vue'
import { Card, CardContent } from '@/components/ui/card'
import type { HomeModemItem } from '@/types/home'

const props = defineProps<{
  items: HomeModemItem[]
  isLoading: boolean
}>()

const { t } = useI18n()

const hasItems = computed(() => props.items.length > 0)
</script>

<template>
  <HomeModemSkeletonList v-if="props.isLoading" />

  <div v-else-if="hasItems" class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
    <RouterLink
      v-for="item in props.items"
      :key="item.id"
      :to="{ name: 'modem-detail', params: { id: item.id } }"
      class="group block min-w-0"
    >
      <HomeModemCard v-bind="item" />
    </RouterLink>
  </div>

  <Card v-else class="border-0 shadow-sm">
    <CardContent class="py-10 text-center">
      <p class="text-sm text-muted-foreground">
        {{ t('home.noModems') }}
      </p>
    </CardContent>
  </Card>
</template>
