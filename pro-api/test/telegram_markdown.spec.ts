import { describe, expect, it } from "vitest";

import {
  boldMarkdownV2,
  codeMarkdownV2,
  escapeMarkdownV2,
  preformattedMarkdownV2,
} from "../src/telegram_markdown";

describe("Telegram MarkdownV2", () => {
  it.each([
    {
      name: "plain text",
      render: escapeMarkdownV2,
      input: "Hello world",
      want: "Hello world",
    },
    {
      name: "reserved characters",
      render: escapeMarkdownV2,
      input: "_*[]()~`>#+-=|{}.!\\",
      want: "\\_\\*\\[\\]\\(\\)\\~\\`\\>\\#\\+\\-\\=\\|\\{\\}\\.\\!\\\\",
    },
    {
      name: "bold text",
      render: boldMarkdownV2,
      input: "A *bold* title",
      want: "*A \\*bold\\* title*",
    },
    {
      name: "inline code",
      render: codeMarkdownV2,
      input: "path\\`value",
      want: "`path\\\\\\`value`",
    },
    {
      name: "preformatted text",
      render: preformattedMarkdownV2,
      input: "one\\two\n`three`",
      want: "```\none\\\\two\n\\`three\\`\n```",
    },
  ])("renders $name", ({ render, input, want }) => {
    expect(render(input)).toBe(want);
  });
});
