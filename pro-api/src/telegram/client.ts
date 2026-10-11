import { TELEGRAM_MARKDOWN_V2 } from './markdown';
import type { TelegramReply } from './messages';
import { readJson } from '../http/body';

interface TelegramApiResponse {
  ok: boolean;
  result?: unknown;
  description?: string;
  parameters?: {
    retry_after?: number;
  };
}

const TELEGRAM_MESSAGE_LIMIT = 4000;
const MAX_RETRY_AFTER_SECONDS = 10;
const REQUEST_TIMEOUT_MS = 10_000;

export interface TelegramBotCommand {
  command: string;
  description: string;
}

export type TelegramCommandScope =
  { type: 'all_private_chats' } | { type: 'chat'; chat_id: number };

type TelegramMethod =
  | 'sendMessage'
  | 'editMessageText'
  | 'answerCallbackQuery'
  | 'setMyCommands'
  | 'deleteMyCommands';

function isTelegramApiResponse(value: unknown): value is TelegramApiResponse {
  if (typeof value !== 'object' || value === null || !('ok' in value)) {
    return false;
  }
  if (typeof value.ok !== 'boolean') {
    return false;
  }
  if ('description' in value && typeof value.description !== 'string') {
    return false;
  }
  if (!('parameters' in value)) {
    return true;
  }
  if (typeof value.parameters !== 'object' || value.parameters === null) {
    return false;
  }
  if (!('retry_after' in value.parameters)) {
    return true;
  }
  return (
    typeof value.parameters.retry_after === 'number' &&
    Number.isSafeInteger(value.parameters.retry_after) &&
    value.parameters.retry_after >= 0
  );
}

function telegramApiError(
  method: string,
  response: Response,
  result: TelegramApiResponse | null,
): Error {
  const detail = result?.description ? `: ${result.description}` : '';
  return new Error(
    `Telegram ${method} returned HTTP ${response.status}${detail}`,
  );
}

async function callTelegram(
  botToken: string,
  method: TelegramMethod,
  body: object,
  requireTrueResult = false,
): Promise<void> {
  const signal = AbortSignal.timeout(REQUEST_TIMEOUT_MS);
  for (let attempt = 0; attempt < 2; attempt++) {
    let response: Response;
    try {
      response = await fetch(
        `https://api.telegram.org/bot${botToken}/${method}`,
        {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify(body),
          redirect: 'manual',
          signal,
        },
      );
    } catch (cause) {
      throw new Error(
        signal.aborted
          ? `Telegram ${method} request timed out`
          : `Telegram ${method} request could not be completed`,
        { cause },
      );
    }
    let value: unknown;
    try {
      value = await readJson(response.body);
    } catch (cause) {
      if (signal.aborted) {
        throw new Error(`Telegram ${method} request timed out`, { cause });
      }
      value = null;
    }
    const result = isTelegramApiResponse(value) ? value : null;
    if (!response.ok) {
      const retryAfter = result?.parameters?.retry_after;
      if (
        response.status === 429 &&
        attempt === 0 &&
        retryAfter !== undefined
      ) {
        if (retryAfter > MAX_RETRY_AFTER_SECONDS) {
          throw new Error(
            `Telegram ${method} retry delay ${retryAfter}s exceeds limit`,
          );
        }
        await scheduler.wait(retryAfter * 1000, { signal });
        continue;
      }
      throw telegramApiError(method, response, result);
    }
    if (!result) {
      throw new Error(`Telegram ${method} returned an invalid response`);
    }
    if (!result.ok) {
      const detail = result.description ? `: ${result.description}` : '';
      throw new Error(`Telegram ${method} rejected the request${detail}`);
    }
    if (requireTrueResult && result.result !== true) {
      throw new Error(`Telegram ${method} did not return true`);
    }
    return;
  }
}

function validateReply(reply: TelegramReply): void {
  if (!reply.text || reply.text.length > TELEGRAM_MESSAGE_LIMIT) {
    throw new Error('Telegram message is empty or exceeds size limit');
  }
}

export async function sendTelegram(
  botToken: string,
  chatId: number,
  reply: TelegramReply,
): Promise<void> {
  validateReply(reply);
  await callTelegram(botToken, 'sendMessage', {
    chat_id: chatId,
    text: reply.text,
    parse_mode: TELEGRAM_MARKDOWN_V2,
    ...(reply.replyMarkup ? { reply_markup: reply.replyMarkup } : {}),
  });
}

export async function editTelegramMessage(
  botToken: string,
  chatId: number,
  messageId: number,
  reply: TelegramReply,
): Promise<void> {
  validateReply(reply);
  await callTelegram(botToken, 'editMessageText', {
    chat_id: chatId,
    message_id: messageId,
    text: reply.text,
    parse_mode: TELEGRAM_MARKDOWN_V2,
    reply_markup: reply.replyMarkup ?? { inline_keyboard: [] },
  });
}

export async function answerTelegramCallback(
  botToken: string,
  callbackQueryId: string,
  text?: string,
): Promise<void> {
  await callTelegram(botToken, 'answerCallbackQuery', {
    callback_query_id: callbackQueryId,
    ...(text ? { text } : {}),
  });
}

export async function setTelegramCommands(
  botToken: string,
  commands: TelegramBotCommand[],
  scope: TelegramCommandScope,
): Promise<void> {
  await callTelegram(botToken, 'setMyCommands', { commands, scope }, true);
}

export async function deleteTelegramCommands(
  botToken: string,
  scope: TelegramCommandScope,
): Promise<void> {
  await callTelegram(botToken, 'deleteMyCommands', { scope }, true);
}
