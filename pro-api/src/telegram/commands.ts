import type { Database } from '../storage/authorization';
import { telegramAdmins } from './access';
import { deleteTelegramCommands, setTelegramCommands } from './client';
import type { TelegramBotCommand } from './client';
import { boldMarkdownV2, codeMarkdownV2, escapeMarkdownV2 } from './markdown';

type CommandAccess = 'user' | 'admin';

interface TelegramCommandDefinition {
  command: string;
  description: string;
  usage: string;
  adminUsage?: string;
  access: CommandAccess;
}

const TELEGRAM_COMMANDS = [
  {
    command: 'start',
    description: 'Show Pro status or authorize a device',
    usage: '/start [pairing_id]',
    access: 'user',
  },
  {
    command: 'download',
    description: 'Download Sigmo Pro',
    usage: '/download [stable|dev] [target]',
    access: 'user',
  },
  {
    command: 'grant',
    description: 'Grant a Pro entitlement',
    usage: '/grant <telegram_id> [max_devices] [expires_at]',
    access: 'admin',
  },
  {
    command: 'revoke',
    description: 'Revoke a Pro entitlement',
    usage: '/revoke <telegram_id>',
    access: 'admin',
  },
  {
    command: 'status',
    description: 'Show Pro entitlement status',
    usage: '/status <telegram_id>',
    access: 'admin',
  },
  {
    command: 'entitlements',
    description: 'List active Pro entitlements',
    usage: '/entitlements',
    access: 'admin',
  },
  {
    command: 'devices',
    description: 'List linked devices',
    usage: '/devices',
    adminUsage: '/devices [telegram_id]',
    access: 'user',
  },
  {
    command: 'revoke_device',
    description: 'Revoke a linked device by ID',
    usage: '/revoke_device <device_id>',
    access: 'user',
  },
] satisfies readonly TelegramCommandDefinition[];

function commandDefinitions(isAdmin: boolean): TelegramCommandDefinition[] {
  return TELEGRAM_COMMANDS.filter(({ access }) => access === 'user' || isAdmin);
}

function commandsFor(isAdmin: boolean): TelegramBotCommand[] {
  return commandDefinitions(isAdmin).map(({ command, description }) => ({
    command,
    description,
  }));
}

export function availableCommands(isAdmin: boolean): string {
  const commands = commandDefinitions(isAdmin).map((command) => {
    const usage = isAdmin
      ? (command.adminUsage ?? command.usage)
      : command.usage;
    return [
      escapeMarkdownV2(`/${command.command}`),
      escapeMarkdownV2(command.description),
      `${boldMarkdownV2('Usage')}: ${codeMarkdownV2(usage)}`,
    ].join('\n');
  });
  return [boldMarkdownV2('Available commands'), ...commands].join('\n\n');
}

export async function reconcileTelegramCommands(
  env: Env,
  db: Database,
): Promise<void> {
  const currentAdmins = [...telegramAdmins(env)].sort(
    (left, right) => left - right,
  );
  const currentAdminSet = new Set(currentAdmins);
  const previousAdmins = await db.listTelegramCommandAdmins();
  const operations = [
    setTelegramCommands(env.SIGMO_TELEGRAM_BOT_TOKEN, commandsFor(false), {
      type: 'all_private_chats',
    }),
  ];

  for (const chatId of currentAdmins) {
    operations.push(
      setTelegramCommands(env.SIGMO_TELEGRAM_BOT_TOKEN, commandsFor(true), {
        type: 'chat',
        chat_id: chatId,
      }),
    );
  }
  for (const chatId of previousAdmins) {
    if (currentAdminSet.has(chatId)) {
      continue;
    }
    operations.push(
      deleteTelegramCommands(env.SIGMO_TELEGRAM_BOT_TOKEN, {
        type: 'chat',
        chat_id: chatId,
      }),
    );
  }

  await Promise.all(operations);
  await db.replaceTelegramCommandAdmins(currentAdmins);
}
