export function logError(
  message: string,
  caught: unknown,
  details: Record<string, string | number> = {},
): void {
  console.error(
    JSON.stringify({
      message,
      error: caught instanceof Error ? caught.message : String(caught),
      ...details,
    }),
  );
}
