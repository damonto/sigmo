# /// script
# requires-python = ">=3.10"
# dependencies = ["fastmcp==4.1.0"]
# ///
"""Inspect and call Xcode MCP through the development host's local SSH forward."""

import argparse
import asyncio
import json
from pathlib import Path
import sys

from fastmcp import Client


async def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://127.0.0.1:8765/mcp")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--timeout", default=180, type=int)
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("list")
    call = commands.add_parser("call")
    call.add_argument("tool")
    call.add_argument("--arguments", default="{}", help="JSON object matching the tool schema")
    args = parser.parse_args()
    async with Client(args.url, timeout=args.timeout, init_timeout=30) as client:
        if args.command == "list":
            payload = [tool.model_dump(mode="json", exclude_none=True) for tool in await client.list_tools()]
        else:
            arguments = json.loads(args.arguments)
            try:
                result = await client.call_tool(args.tool, arguments, raise_on_error=False)
            except Exception as error:
                print(f"MCP call interrupted: {error}", file=sys.stderr)
                print("The result is unknown. Inspect GatewayStatus and Xcode before retrying; the call was not replayed.", file=sys.stderr)
                raise SystemExit(1) from error
            payload = {
                "isError": result.is_error,
                "content": [item.model_dump(mode="json", exclude_none=True) for item in result.content],
                "structuredContent": result.structured_content,
            }
    text = json.dumps(payload, indent=2, ensure_ascii=False)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(text + "\n")
        print(f"Saved MCP result to {args.output}")
    else:
        print(text)
    if isinstance(payload, dict) and payload.get("isError"):
        raise SystemExit(1)


if __name__ == "__main__":
    asyncio.run(main())
