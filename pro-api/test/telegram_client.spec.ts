import { afterEach, describe, expect, it, vi } from "vitest";

import {
  editTelegramMessage,
  sendTelegram,
} from "../src/telegram_client";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Telegram client", () => {
  it("retries one rate-limited request", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        Response.json(
          {
            ok: false,
            description: "Too Many Requests",
            parameters: { retry_after: 0 },
          },
          { status: 429 },
        ),
      )
      .mockResolvedValueOnce(Response.json({ ok: true }));

    await sendTelegram("test-token", 1234, { text: "Hello" });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    const request = new Request(
      fetchMock.mock.calls[0][0],
      fetchMock.mock.calls[0][1],
    );
    await expect(request.json()).resolves.toMatchObject({
      chat_id: 1234,
      text: "Hello",
      parse_mode: "MarkdownV2",
    });
  });

  it("does not wait beyond the background task budget", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json(
        {
          ok: false,
          description: "Too Many Requests",
          parameters: { retry_after: 11 },
        },
        { status: 429 },
      ),
    );

    await expect(
      sendTelegram("test-token", 1234, { text: "Hello" }),
    ).rejects.toThrow("retry delay 11s exceeds limit");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("retries a rate-limited request only once", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        Response.json(
        {
          ok: false,
          description: "Too Many Requests",
          parameters: { retry_after: 0 },
        },
        { status: 429 },
      ),
    );

    await expect(
      sendTelegram("test-token", 1234, { text: "Hello" }),
    ).rejects.toThrow("HTTP 429: Too Many Requests");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("reports Telegram API rejections separately from HTTP failures", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        ok: false,
        description: "Bad Request: message is not modified",
      }),
    );

    await expect(
      sendTelegram("test-token", 1234, { text: "Hello" }),
    ).rejects.toThrow(
      "Telegram sendMessage rejected the request: Bad Request: message is not modified",
    );
  });

  it("rejects oversized messages before sending", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");

    await expect(
      sendTelegram("test-token", 1234, { text: "A".repeat(4_001) }),
    ).rejects.toThrow("exceeds size limit");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("clears stale inline buttons when editing a message", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(Response.json({ ok: true }));

    await editTelegramMessage("test-token", 1234, 42, { text: "Last page" });

    const request = new Request(
      fetchMock.mock.calls[0][0],
      fetchMock.mock.calls[0][1],
    );
    expect(new URL(request.url).pathname).toBe(
      "/bottest-token/editMessageText",
    );
    await expect(request.json()).resolves.toMatchObject({
      chat_id: 1234,
      message_id: 42,
      text: "Last page",
      parse_mode: "MarkdownV2",
      reply_markup: { inline_keyboard: [] },
    });
  });
});
