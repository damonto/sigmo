import type { RequestContext } from '../http/context';

import { telegramAdmins } from './access';
import { answerTelegramCallback, editTelegramMessage } from './client';
import { renderDevicePage, renderEntitlementPage } from './messages';
import type { TelegramReply } from './messages';
import { parseTelegramPageCallback } from './pagination';
import type { TelegramUpdate } from './types';

const PAGE_SIZE = 10;

export async function entitlementPage(
  context: RequestContext,
  requestedPage: number,
): Promise<TelegramReply> {
  const timestamp = new Date().toISOString();
  let page = requestedPage;
  let result = await context.db.listActiveEntitlements(
    timestamp,
    PAGE_SIZE,
    (page - 1) * PAGE_SIZE,
  );
  const pageCount = Math.ceil(result.total / PAGE_SIZE);
  if (pageCount > 0 && page > pageCount) {
    page = pageCount;
    result = await context.db.listActiveEntitlements(
      timestamp,
      PAGE_SIZE,
      (page - 1) * PAGE_SIZE,
    );
  }
  return renderEntitlementPage(result, page, pageCount);
}

export async function devicePage(
  context: RequestContext,
  telegramId: number,
  requestedPage: number,
): Promise<TelegramReply> {
  let page = requestedPage;
  let result = await context.db.listDevices(
    telegramId,
    PAGE_SIZE,
    (page - 1) * PAGE_SIZE,
  );
  const pageCount = Math.ceil(result.total / PAGE_SIZE);
  if (pageCount > 0 && page > pageCount) {
    page = pageCount;
    result = await context.db.listDevices(
      telegramId,
      PAGE_SIZE,
      (page - 1) * PAGE_SIZE,
    );
  }
  return renderDevicePage(result, telegramId, page, pageCount);
}

export async function handleTelegramPageCallback(
  context: RequestContext,
  callbackQuery: NonNullable<TelegramUpdate['callbackQuery']>,
): Promise<void> {
  const { env } = context;
  const callback = callbackQuery.data
    ? parseTelegramPageCallback(callbackQuery.data)
    : null;
  const message = callbackQuery.message;
  const isPrivateMessage =
    message?.chat.type === 'private' &&
    message.chat.id === callbackQuery.from.id;
  const isAdmin = telegramAdmins(env).has(callbackQuery.from.id);
  const isAuthorized =
    callback !== null &&
    isPrivateMessage &&
    (callback.resource === 'entitlements'
      ? isAdmin
      : isAdmin || callback.telegramId === callbackQuery.from.id);
  if (!isAuthorized || !callback || !message) {
    await answerTelegramCallback(
      env.SIGMO_TELEGRAM_BOT_TOKEN,
      callbackQuery.id,
      'This button is not available.',
    );
    return;
  }

  await answerTelegramCallback(env.SIGMO_TELEGRAM_BOT_TOKEN, callbackQuery.id);
  const reply =
    callback.resource === 'entitlements'
      ? await entitlementPage(context, callback.page)
      : await devicePage(context, callback.telegramId, callback.page);
  await editTelegramMessage(
    env.SIGMO_TELEGRAM_BOT_TOKEN,
    message.chat.id,
    message.messageId,
    reply,
  );
}
