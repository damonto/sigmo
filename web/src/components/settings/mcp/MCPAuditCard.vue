<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { MCPAuditEvent } from '@/types/mcp'

const props = defineProps<{
  events: MCPAuditEvent[]
  retentionDays: number
}>()

const { t, locale } = useI18n()
const formatTime = (value: string) =>
  new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle>{{ t('settings.mcp.auditTitle') }}</CardTitle>
      <CardDescription>
        {{ t('settings.mcp.auditDescription', { days: props.retentionDays }) }}
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-2">
      <p v-if="props.events.length === 0" class="py-6 text-center text-sm text-muted-foreground">
        {{ t('settings.mcp.noAudit') }}
      </p>
      <div
        v-for="event in props.events"
        :key="event.id"
        class="flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm"
      >
        <div>
          <span class="font-mono">{{ event.tool }}</span>
          <span class="ml-2 text-muted-foreground">{{ event.keyName }}</span>
        </div>
        <div class="flex items-center gap-2">
          <Badge :variant="event.outcome === 'success' ? 'secondary' : 'destructive'">
            {{ t(`settings.mcp.outcome.${event.outcome}`) }}
          </Badge>
          <span class="text-xs text-muted-foreground">{{ formatTime(event.createdAt) }}</span>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
