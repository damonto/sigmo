import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

import { resolveAPIWebSocketURL } from '@/lib/apiUrl'
import { getStoredToken } from '@/lib/authStorage'
import type {
  SimApplicationMenu,
  SimApplicationMenuItem,
  SimApplicationView,
} from '@/types/simApplication'

type ServerMessage = {
  type: string
  available?: boolean
  profileIccid?: string
  menu?: SimApplicationMenu
  kind?: SimApplicationMenu['kind']
  title?: string
  items?: SimApplicationMenuItem[]
  defaultItemId?: number
  helpAvailable?: boolean
  text?: string
  defaultText?: string
  minLength?: number
  maxLength?: number
  hideInput?: boolean
  yesNo?: boolean
  highPriority?: boolean
  userClear?: boolean
  immediateResponse?: boolean
  command?: string
  message?: string
}

const messageToMenu = (message: ServerMessage): SimApplicationMenu | null => {
  if (message.menu) return message.menu
  if (message.kind !== 'root' && message.kind !== 'select-item') return null
  return {
    kind: message.kind,
    title: message.title,
    items: message.items ?? [],
    defaultItemId: message.defaultItemId,
    helpAvailable: message.helpAvailable,
  }
}

export const useSimApplicationSession = (modemId: Ref<string>) => {
  const available = ref(false)
  const profileIccid = ref('')
  const rootMenu = ref<SimApplicationMenu | null>(null)
  const currentView = ref<SimApplicationView | null>(null)
  const dialogOpen = ref(false)
  const errorMessage = ref('')

  let ws: WebSocket | null = null

  const resetState = () => {
    available.value = false
    profileIccid.value = ''
    rootMenu.value = null
    currentView.value = null
    dialogOpen.value = false
  }

  const clearError = () => {
    errorMessage.value = ''
  }

  const close = () => {
    if (!ws) return
    const current = ws
    ws = null
    current.onclose = null
    current.close()
  }

  const fail = (message?: string) => {
    close()
    resetState()
    errorMessage.value = message?.trim() || 'SIM Application session stopped'
  }

  const send = (payload: object) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return
    ws.send(JSON.stringify(payload))
  }

  const returnToRoot = () => {
    currentView.value = rootMenu.value ? { type: 'menu', menu: rootMenu.value } : null
    if (!currentView.value) {
      dialogOpen.value = false
    }
  }

  const handleStatus = (message: ServerMessage) => {
    profileIccid.value = message.profileIccid ?? profileIccid.value
    if (message.available === undefined) return

    available.value = message.available
    const menu = messageToMenu(message)
    rootMenu.value = message.available ? menu : null
    if (!message.available) {
      currentView.value = null
      dialogOpen.value = false
      return
    }
    if (dialogOpen.value && rootMenu.value) {
      currentView.value = { type: 'menu', menu: rootMenu.value }
    }
  }

  const handleMenu = (message: ServerMessage) => {
    const menu = messageToMenu(message)
    if (!menu) return
    if (menu.kind === 'root') {
      rootMenu.value = menu
      if (dialogOpen.value) {
        currentView.value = { type: 'menu', menu }
      }
      return
    }
    currentView.value = { type: 'menu', menu }
    dialogOpen.value = true
  }

  const handleMessage = (message: ServerMessage) => {
    switch (message.type) {
      case 'status':
        handleStatus(message)
        return
      case 'menu':
        handleMenu(message)
        return
      case 'display_text':
        currentView.value = {
          type: 'display_text',
          text: message.text ?? '',
          highPriority: Boolean(message.highPriority),
          userClear: Boolean(message.userClear),
          immediateResponse: Boolean(message.immediateResponse),
        }
        dialogOpen.value = true
        return
      case 'input':
        currentView.value = {
          type: 'input',
          text: message.text ?? '',
          defaultText: message.defaultText,
          minLength: message.minLength ?? 0,
          maxLength: message.maxLength ?? 255,
          hideInput: Boolean(message.hideInput),
          helpAvailable: Boolean(message.helpAvailable),
        }
        dialogOpen.value = true
        return
      case 'inkey':
        currentView.value = {
          type: 'inkey',
          text: message.text ?? '',
          yesNo: Boolean(message.yesNo),
          helpAvailable: Boolean(message.helpAvailable),
        }
        dialogOpen.value = true
        return
      case 'confirm':
        currentView.value = {
          type: 'confirm',
          command: message.command ?? '',
          text: message.text ?? '',
        }
        dialogOpen.value = true
        return
      case 'error':
        fail(message.message)
        return
      default:
        return
    }
  }

  const start = (id: string) => {
    close()
    resetState()
    clearError()
    if (!id || id === 'unknown') return

    const conn = new WebSocket(
      resolveAPIWebSocketURL(`modems/${id}/sim-application/sessions`, getStoredToken()),
    )
    ws = conn
    conn.onmessage = (event) => {
      if (ws !== conn) return
      try {
        handleMessage(JSON.parse(event.data) as ServerMessage)
      } catch {
        fail()
      }
    }
    conn.onerror = () => {
      if (ws !== conn) return
      fail()
    }
    conn.onclose = () => {
      if (ws !== conn) return
      ws = null
      available.value = false
      currentView.value = null
      dialogOpen.value = false
    }
  }

  const openRootMenu = () => {
    if (!available.value || !rootMenu.value) return
    currentView.value = { type: 'menu', menu: rootMenu.value }
    dialogOpen.value = true
  }

  const selectMenuItem = (item: SimApplicationMenuItem, helpRequested = false) => {
    send({ type: 'menu_selection', itemId: item.id, helpRequested })
    returnToRoot()
  }

  const submitInput = (text: string) => {
    send({ type: 'input_response', text })
    returnToRoot()
  }

  const submitInkey = (text: string) => {
    send({ type: 'inkey_response', text })
    returnToRoot()
  }

  const respondConfirm = (accepted: boolean) => {
    send({ type: 'confirm_response', accepted })
    returnToRoot()
  }

  const back = () => {
    send({ type: 'back' })
    returnToRoot()
  }

  watch(
    modemId,
    (id) => {
      start(id)
    },
    { immediate: true },
  )

  onBeforeUnmount(() => {
    close()
  })

  return {
    available,
    profileIccid,
    currentView,
    dialogOpen,
    errorMessage,
    openRootMenu,
    selectMenuItem,
    submitInput,
    submitInkey,
    respondConfirm,
    back,
  }
}
