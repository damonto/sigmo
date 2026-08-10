import { webcrypto } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  cloudflareTest,
  readD1Migrations,
} from "@cloudflare/vitest-pool-workers";
import { defineConfig } from "vitest/config";

export default defineConfig(async () => {
  const pair = await webcrypto.subtle.generateKey("Ed25519", true, [
    "sign",
    "verify",
  ]);
  if (!("privateKey" in pair && "publicKey" in pair)) {
    throw new Error("generate test Ed25519 key pair");
  }
  const privateKey = Buffer.from(
    await webcrypto.subtle.exportKey("pkcs8", pair.privateKey),
  ).toString("base64");
  const publicKey = Buffer.from(
    await webcrypto.subtle.exportKey("raw", pair.publicKey),
  ).toString("base64");
  const migrations = await readD1Migrations(
    path.join(path.dirname(fileURLToPath(import.meta.url)), "migrations"),
  );

  return {
    plugins: [
      cloudflareTest({
        wrangler: { configPath: "./wrangler.jsonc" },
        miniflare: {
          bindings: {
            TEST_MIGRATIONS: migrations,
            SIGMO_TELEGRAM_BOT_TOKEN: "test-bot-token",
            SIGMO_TELEGRAM_WEBHOOK_SECRET: "test-webhook-secret",
            SIGMO_LICENSE_PRIVATE_KEY: privateKey,
            SIGMO_LICENSE_PUBLIC_KEY: publicKey,
            SIGMO_DOWNLOAD_TICKET_SECRET:
              "test-download-ticket-secret-32-bytes",
            SIGMO_ADMIN_TELEGRAM_IDS: "1001",
          },
        },
      }),
    ],
    test: {
      setupFiles: ["./test/apply-migrations.ts"],
    },
  };
});
