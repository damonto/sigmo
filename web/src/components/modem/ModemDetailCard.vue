<script setup lang="ts">
import { computed } from 'vue'

import RegionFlag from '@/components/RegionFlag.vue'
import { Badge } from '@/components/ui/badge'
import type { EuiccApiResponse } from '@/types/euicc'
import type { Modem } from '@/types/modem'

const props = defineProps<{
  modem: Modem
  euicc?: EuiccApiResponse | null
}>()

const formatBytes = (bytes?: number) => {
  if (bytes === null || bytes === undefined) return 'N/A'
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${Math.round((bytes / Math.pow(k, i)) * 100) / 100} ${sizes[i]}`
}

const storageRemaining = computed(() => formatBytes(props.euicc?.freeSpace))
const eid = computed(() => props.euicc?.eid || 'N/A')
const sasUpName = computed(() => props.euicc?.sasUp?.name || 'N/A')
const sasUpRegion = computed(() => props.euicc?.sasUp?.region ?? '')
const certificates = computed(() => props.euicc?.certificates ?? [])
</script>

<template>
  <div class="space-y-6">
    <!-- Basic Info -->
    <section class="space-y-3">
      <h2 class="text-base font-semibold text-foreground">Basic Information</h2>
      <div class="grid gap-3 text-sm">
        <div class="flex justify-between">
          <span class="text-muted-foreground">ID</span>
          <span class="font-mono">{{ modem.id }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Name</span>
          <span>{{ modem.name }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Manufacturer</span>
          <span>{{ modem.manufacturer }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Firmware Revision</span>
          <span class="font-mono">{{ modem.firmwareRevision }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Hardware Revision</span>
          <span class="font-mono">{{ modem.hardwareRevision }}</span>
        </div>
        <div v-if="modem.number" class="flex justify-between">
          <span class="text-muted-foreground">Number</span>
          <span class="font-mono">{{ modem.number }}</span>
        </div>
      </div>
    </section>

    <section v-if="modem.supportsEsim" class="space-y-3">
      <h2 class="text-base font-semibold text-foreground">eUICC Information</h2>
      <div class="grid gap-3 text-sm">
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">EID</span>
          <span class="break-all font-mono">{{ eid }}</span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">Storage Remaining</span>
          <span class="font-semibold">
            {{ storageRemaining }}
          </span>
        </div>
        <div class="flex items-center justify-between gap-4">
          <span class="text-muted-foreground">SAS Accreditation</span>
          <span class="flex items-center justify-end gap-2 text-right">
            <span>{{ sasUpName }}</span>
            <RegionFlag :region-code="sasUpRegion" class="rounded-sm text-base" />
          </span>
        </div>
        <div class="flex flex-col gap-2">
          <span class="text-muted-foreground">Certificates</span>
          <div v-if="certificates.length" class="flex flex-col gap-1">
            <span
              v-for="(cert, index) in certificates"
              :key="index"
              class="rounded-md bg-muted px-2 py-1 text-xs text-foreground"
            >
              {{ cert }}
            </span>
          </div>
          <span v-else class="text-xs text-muted-foreground">N/A</span>
        </div>
      </div>
    </section>

    <!-- SIM Info -->
    <section class="space-y-3">
      <h2 class="text-base font-semibold text-foreground">SIM Information</h2>
      <div class="grid gap-3 text-sm">
        <div class="flex justify-between">
          <span class="text-muted-foreground">Operator</span>
          <span>{{ modem.sim.operatorName }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Operator ID</span>
          <span class="font-mono">{{ modem.sim.operatorIdentifier }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Region</span>
          <span class="flex items-center gap-2 font-mono">
            <RegionFlag :region-code="modem.sim.regionCode" class="rounded-sm text-base" />
          </span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">ICCID</span>
          <span class="font-mono">{{ modem.sim.identifier }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Active</span>
          <span>{{ modem.sim.active ? 'Yes' : 'No' }}</span>
        </div>
      </div>
    </section>

    <!-- Network Info -->
    <section class="space-y-3">
      <h2 class="text-base font-semibold text-foreground">Network Information</h2>
      <div class="grid gap-3 text-sm">
        <div class="flex justify-between">
          <span class="text-muted-foreground">Access Technology</span>
          <span>{{ modem.accessTechnology || 'N/A' }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Registration State</span>
          <span>{{ modem.registrationState }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Registered Operator</span>
          <span>{{ modem.registeredOperator.name }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Operator Code</span>
          <span class="font-mono">{{ modem.registeredOperator.code }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-muted-foreground">Signal Quality</span>
          <span class="font-mono">{{ modem.signalQuality }}%</span>
        </div>
      </div>
    </section>

    <!-- Features -->
    <section class="space-y-3">
      <h2 class="text-base font-semibold text-foreground">Features</h2>
      <div class="flex flex-wrap gap-2">
        <Badge v-if="modem.supportsEsim">eSIM Support</Badge>
        <Badge v-else variant="secondary">Physical SIM Only</Badge>
      </div>
    </section>
  </div>
</template>
