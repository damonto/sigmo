import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import ModemMessageBubble from '@/components/modem/messages/ModemMessageBubble.vue'
import type { ThreadMessageItem } from '@/composables/useModemMessageThread'

const messageItem = (incoming: boolean, status = 'sent'): ThreadMessageItem => ({
  key: incoming ? 'incoming-1' : 'outgoing-1',
  incoming,
  text: incoming ? 'hello' : '321321313',
  timestampLabel: '2:46 PM',
  status: incoming ? '' : status,
  wifiCalling: true,
})

describe('ModemMessageBubble', () => {
  it('keeps outgoing metadata readable on the primary bubble color', () => {
    const wrapper = mount(ModemMessageBubble, {
      props: {
        item: messageItem(false),
      },
    })

    const meta = wrapper.get('[data-testid="message-meta"]')

    expect(meta.classes()).toContain('text-primary-foreground/90')
    expect(meta.classes()).toContain('font-medium')
    expect(meta.text()).toContain('2:46 PM')
    expect(meta.text()).toContain('sent')
    expect(wrapper.get('svg').classes()).toContain('text-primary-foreground/90')
  })

  it('uses muted metadata on incoming messages', () => {
    const wrapper = mount(ModemMessageBubble, {
      props: {
        item: messageItem(true),
      },
    })

    const meta = wrapper.get('[data-testid="message-meta"]')

    expect(meta.classes()).toContain('text-muted-foreground')
    expect(meta.classes()).not.toContain('text-primary-foreground/90')
    expect(meta.text()).not.toContain('sent')
    expect(wrapper.get('svg').classes()).toContain('text-sky-500')
  })

  it('shows delivered status for outgoing messages', () => {
    const wrapper = mount(ModemMessageBubble, {
      props: {
        item: messageItem(false, 'delivered'),
      },
    })

    expect(wrapper.get('[data-testid="message-meta"]').text()).toContain('delivered')
  })
})
