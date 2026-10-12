<script setup lang="ts">
import { KeyRound } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import type { CreateMCPAPIKey, MCPPermissionGroup } from '@/types/mcp'
import type { Modem } from '@/types/modem'

type ModemOption = Pick<Modem, 'id' | 'name'>

const props = defineProps<{
  isCreating: boolean
  modems: ModemOption[]
  open: boolean
  permissionGroups: MCPPermissionGroup[]
}>()

const emit = defineEmits<{
  create: [payload: CreateMCPAPIKey]
  'update:open': [open: boolean]
}>()

const { t } = useI18n()
const name = ref('')
const validityDays = ref(30)
const allModems = ref(true)
const selectedModems = ref<string[]>([])
const selectedPermissions = ref<string[]>([])

const availablePermissions = computed(() =>
  props.permissionGroups.flatMap((group) => group.permissions),
)
const allPermissionsSelected = computed(
  () =>
    availablePermissions.value.length > 0 &&
    availablePermissions.value.every((permission) =>
      selectedPermissions.value.includes(permission),
    ),
)
const canCreate = computed(
  () =>
    name.value.trim() !== '' &&
    validityDays.value >= 1 &&
    validityDays.value <= 180 &&
    selectedPermissions.value.length > 0 &&
    (allModems.value || selectedModems.value.length > 0),
)

const permissionLabel = (permission: string) =>
  t(`settings.mcp.permissionLabels.${permission.split('.').join('_')}`)
const moduleLabel = (module: string) => t(`settings.mcp.modules.${module}`)
const togglePermission = (permission: string, enabled: boolean) => {
  selectedPermissions.value = enabled
    ? [...new Set([...selectedPermissions.value, permission])]
    : selectedPermissions.value.filter((value) => value !== permission)
}
const toggleAllPermissions = () => {
  selectedPermissions.value = allPermissionsSelected.value ? [] : [...availablePermissions.value]
}
const toggleModem = (id: string, enabled: boolean) => {
  selectedModems.value = enabled
    ? [...new Set([...selectedModems.value, id])]
    : selectedModems.value.filter((value) => value !== id)
}
const resetForm = () => {
  name.value = ''
  validityDays.value = 30
  allModems.value = true
  selectedModems.value = []
  selectedPermissions.value = []
}
const submit = () => {
  if (!canCreate.value) return
  emit('create', {
    name: name.value.trim(),
    validityDays: validityDays.value,
    allModems: allModems.value,
    modemIds: allModems.value ? [] : selectedModems.value,
    permissions: selectedPermissions.value,
  })
}

watch(
  () => props.open,
  (open) => {
    if (!open) resetForm()
  },
)
</script>

<template>
  <Dialog :open="props.open" @update:open="emit('update:open', $event)">
    <DialogContent class="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle>{{ t('settings.mcp.createKey') }}</DialogTitle>
        <DialogDescription>{{ t('settings.mcp.createDescription') }}</DialogDescription>
      </DialogHeader>

      <div class="space-y-4">
        <div class="space-y-2">
          <Label for="mcp-key-name">{{ t('settings.mcp.keyName') }}</Label>
          <Input id="mcp-key-name" v-model="name" maxlength="64" />
        </div>
        <div class="space-y-2">
          <Label for="mcp-validity">{{ t('settings.mcp.validityDays') }}</Label>
          <Input id="mcp-validity" v-model.number="validityDays" type="number" min="1" max="180" />
        </div>
        <div class="flex items-center justify-between rounded-lg border p-3">
          <div>
            <p class="text-sm font-medium">{{ t('settings.mcp.allModems') }}</p>
            <p class="text-xs text-muted-foreground">
              {{ t('settings.mcp.allModemsDescription') }}
            </p>
          </div>
          <Switch
            data-testid="mcp-all-modems"
            :model-value="allModems"
            @update:model-value="allModems = $event === true"
          />
        </div>
        <div v-if="!allModems" class="space-y-2">
          <Label>{{ t('settings.mcp.modems') }}</Label>
          <div class="grid gap-2 sm:grid-cols-2">
            <label
              v-for="modem in props.modems"
              :key="modem.id"
              class="flex items-center gap-2 rounded-lg border p-2 text-sm"
            >
              <Checkbox
                :model-value="selectedModems.includes(modem.id)"
                @update:model-value="toggleModem(modem.id, $event === true)"
              />
              <span>
                <span class="block">{{ modem.name }}</span>
                <span class="font-mono text-xs text-muted-foreground">{{ modem.id }}</span>
              </span>
            </label>
          </div>
        </div>
        <div class="space-y-3">
          <div class="flex items-center justify-between gap-3">
            <Label>{{ t('settings.mcp.permissions') }}</Label>
            <Button
              type="button"
              variant="outline"
              size="sm"
              data-testid="toggle-all-mcp-permissions"
              @click="toggleAllPermissions"
            >
              {{
                t(
                  allPermissionsSelected
                    ? 'settings.mcp.clearAllPermissions'
                    : 'settings.mcp.selectAllPermissions',
                )
              }}
            </Button>
          </div>
          <div
            v-for="group in props.permissionGroups"
            :key="group.module"
            class="rounded-lg border p-3"
          >
            <p class="mb-2 text-sm font-medium">{{ moduleLabel(group.module) }}</p>
            <div class="grid gap-2 sm:grid-cols-2">
              <label
                v-for="permission in group.permissions"
                :key="permission"
                :data-permission="permission"
                class="flex items-center gap-2 text-sm"
              >
                <Checkbox
                  :model-value="selectedPermissions.includes(permission)"
                  @update:model-value="togglePermission(permission, $event === true)"
                />
                {{ permissionLabel(permission) }}
              </label>
            </div>
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('update:open', false)">
          {{ t('settings.cancel') }}
        </Button>
        <Button
          data-testid="create-mcp-key"
          :disabled="!canCreate || props.isCreating"
          @click="submit"
        >
          <Spinner v-if="props.isCreating" class="size-4" />
          <KeyRound v-else class="size-4" />
          {{ t('settings.mcp.createKey') }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
