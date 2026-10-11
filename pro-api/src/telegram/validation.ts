import type { TelegramChat, TelegramUser, TelegramUpdate } from './types';
import { isRecord, isNonEmptyString, isSafeInteger } from '../http/validation';

export function telegramUpdate(value: unknown): TelegramUpdate | null {
  if (!isRecord(value)) {
    return null;
  }
  const update: TelegramUpdate = {};
  if (value.message !== undefined) {
    const message = telegramMessage(value.message);
    if (!message) {
      return null;
    }
    update.message = message;
  }
  if (value.callback_query !== undefined) {
    const callbackQuery = telegramCallbackQuery(value.callback_query);
    if (!callbackQuery) {
      return null;
    }
    update.callbackQuery = callbackQuery;
  }
  return update;
}

function telegramChat(value: unknown): TelegramChat | null {
  if (!isRecord(value)) {
    return null;
  }
  const chatType = value.type;
  if (
    !isSafeInteger(value.id) ||
    (chatType !== 'private' &&
      chatType !== 'group' &&
      chatType !== 'supergroup' &&
      chatType !== 'channel')
  ) {
    return null;
  }
  return { id: value.id, type: chatType };
}

function telegramUser(value: unknown): TelegramUser | null {
  if (
    !isRecord(value) ||
    !isSafeInteger(value.id) ||
    value.id <= 0 ||
    typeof value.first_name !== 'string' ||
    (value.last_name !== undefined && typeof value.last_name !== 'string') ||
    (value.username !== undefined && typeof value.username !== 'string')
  ) {
    return null;
  }
  return {
    id: value.id,
    first_name: value.first_name,
    last_name: value.last_name,
    username: value.username,
  };
}

function telegramMessage(
  value: unknown,
): NonNullable<TelegramUpdate['message']> | null {
  if (!isRecord(value)) {
    return null;
  }
  const chat = telegramChat(value.chat);
  if (!chat) {
    return null;
  }
  if (value.from === undefined || value.text === undefined) {
    return { chat };
  }
  const from = telegramUser(value.from);
  if (!from || typeof value.text !== 'string') {
    return null;
  }
  return { chat, from, text: value.text };
}

function telegramCallbackQuery(
  value: unknown,
): NonNullable<TelegramUpdate['callbackQuery']> | null {
  if (
    !isRecord(value) ||
    !isNonEmptyString(value.id) ||
    (value.data !== undefined && typeof value.data !== 'string')
  ) {
    return null;
  }
  const from = telegramUser(value.from);
  if (!from) {
    return null;
  }
  if (value.message === undefined) {
    return { id: value.id, from, data: value.data };
  }
  if (
    !isRecord(value.message) ||
    !isSafeInteger(value.message.message_id) ||
    value.message.message_id <= 0
  ) {
    return null;
  }
  const chat = telegramChat(value.message.chat);
  if (!chat) {
    return null;
  }
  return {
    id: value.id,
    from,
    data: value.data,
    message: { messageId: value.message.message_id, chat },
  };
}
