import { defineComponent, nextTick, ref, type Ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type {
  CreateMCPAPIKeyResponse,
  MCPAPIKeysResponse,
  MCPAuditEventsResponse,
  MCPSettings,
} from '@/types/mcp'
import SettingsMCPView from '@/views/SettingsMCPView.vue'

const mcpApi = vi.hoisted(() => ({
  createAPIKey: vi.fn(),
  downloadSkill: vi.fn(),
  getAPIKeys: vi.fn(),
  getAuditEvents: vi.fn(),
  getSettings: vi.fn(),
  revokeAPIKey: vi.fn(),
  updateSettings: vi.fn(),
}))

const modemApi = vi.hoisted(() => ({
  getModems: vi.fn(),
}))

const clipboard = vi.hoisted(() => ({
  write: vi.fn(),
}))

const toast = vi.hoisted(() => ({
  success: vi.fn(),
}))

vi.mock('@/apis/mcp', () => ({ useMCPApi: () => mcpApi }))
vi.mock('@/apis/modem', () => ({ useModemApi: () => modemApi }))
vi.mock('@/lib/clipboard', () => ({ writeClipboardText: clipboard.write }))
vi.mock('vue-sonner', () => ({ toast }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: ref('en-US'),
    t: (key: string) => key,
  }),
}))

const apiResult = <T>(value: T): Promise<{ data: Ref<T> }> =>
  Promise.resolve({ data: ref(value) as Ref<T> })

const settings = (enabled = false): MCPSettings => ({
  enabled,
  endpointPath: '/mcp',
  auditRetentionDays: 90,
  permissions: [
    { name: 'modem.read', module: 'modem' },
    { name: 'volte.read', module: 'volte' },
    { name: 'calls.read', module: 'calls' },
    { name: 'calls.delete', module: 'calls' },
  ],
})

const keys: MCPAPIKeysResponse = {
  apiKeys: [
    {
      id: 'key-1',
      name: 'Existing agent',
      tokenHint: 'sigmo_mcp_…123456',
      status: 'active',
      allModems: true,
      modemIds: [],
      permissions: ['calls.read'],
      createdAt: '2026-07-19T00:00:00Z',
      expiresAt: '2026-08-18T00:00:00Z',
    },
  ],
}

const audit: MCPAuditEventsResponse = {
  events: [
    {
      id: 1,
      keyId: 'key-1',
      keyName: 'Existing agent',
      tool: 'list_calls',
      modemIds: ['imei-a'],
      outcome: 'success',
      durationMs: 4,
      createdAt: '2026-07-19T00:00:00Z',
    },
  ],
}

const SlotStub = defineComponent({ template: '<div><slot /></div>' })
const ButtonStub = defineComponent({
  inheritAttrs: false,
  props: { disabled: Boolean },
  emits: ['click'],
  template:
    '<button v-bind="$attrs" type="button" :disabled="disabled" @click="$emit(\'click\', $event)"><slot /></button>',
})
const InputStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: [String, Number], disabled: Boolean },
  emits: ['update:model-value'],
  template:
    '<input v-bind="$attrs" :value="modelValue" :disabled="disabled" @input="$emit(\'update:model-value\', $event.target.value)" />',
})
const ToggleStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: Boolean, disabled: Boolean },
  emits: ['update:model-value'],
  template:
    '<button v-bind="$attrs" type="button" :disabled="disabled" @click="$emit(\'update:model-value\', !modelValue)" />',
})
const DialogStub = defineComponent({
  name: 'DialogStub',
  props: { open: Boolean },
  emits: ['update:open'],
  template:
    '<section v-if="open" data-dialog><button data-testid="close-dialog" @click="$emit(\'update:open\', false)" /><slot /></section>',
})
const SettingsHeaderStub = defineComponent({
  props: { title: String, description: String },
  template: '<header>{{ title }} {{ description }}</header>',
})

const mountView = () =>
  mount(SettingsMCPView, {
    global: {
      stubs: {
        Alert: SlotStub,
        AlertDescription: SlotStub,
        AlertDialog: DialogStub,
        AlertDialogAction: ButtonStub,
        AlertDialogCancel: ButtonStub,
        AlertDialogContent: SlotStub,
        AlertDialogDescription: SlotStub,
        AlertDialogFooter: SlotStub,
        AlertDialogHeader: SlotStub,
        AlertDialogTitle: SlotStub,
        AlertTitle: SlotStub,
        Badge: SlotStub,
        Button: ButtonStub,
        Card: SlotStub,
        CardContent: SlotStub,
        CardDescription: SlotStub,
        CardHeader: SlotStub,
        CardTitle: SlotStub,
        Checkbox: ToggleStub,
        Dialog: DialogStub,
        DialogContent: SlotStub,
        DialogDescription: SlotStub,
        DialogFooter: SlotStub,
        DialogHeader: SlotStub,
        DialogTitle: SlotStub,
        Input: InputStub,
        Label: SlotStub,
        SettingsHeader: SettingsHeaderStub,
        Spinner: SlotStub,
        Switch: ToggleStub,
      },
    },
  })

describe('SettingsMCPView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mcpApi.getSettings.mockImplementation(() => apiResult(settings()))
    mcpApi.getAPIKeys.mockImplementation(() => apiResult(keys))
    mcpApi.getAuditEvents.mockImplementation(() => apiResult(audit))
    modemApi.getModems.mockImplementation(() =>
      apiResult([{ id: 'imei-a', name: 'Modem A', manufacturer: '', model: '' }]),
    )
    mcpApi.updateSettings.mockImplementation((enabled: boolean) => apiResult(settings(enabled)))
    mcpApi.revokeAPIKey.mockImplementation(() => apiResult(undefined))
    mcpApi.downloadSkill.mockResolvedValue(new Blob(['skill']))
    clipboard.write.mockResolvedValue(undefined)
  })

  it('loads dynamic Pro permissions, toggles MCP, copies the endpoint, and shows audits', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="open-mcp-key-form"]').trigger('click')
    await nextTick()
    expect(wrapper.text()).toContain('settings.mcp.permissionLabels.calls_read')
    expect(wrapper.text()).toContain('settings.mcp.permissionLabels.calls_delete')
    expect(wrapper.text()).not.toContain('calls_dial')
    expect(wrapper.text()).toContain('list_calls')

    await wrapper.get('[data-testid="mcp-enabled"]').trigger('click')
    await flushPromises()
    expect(mcpApi.updateSettings).toHaveBeenCalledWith(true)
    expect(toast.success).toHaveBeenCalledWith('settings.mcp.saved')

    await wrapper.get('[data-testid="copy-mcp-endpoint"]').trigger('click')
    expect(clipboard.write).toHaveBeenCalledWith('http://localhost:3000/mcp')

    await wrapper.get('[data-testid="copy-mcp-example-config"]').trigger('click')
    expect(clipboard.write).toHaveBeenCalledWith(
      expect.stringContaining('"Authorization": "Bearer <API_KEY>"'),
    )
  })

  it('creates a scoped key, shows the secret once, and revokes a key', async () => {
    const created: CreateMCPAPIKeyResponse = {
      apiKey: {
        id: 'key-2',
        name: 'Call audit agent',
        tokenHint: 'sigmo_mcp_…abcdef',
        status: 'active',
        allModems: true,
        modemIds: [],
        permissions: ['calls.read'],
        createdAt: '2026-07-19T00:00:00Z',
        expiresAt: '2026-08-18T00:00:00Z',
      },
      token: 'sigmo_mcp_secret',
    }
    mcpApi.createAPIKey.mockImplementation(() => apiResult(created))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="open-mcp-key-form"]').trigger('click')
    await nextTick()
    await wrapper.get('#mcp-key-name').setValue('Call audit agent')
    const toggleAll = wrapper.get('[data-testid="toggle-all-mcp-permissions"]')
    await toggleAll.trigger('click')
    expect(toggleAll.text()).toBe('settings.mcp.clearAllPermissions')
    await toggleAll.trigger('click')
    expect(toggleAll.text()).toBe('settings.mcp.selectAllPermissions')
    await toggleAll.trigger('click')
    await wrapper.get('[data-testid="create-mcp-key"]').trigger('click')
    await flushPromises()

    expect(mcpApi.createAPIKey).toHaveBeenCalledWith({
      name: 'Call audit agent',
      validityDays: 30,
      allModems: true,
      modemIds: [],
      permissions: ['modem.read', 'volte.read', 'calls.read', 'calls.delete'],
    })
    expect(wrapper.get('[data-testid="mcp-secret"]').attributes('value')).toBe('sigmo_mcp_secret')
    await wrapper.get('[data-testid="copy-mcp-secret"]').trigger('click')
    expect(clipboard.write).toHaveBeenCalledWith('sigmo_mcp_secret')

    await wrapper.get('[data-testid="close-dialog"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="mcp-secret"]').exists()).toBe(false)

    await wrapper.get('[data-testid="revoke-mcp-key"]').trigger('click')
    await nextTick()
    await wrapper.get('[data-testid="confirm-revoke-mcp-key"]').trigger('click')
    await flushPromises()
    expect(mcpApi.revokeAPIKey).toHaveBeenCalledWith('key-2')
    expect(toast.success).toHaveBeenCalledWith('settings.mcp.revoked')
  })
})
