export const telegramMarkdownV2 = "MarkdownV2";

export function escapeMarkdownV2(value: string): string {
  return value.replace(/[_*[\]()~`>#+\-=|{}.!\\]/g, "\\$&");
}

export function boldMarkdownV2(value: string): string {
  return `*${escapeMarkdownV2(value)}*`;
}

export function codeMarkdownV2(value: string): string {
  return `\`${value.replace(/[`\\]/g, "\\$&")}\``;
}

export function preformattedMarkdownV2(value: string): string {
  return `\`\`\`\n${value.replace(/[`\\]/g, "\\$&")}\n\`\`\``;
}
