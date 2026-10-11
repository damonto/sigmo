"""Check interrupted Xcode calls without launching Xcode or changing source."""

import asyncio
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import AsyncMock

from fastmcp import Client, FastMCP
from fastmcp.exceptions import ToolError
from fastmcp.server.middleware import MiddlewareContext
from fastmcp.tools import ToolResult
from mcp_types import CallToolRequestParams

from mcp_gateway import SourceSync, SyncMiddleware, make_gateway


class CallTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.workspace = Path(temporary.name) / "workspace"
        self.sync = SourceSync("http://127.0.0.1:8766", self.workspace)
        self.sync.state["revision"] = "saved-source"
        self.sync.run = AsyncMock()
        self.middleware = SyncMiddleware(self.sync)
        self.context = MiddlewareContext(message=CallToolRequestParams(name="BuildProject", arguments={}))

    async def test_results_are_recorded_without_replaying_calls(self):
        for error, state in ((None, "completed"), (ToolError("build rejected"), "failed"),
                             (ConnectionError("bridge disconnected"), "unknown")):
            with self.subTest(state=state):
                call = AsyncMock(side_effect=error, return_value=ToolResult(content="done"))
                if error:
                    with self.assertRaises(type(error)):
                        await self.middleware.on_call_tool(self.context, call)
                else:
                    await self.middleware.on_call_tool(self.context, call)
                call.assert_awaited_once_with(self.context)
                restored = SourceSync(self.sync.source_url, self.workspace)
                self.assertEqual(restored.last_call["state"], state)
                self.assertEqual(restored.last_call["revision"], "saved-source")
                self.assertFalse(self.sync.lock.locked())

    async def test_disconnect_keeps_source_locked_until_xcode_call_ends(self):
        entered = asyncio.Event()
        finish = asyncio.Event()
        calls = 0

        async def build(context):
            nonlocal calls
            calls += 1
            entered.set()
            await finish.wait()
            return ToolResult(content="built")

        task = asyncio.create_task(self.middleware.on_call_tool(self.context, build))
        try:
            await asyncio.wait_for(entered.wait(), 2)
            task.cancel()
            await asyncio.sleep(0)
            self.assertFalse(task.done())
            self.assertTrue(self.sync.lock.locked())
            self.assertEqual(json.loads(self.sync.call_path.read_text())["state"], "unknown")
            finish.set()
            with self.assertRaises(asyncio.CancelledError):
                await task
            self.assertFalse(self.sync.lock.locked())
            self.assertEqual(calls, 1)
            self.assertEqual(SourceSync(self.sync.source_url, self.workspace).last_call["state"], "unknown")
        finally:
            finish.set()
            if not task.done():
                task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def test_sync_failure_does_not_submit_a_call(self):
        self.sync.run.side_effect = ConnectionError("source offline")
        call = AsyncMock()
        with self.assertRaises(ToolError):
            await self.middleware.on_call_tool(self.context, call)
        call.assert_not_awaited()
        self.assertIsNone(self.sync.last_call)

    async def test_shutdown_closes_bridge_before_waiting_for_source_sync(self):
        syncing = asyncio.Event()
        finish_sync = asyncio.Event()
        stop = asyncio.Event()
        bridge_closed = asyncio.Event()
        bridge = Client(FastMCP("Xcode fixture"))
        close_bridge = bridge.close

        async def slow_sync():
            syncing.set()
            try:
                await finish_sync.wait()
            except asyncio.CancelledError:
                # Match SourceSync.run: finish the filesystem worker before returning.
                await finish_sync.wait()
                raise

        async def close():
            await close_bridge()
            bridge_closed.set()

        self.sync.run = slow_sync
        bridge.close = close
        gateway = make_gateway(self.sync, bridge, interval=0)

        async def run():
            async with Client(gateway):
                await stop.wait()

        task = asyncio.create_task(run())
        try:
            await asyncio.wait_for(syncing.wait(), 2)
            stop.set()
            await asyncio.wait_for(bridge_closed.wait(), 2)
            self.assertFalse(task.done())
        finally:
            stop.set()
            finish_sync.set()
            await asyncio.wait_for(task, 5)

    async def test_proxy_keeps_call_locked_after_client_cancels(self):
        backend = FastMCP("Xcode fixture")
        entered = asyncio.Event()
        finish = asyncio.Event()
        calls = 0

        @backend.tool
        async def build_fixture() -> str:
            nonlocal calls
            calls += 1
            entered.set()
            await finish.wait()
            return "fixture built"

        gateway = make_gateway(self.sync, Client(backend, timeout=5), interval=3600)
        async with Client(gateway, timeout=5) as client:
            self.assertIn("build_fixture", [tool.name for tool in await client.list_tools()])
            call = asyncio.create_task(client.call_tool("build_fixture"))
            try:
                await asyncio.wait_for(entered.wait(), 2)
                call.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await call
                self.assertTrue(self.sync.lock.locked())
                finish.set()

                async def wait_for_result():
                    while self.sync.lock.locked():
                        await asyncio.sleep(0.01)

                await asyncio.wait_for(wait_for_result(), 2)
                self.assertEqual(calls, 1)
                self.assertEqual(self.sync.last_call["state"], "unknown")
                result = await client.call_tool("build_fixture")
                self.assertEqual(result.content[0].text, "fixture built")
                self.assertEqual(calls, 2)
            finally:
                finish.set()
                if not call.done():
                    call.cancel()
                await asyncio.gather(call, return_exceptions=True)

    def test_gateway_restart_marks_an_inflight_call_unknown(self):
        self.sync.last_call = {"tool": "RunProject", "revision": "saved-source", "state": "running"}
        self.sync.save_call()
        restored = SourceSync(self.sync.source_url, self.workspace)
        self.assertEqual(restored.last_call["state"], "unknown")
        self.assertEqual(json.loads(restored.call_path.read_text())["state"], "unknown")


if __name__ == "__main__":
    unittest.main()
