import { afterEach, describe, expect, it, vi } from 'vitest';

import { editTelegramMessage, sendTelegram } from '../src/telegram/client';
import { MAX_JSON_BODY_SIZE } from '../src/http/body';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('Telegram client', () => {
  it('cancels oversized upstream JSON before buffering the entire body', async () => {
    const encoder = new TextEncoder();
    const chunks = [
      encoder.encode('{"ok":true,"result":"'),
      encoder.encode('a'.repeat(MAX_JSON_BODY_SIZE)),
      encoder.encode('"}'),
    ];
    let cancelled = false;
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        new ReadableStream<Uint8Array>({
          pull(controller) {
            const chunk = chunks.shift();
            if (chunk) {
              controller.enqueue(chunk);
            } else {
              controller.close();
            }
          },
          cancel() {
            cancelled = true;
          },
        }),
        { headers: { 'content-type': 'application/json' } },
      ),
    );

    await expect(
      sendTelegram('test-token', 1234, { text: 'Hello' }),
    ).rejects.toThrow('invalid response');
    expect(cancelled).toBe(true);
  });

  it.each([301, 302, 303, 307, 308])(
    'rejects HTTP %s redirects',
    async (status) => {
      const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        new Response(null, {
          status,
          headers: { location: 'https://unexpected.example' },
        }),
      );

      await expect(
        sendTelegram('test-token', 1234, { text: 'Hello' }),
      ).rejects.toThrow(`HTTP ${status}`);
      expect(fetchMock.mock.calls[0][1]?.redirect).toBe('manual');
      expect(fetchMock).toHaveBeenCalledTimes(1);
    },
  );

  it('bounds network requests with a deadline', async () => {
    const signal = AbortSignal.abort(
      new DOMException('deadline', 'TimeoutError'),
    );
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(signal);
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      new Request(input, init).signal.throwIfAborted();
      return Response.json({ ok: true });
    });

    await expect(
      sendTelegram('test-token', 1234, { text: 'Hello' }),
    ).rejects.toThrow('timed out');
  });

  it('retries one rate-limited request', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        Response.json(
          {
            ok: false,
            description: 'Too Many Requests',
            parameters: { retry_after: 0 },
          },
          { status: 429 },
        ),
      )
      .mockResolvedValueOnce(Response.json({ ok: true }));

    await sendTelegram('test-token', 1234, { text: 'Hello' });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    const request = new Request(
      fetchMock.mock.calls[0][0],
      fetchMock.mock.calls[0][1],
    );
    await expect(request.json()).resolves.toMatchObject({
      chat_id: 1234,
      text: 'Hello',
      parse_mode: 'MarkdownV2',
    });
  });

  it('does not wait beyond the background task budget', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      Response.json(
        {
          ok: false,
          description: 'Too Many Requests',
          parameters: { retry_after: 11 },
        },
        { status: 429 },
      ),
    );

    await expect(
      sendTelegram('test-token', 1234, { text: 'Hello' }),
    ).rejects.toThrow('retry delay 11s exceeds limit');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('retries a rate-limited request only once', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async () =>
        Response.json(
          {
            ok: false,
            description: 'Too Many Requests',
            parameters: { retry_after: 0 },
          },
          { status: 429 },
        ),
      );

    await expect(
      sendTelegram('test-token', 1234, { text: 'Hello' }),
    ).rejects.toThrow('HTTP 429: Too Many Requests');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('reports Telegram API rejections separately from HTTP failures', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      Response.json({
        ok: false,
        description: 'Bad Request: message is not modified',
      }),
    );

    await expect(
      sendTelegram('test-token', 1234, { text: 'Hello' }),
    ).rejects.toThrow(
      'Telegram sendMessage rejected the request: Bad Request: message is not modified',
    );
  });

  it('rejects oversized messages before sending', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch');

    await expect(
      sendTelegram('test-token', 1234, { text: 'A'.repeat(4_001) }),
    ).rejects.toThrow('exceeds size limit');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('clears stale inline buttons when editing a message', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(Response.json({ ok: true }));

    await editTelegramMessage('test-token', 1234, 42, { text: 'Last page' });

    const request = new Request(
      fetchMock.mock.calls[0][0],
      fetchMock.mock.calls[0][1],
    );
    expect(new URL(request.url).pathname).toBe(
      '/bottest-token/editMessageText',
    );
    await expect(request.json()).resolves.toMatchObject({
      chat_id: 1234,
      message_id: 42,
      text: 'Last page',
      parse_mode: 'MarkdownV2',
      reply_markup: { inline_keyboard: [] },
    });
  });
});
