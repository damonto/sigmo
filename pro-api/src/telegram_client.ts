import { telegramMarkdownV2 } from "./telegram_markdown";
import type { TelegramReply } from "./telegram_messages";

type TelegramAPIResponse = {
  ok: boolean;
  result?: unknown;
  description?: string;
  parameters?: {
    retry_after?: number;
  };
};

const telegramMessageLimit = 4000;
const maxRetryAfterSeconds = 10;

export type TelegramBotCommand = {
  command: string;
  description: string;
};

export type TelegramCommandScope =
  | { type: "all_private_chats" }
  | { type: "chat"; chat_id: number };

type TelegramMethod =
  | "sendMessage"
  | "editMessageText"
  | "answerCallbackQuery"
  | "setMyCommands"
  | "deleteMyCommands";

function isTelegramAPIResponse(value: unknown): value is TelegramAPIResponse {
  if (typeof value !== "object" || value === null || !("ok" in value))
    return false;
  if (typeof value.ok !== "boolean") return false;
  if ("description" in value && typeof value.description !== "string")
    return false;
  if (!("parameters" in value)) return true;
  if (typeof value.parameters !== "object" || value.parameters === null)
    return false;
  if (!("retry_after" in value.parameters)) return true;
  return (
    typeof value.parameters.retry_after === "number" &&
    Number.isSafeInteger(value.parameters.retry_after) &&
    value.parameters.retry_after >= 0
  );
}

function telegramAPIError(
  method: string,
  response: Response,
  result: TelegramAPIResponse | null,
): Error {
  const detail = result?.description ? `: ${result.description}` : "";
  return new Error(`Telegram ${method} returned HTTP ${response.status}${detail}`);
}

async function callTelegram(
  botToken: string,
  method: TelegramMethod,
  body: object,
  requireTrueResult = false,
): Promise<void> {
  for (let attempt = 0; attempt < 2; attempt++) {
    const response = await fetch(
      `https://api.telegram.org/bot${botToken}/${method}`,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      },
    );
    const value = await response.json<unknown>().catch(() => null);
    const result = isTelegramAPIResponse(value) ? value : null;
    if (!response.ok) {
      const retryAfter = result?.parameters?.retry_after;
      if (
        response.status === 429 &&
        attempt === 0 &&
        retryAfter !== undefined
      ) {
        if (retryAfter > maxRetryAfterSeconds)
          throw new Error(
            `Telegram ${method} retry delay ${retryAfter}s exceeds limit`,
          );
        await scheduler.wait(retryAfter * 1000);
        continue;
      }
      throw telegramAPIError(method, response, result);
    }
    if (!result)
      throw new Error(`Telegram ${method} returned an invalid response`);
    if (!result.ok) {
      const detail = result.description ? `: ${result.description}` : "";
      throw new Error(`Telegram ${method} rejected the request${detail}`);
    }
    if (requireTrueResult && result.result !== true)
      throw new Error(`Telegram ${method} did not return true`);
    return;
  }
}

function validateReply(reply: TelegramReply): void {
  if (!reply.text || reply.text.length > telegramMessageLimit)
    throw new Error("Telegram message is empty or exceeds size limit");
}

export async function sendTelegram(
  botToken: string,
  chatID: number,
  reply: TelegramReply,
): Promise<void> {
  validateReply(reply);
  await callTelegram(botToken, "sendMessage", {
    chat_id: chatID,
    text: reply.text,
    parse_mode: telegramMarkdownV2,
    ...(reply.replyMarkup ? { reply_markup: reply.replyMarkup } : {}),
  });
}

export async function editTelegramMessage(
  botToken: string,
  chatID: number,
  messageID: number,
  reply: TelegramReply,
): Promise<void> {
  validateReply(reply);
  await callTelegram(botToken, "editMessageText", {
    chat_id: chatID,
    message_id: messageID,
    text: reply.text,
    parse_mode: telegramMarkdownV2,
    reply_markup: reply.replyMarkup ?? { inline_keyboard: [] },
  });
}

export async function answerTelegramCallback(
  botToken: string,
  callbackQueryID: string,
  text?: string,
): Promise<void> {
  await callTelegram(botToken, "answerCallbackQuery", {
    callback_query_id: callbackQueryID,
    ...(text ? { text } : {}),
  });
}

export async function setTelegramCommands(
  botToken: string,
  commands: TelegramBotCommand[],
  scope: TelegramCommandScope,
): Promise<void> {
  await callTelegram(
    botToken,
    "setMyCommands",
    { commands, scope },
    true,
  );
}

export async function deleteTelegramCommands(
  botToken: string,
  scope: TelegramCommandScope,
): Promise<void> {
  await callTelegram(botToken, "deleteMyCommands", { scope }, true);
}
