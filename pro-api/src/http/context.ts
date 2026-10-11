import type { Database } from '../storage/authorization';

export interface RequestContext {
  env: Env;
  db: Database;
  execution: Pick<ExecutionContext, 'waitUntil'>;
}
