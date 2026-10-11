import { app } from './http/app';
import { Database } from './storage/authorization';
import { logError } from './observability/log';
import { PushDatabase } from './storage/push';
import { reconcileTelegramCommands } from './telegram/commands';

export default {
  fetch: app.fetch,
  async scheduled(controller: ScheduledController, env: Env): Promise<void> {
    if (String(env.MAINTENANCE_MODE) === 'true') {
      return;
    }
    const db = new Database(env.DB);
    try {
      await db.deleteExpiredState(new Date().toISOString());
      await new PushDatabase(env.DB).clean();
      await reconcileTelegramCommands(env, db);
    } catch (caught) {
      logError('run scheduled maintenance', caught, { cron: controller.cron });
      throw caught;
    }
  },
} satisfies ExportedHandler<Env>;
