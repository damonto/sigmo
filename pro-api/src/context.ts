import type { Database } from "./database";

export type RequestContext = {
  env: Env;
  db: Database;
  execution: ExecutionContext;
};
