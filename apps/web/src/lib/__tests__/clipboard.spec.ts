import { afterEach, describe, expect, it, vi } from 'vitest'

import { writeClipboardText } from '../clipboard'

const originalExecCommand = document.execCommand

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()

  if (originalExecCommand) {
    Object.defineProperty(document, 'execCommand', {
      value: originalExecCommand,
      configurable: true,
    })
    return
  }

  Reflect.deleteProperty(document, 'execCommand')
})

describe('writeClipboardText', () => {
  it('uses the Clipboard API when available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', {
      clipboard: {
        writeText,
      },
    })

    await writeClipboardText('hello')

    expect(writeText).toHaveBeenCalledWith('hello')
  })

  it('falls back to execCommand when the Clipboard API is unavailable', async () => {
    const execCommand = vi.fn(() => true)
    vi.stubGlobal('navigator', {})
    Object.defineProperty(document, 'execCommand', {
      value: execCommand,
      configurable: true,
    })

    await writeClipboardText('fallback')

    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(document.querySelector('textarea')).toBeNull()
  })
})
