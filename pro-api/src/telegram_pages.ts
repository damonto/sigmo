import type { RequestContext } from "./context";
import { nowISO } from "./http";
import { telegramAdmins } from "./telegram_access";
import {
  answerTelegramCallback,
  editTelegramMessage,
} from "./telegram_client";
import {
  renderDevicePage,
  renderEntitlementPage,
} from "./telegram_messages";
import type { TelegramReply } from "./telegram_messages";
import { parseTelegramPageCallback } from "./telegram_pagination";
import type { TelegramUpdate } from "./types";

const pageSize = 10;

export async function entitlementPage(
  context: RequestContext,
  requestedPage: number,
): Promise<TelegramReply> {
  const timestamp = nowISO();
  let page = requestedPage;
  let result = await context.db.listActiveEntitlements(
    timestamp,
    pageSize,
    (page - 1) * pageSize,
  );
  const pageCount = Math.ceil(result.total / pageSize);
  if (pageCount > 0 && page > pageCount) {
    page = pageCount;
    result = await context.db.listActiveEntitlements(
      timestamp,
      pageSize,
      (page - 1) * pageSize,
    );
  }
  return renderEntitlementPage(result, page, pageCount);
}

export async function devicePage(
  context: RequestContext,
  telegramID: number,
  requestedPage: number,
): Promise<TelegramReply> {
  let page = requestedPage;
  let result = await context.db.listDevices(
    telegramID,
    pageSize,
    (page - 1) * pageSize,
  );
  const pageCount = Math.ceil(result.total / pageSize);
  if (pageCount > 0 && page > pageCount) {
    page = pageCount;
    result = await context.db.listDevices(
      telegramID,
      pageSize,
      (page - 1) * pageSize,
    );
  }
  return renderDevicePage(result, telegramID, page, pageCount);
}

export async function handleTelegramPageCallback(
  context: RequestContext,
  callbackQuery: NonNullable<TelegramUpdate["callbackQuery"]>,
): Promise<void> {
  const { env } = context;
  const callback = callbackQuery.data
    ? parseTelegramPageCallback(callbackQuery.data)
    : null;
  const message = callbackQuery.message;
  const isPrivateMessage =
    message?.chat.type === "private" &&
    message.chat.id === callbackQuery.from.id;
  const isAdmin = telegramAdmins(env).has(callbackQuery.from.id);
  const isAuthorized =
    callback !== null &&
    isPrivateMessage &&
    (callback.resource === "entitlements"
      ? isAdmin
      : isAdmin || callback.telegramID === callbackQuery.from.id);
  if (!isAuthorized || !callback || !message) {
    await answerTelegramCallback(
      env.SIGMO_TELEGRAM_BOT_TOKEN,
      callbackQuery.id,
      "This button is not available.",
    );
    return;
  }

  await answerTelegramCallback(env.SIGMO_TELEGRAM_BOT_TOKEN, callbackQuery.id);
  const reply =
    callback.resource === "entitlements"
      ? await entitlementPage(context, callback.page)
      : await devicePage(context, callback.telegramID, callback.page);
  await editTelegramMessage(
    env.SIGMO_TELEGRAM_BOT_TOKEN,
    message.chat.id,
    message.messageId,
    reply,
  );
}
