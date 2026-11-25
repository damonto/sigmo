<script setup lang="ts">
import { computed } from 'vue'
import { Check, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import type { ConnectionProgress } from '@/types/connectionProgress'

const props = defineProps<{
  access: 'wifiCalling' | 'volte'
  connected: boolean
  progress?: ConnectionProgress
}>()

type NodeStatus = 'pending' | 'current' | 'completed' | 'failed'

const { t } = useI18n()

const milestones = {
  wifiCalling: [
    { label: 'ePDG', stages: ['epdg_resolve', 'epdg_ike_init'] },
    { label: 'AKA', stages: ['epdg_eap_aka'] },
    { label: 'IPsec', stages: ['epdg_ipsec'] },
    {
      label: 'IMS',
      stages: [
        'ims_transport',
        'ims_registration_slot',
        'ims_register_challenge',
        'ims_aka',
        'ims_ipsec',
        'ims_register_final',
        'ims_starting',
        'ims_disconnected',
      ],
    },
  ],
  volte: [
    { label: 'LTE', stages: ['lte_packet_service', 'volte_data_path'] },
    { label: 'IMS PDN', stages: ['ims_pdn'] },
    {
      label: 'AKA',
      stages: ['ims_transport', 'ims_registration_slot', 'ims_register_challenge', 'ims_aka'],
    },
    {
      label: 'IMS',
      stages: ['ims_ipsec', 'ims_register_final', 'ims_starting', 'ims_disconnected'],
    },
  ],
}

const activeNode = computed((): { index: number; status: NodeStatus } => {
  const { progress } = props
  if (!progress) return { index: -1, status: 'pending' }

  const group = milestones[props.access]
  const indexFor = (stage: string) => group.findIndex((node) => node.stages.includes(stage))
  const index = indexFor(progress.stage)
  if (index >= 0) return { index, status: progress.status === 'failed' ? 'failed' : 'current' }

  // Retry waits have no milestone of their own. Retain the last node reached in this attempt.
  for (const step of progress.steps.slice().reverse()) {
    const previousIndex = indexFor(step.stage)
    if (previousIndex < 0) continue
    if (step.status === 'failed') return { index: previousIndex, status: 'failed' }
    if (step.status === 'completed') return { index: previousIndex, status: 'completed' }
    return { index: previousIndex, status: 'current' }
  }
  return { index: -1, status: 'pending' }
})

const nodes = computed(() => {
  const ready = props.progress ? props.progress.status === 'ready' : props.connected
  const { index: activeIndex, status } = activeNode.value
  return milestones[props.access].map<{ label: string; status: NodeStatus }>(
    ({ label }, index) => ({
      label,
      status:
        ready || index < activeIndex ? 'completed' : index === activeIndex ? status : 'pending',
    }),
  )
})

const markerClasses = {
  pending: 'border-border bg-card',
  current: 'border-primary bg-card text-primary ring-2 ring-primary/20',
  completed:
    'border-emerald-600 bg-emerald-600 text-white dark:border-emerald-400 dark:bg-emerald-400 dark:text-emerald-950',
  failed: 'border-destructive bg-destructive text-white',
}
</script>

<template>
  <ol
    class="mt-1 flex w-full flex-wrap items-center gap-x-1.5 gap-y-1"
    role="list"
    :aria-label="t('modemDetail.settings.imsProgress.label')"
  >
    <li
      v-for="(node, index) in nodes"
      :key="node.label"
      class="flex min-w-max flex-auto items-center gap-1 last:flex-none"
      :aria-current="node.status === 'current' ? 'step' : undefined"
      :title="t(`modemDetail.settings.imsProgress.${node.status}`)"
    >
      <span
        aria-hidden="true"
        class="grid size-3 shrink-0 place-items-center rounded-full border"
        :class="markerClasses[node.status]"
      >
        <Check
          v-if="node.status === 'completed'"
          class="size-2"
        />
        <X
          v-else-if="node.status === 'failed'"
          class="size-2"
        />
        <span
          v-else-if="node.status === 'current'"
          class="size-1.5 rounded-full bg-current"
        />
      </span>
      <span
        class="text-xs leading-none font-medium"
        :class="node.status === 'pending' ? 'text-muted-foreground' : 'text-foreground'"
      >
        {{ node.label }}
      </span>
      <span class="sr-only">{{ t(`modemDetail.settings.imsProgress.${node.status}`) }}</span>
      <span
        v-if="index < nodes.length - 1"
        aria-hidden="true"
        class="h-px min-w-1 flex-1"
        :class="
          node.status === 'completed' && nodes[index + 1]?.status !== 'pending'
            ? 'bg-emerald-600/45 dark:bg-emerald-400/45'
            : 'bg-border'
        "
      />
    </li>
  </ol>
</template>
