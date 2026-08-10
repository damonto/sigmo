import type { Database } from "./database";

type CommandAccess = "user" | "admin";

type TelegramCommandDefinition = {
  command: string;
  description: string;
  usage: string;
  adminUsage?: string;
  access: CommandAccess;
};

type TelegramBotCommand = {
  command: string;
  description: string;
};

type TelegramCommandScope =
  | { type: "all_private_chats" }
  | { type: "chat"; chat_id: number };

type TelegramCommandRequest = {
  commands?: TelegramBotCommand[];
  scope: TelegramCommandScope;
};

type TelegramAPIResponse = {
  ok: boolean;
  result?: boolean;
  description?: string;
};

const telegramCommands = [
  {
    command: "start",
    description: "Authorize a Sigmo Pro device",
    usage: "/start [pairing_id]",
    access: "user",
  },
  {
    command: "grant",
    description: "Grant a Pro entitlement",
    usage: "/grant <telegram_id> [max_devices] [expires_at]",
    access: "admin",
  },
  {
    command: "revoke",
    description: "Revoke a Pro entitlement",
    usage: "/revoke <telegram_id>",
    access: "admin",
  },
  {
    command: "status",
    description: "Show Pro entitlement status",
    usage: "/status <telegram_id>",
    access: "admin",
  },
  {
    command: "entitlements",
    description: "List active Pro entitlements",
    usage: "/entitlements",
    access: "admin",
  },
  {
    command: "devices",
    description: "List linked devices",
    usage: "/devices",
    adminUsage: "/devices [telegram_id]",
    access: "user",
  },
  {
    command: "revoke_device",
    description: "Revoke a linked device by ID",
    usage: "/revoke_device <device_id>",
    access: "user",
  },
] satisfies readonly TelegramCommandDefinition[];

export function admins(env: Env): Set<number> {
  return new Set(
    env.SIGMO_ADMIN_TELEGRAM_IDS.split(",")
      .map((value) => Number(value.trim()))
      .filter((value) => Number.isSafeInteger(value) && value > 0),
  );
}

function commandDefinitions(isAdmin: boolean): TelegramCommandDefinition[] {
  return telegramCommands.filter(
    ({ access }) => access === "user" || isAdmin,
  );
}

function commandsFor(isAdmin: boolean): TelegramBotCommand[] {
  return commandDefinitions(isAdmin).map(({ command, description }) => ({
    command,
    description,
  }));
}

export function availableCommands(isAdmin: boolean): string {
  const lines = commandDefinitions(isAdmin).map((command) => {
    const usage = isAdmin
      ? (command.adminUsage ?? command.usage)
      : command.usage;
    return `${usage} — ${command.description}`;
  });
  return ["Available commands:", ...lines].join("\n");
}

function isTelegramAPIResponse(value: unknown): value is TelegramAPIResponse {
  if (typeof value !== "object" || value === null || !("ok" in value))
    return false;
  if (typeof value.ok !== "boolean") return false;
  if ("result" in value && typeof value.result !== "boolean") return false;
  return !("description" in value) || typeof value.description === "string";
}

async function callTelegramBooleanMethod(
  env: Env,
  method: "setMyCommands" | "deleteMyCommands",
  body: TelegramCommandRequest,
): Promise<void> {
  const response = await fetch(
    `https://api.telegram.org/bot${env.SIGMO_TELEGRAM_BOT_TOKEN}/${method}`,
    {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    },
  );
  const result = await response.json<unknown>().catch(() => null);
  if (!response.ok)
    throw new Error(`Telegram ${method} returned HTTP ${response.status}`);
  if (!isTelegramAPIResponse(result))
    throw new Error(`Telegram ${method} returned an invalid response`);
  if (!result.ok) {
    const detail = result.description ? `: ${result.description}` : "";
    throw new Error(`Telegram ${method} rejected the request${detail}`);
  }
  if (result.result !== true)
    throw new Error(`Telegram ${method} did not return true`);
}

function setTelegramCommands(
  env: Env,
  commands: TelegramBotCommand[],
  scope: TelegramCommandScope,
): Promise<void> {
  return callTelegramBooleanMethod(env, "setMyCommands", { commands, scope });
}

function deleteTelegramCommands(
  env: Env,
  scope: TelegramCommandScope,
): Promise<void> {
  return callTelegramBooleanMethod(env, "deleteMyCommands", { scope });
}

export async function reconcileTelegramCommands(
  env: Env,
  db: Database,
): Promise<void> {
  const currentAdmins = [...admins(env)].sort((left, right) => left - right);
  const currentAdminSet = new Set(currentAdmins);
  const previousAdmins = await db.listTelegramCommandAdmins();
  const operations = [
    setTelegramCommands(env, commandsFor(false), {
      type: "all_private_chats",
    }),
  ];

  for (const chatID of currentAdmins) {
    operations.push(
      setTelegramCommands(env, commandsFor(true), {
        type: "chat",
        chat_id: chatID,
      }),
    );
  }
  for (const chatID of previousAdmins) {
    if (currentAdminSet.has(chatID)) continue;
    operations.push(
      deleteTelegramCommands(env, { type: "chat", chat_id: chatID }),
    );
  }

  await Promise.all(operations);
  await db.replaceTelegramCommandAdmins(currentAdmins);
}
