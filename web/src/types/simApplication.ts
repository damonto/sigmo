export type SimApplicationMenuItem = {
  id: number
  label: string
}

export type SimApplicationMenu = {
  kind: 'root' | 'select-item'
  title?: string
  items: SimApplicationMenuItem[]
  defaultItemId?: number
  helpAvailable?: boolean
}

export type SimApplicationView =
  | { type: 'menu'; menu: SimApplicationMenu }
  | {
      type: 'display_text'
      text: string
      highPriority: boolean
      userClear: boolean
      immediateResponse: boolean
    }
  | {
      type: 'input'
      text: string
      defaultText?: string
      minLength: number
      maxLength: number
      hideInput: boolean
      helpAvailable: boolean
    }
  | {
      type: 'inkey'
      text: string
      yesNo: boolean
      helpAvailable: boolean
    }
  | {
      type: 'confirm'
      command: string
      text: string
    }
