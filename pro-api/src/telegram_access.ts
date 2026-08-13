export function telegramAdmins(env: Env): Set<number> {
  return new Set(
    env.SIGMO_ADMIN_TELEGRAM_IDS.split(",")
      .map((value) => Number(value.trim()))
      .filter((value) => Number.isSafeInteger(value) && value > 0),
  );
}
