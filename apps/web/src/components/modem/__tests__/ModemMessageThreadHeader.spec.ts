import { defineComponent } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ModemMessageThreadHeader from '@/components/modem/messages/ModemMessageThreadHeader.vue'
import en from '@/i18n/locales/en'

enableAutoUnmount(afterEach)

const mountHeader = (disabled = false) => {
  const call = vi.fn()
  const remove = vi.fn()
  const wrapper = mount(
    defineComponent({
      components: { ModemMessageThreadHeader },
      setup: () => ({ call, remove, disabled }),
      template: `
        <ModemMessageThreadHeader title="Alice" :can-delete="!disabled" @delete="remove">
          <template #actions="{ actionComponent, activateEvent }">
            <component :is="actionComponent" :disabled="disabled" @[activateEvent]="call">
              Call
            </component>
          </template>
        </ModemMessageThreadHeader>
      `,
    }),
    {
      attachTo: document.body,
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })] },
    },
  )
  const openMenu = async () => {
    await wrapper.get('button[aria-label="More actions"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
  }
  const menuItem = (name: string) => {
    const item = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]')).find(
      (item) => item.textContent?.trim() === name,
    )
    if (!item) throw new Error(`missing ${name} menu item`)
    return item
  }
  return { wrapper, call, remove, openMenu, menuItem }
}

describe('message thread actions', () => {
  it('activates the shared action from its desktop button', async () => {
    const { wrapper, call } = mountHeader()
    const button = wrapper.findAll('button').find((button) => button.text() === 'Call')
    if (!button) throw new Error('missing call button')
    await button.trigger('click')
    expect(call).toHaveBeenCalledOnce()
  })

  it('activates the same action from the menu with the keyboard and closes it', async () => {
    const { call, remove, openMenu, menuItem } = mountHeader()
    await openMenu()
    menuItem('Call').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(call).toHaveBeenCalledOnce()
    expect(document.querySelector('[role="menu"]')).toBeNull()
    await openMenu()
    menuItem('Delete').click()
    await flushPromises()
    expect(remove).toHaveBeenCalledOnce()
  })

  it('preserves disabled actions in both button and menu presentations', async () => {
    const { wrapper, call, remove, openMenu, menuItem } = mountHeader(true)
    const button = wrapper.findAll('button').find((button) => button.text() === 'Call')
    expect(button?.attributes('disabled')).toBeDefined()
    await openMenu()
    const item = menuItem('Call')
    expect(item.getAttribute('aria-disabled')).toBe('true')
    item.click()
    item.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    menuItem('Delete').click()
    await flushPromises()
    expect(call).not.toHaveBeenCalled()
    expect(remove).not.toHaveBeenCalled()
    expect(document.querySelector('[role="menu"]')).not.toBeNull()
  })
})
