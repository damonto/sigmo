# /// script
# requires-python = ">=3.10"
# dependencies = ["fastmcp==4.1.0"]
# ///
"""Pull source through SSH and expose Apple's Xcode MCP on the Mac's loopback address."""

import argparse
import asyncio
import hashlib
import io
import json
import logging
import os
import platform
import shutil
import signal
import subprocess
import tarfile
import tempfile
import time
from contextlib import asynccontextmanager, suppress
from pathlib import Path, PurePosixPath
from urllib.parse import urljoin, urlsplit
from urllib.request import ProxyHandler, build_opener

from fastmcp import Client
from fastmcp.client.transports import StdioTransport
from fastmcp.exceptions import ToolError
from fastmcp.server.middleware import Middleware
from fastmcp.server.providers.proxy import FastMCPProxy
from starlette.requests import Request
from starlette.responses import JSONResponse

LOG = logging.getLogger("sigmo.xcode")


def file_hash(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class SourceSync:
    def __init__(self, source_url, workspace):
        self.source_url = source_url.rstrip("/")
        # Source requests go through SSH's loopback listener, never a system proxy.
        self.opener = build_opener(ProxyHandler({}))
        self.workspace = Path(workspace).expanduser().resolve()
        self.workspace.mkdir(parents=True, exist_ok=True)
        self.state_path = self.workspace / ".sigmo-sync.json"
        self.state = (
            json.loads(self.state_path.read_text())
            if self.state_path.exists()
            else {"files": {}}
        )
        self.lock = asyncio.Lock()
        self.last_error = ""
        self.call_path = self.workspace / ".sigmo-call.json"
        self.last_call = json.loads(self.call_path.read_text()) if self.call_path.exists() else None
        if self.last_call and self.last_call["state"] == "running":
            # A gateway restart cannot establish whether Xcode finished an in-flight call.
            self.last_call["state"] = "unknown"
            self.save_call()

    def status(self):
        return {
            "workspace": str(self.workspace),
            "revision": self.state.get("revision"),
            "commit": self.state.get("commit"),
            "last_error": self.last_error,
            "last_call": self.last_call,
        }

    def fetch(self, suffix):
        target = urljoin(self.source_url + "/", suffix)
        source = urlsplit(self.source_url)
        destination = urlsplit(target)
        if (destination.scheme, destination.netloc) != (source.scheme, source.netloc):
            raise ValueError("snapshot URL must belong to the configured source server")
        with self.opener.open(target, timeout=15) as response:
            return response.read()

    def source_path(self, name, *, allow_legacy=False):
        path = PurePosixPath(name)
        if (
            path.is_absolute()
            or path.as_posix() != name
            or ".." in path.parts
            or not (
                len(path.parts) >= 3 and path.parts[:2] == ("apps", "ios")
                or len(path.parts) >= 3 and path.parts[:2] == ("scripts", "ios")
                # Existing Mac state still names ios/ until its first sync after the move.
                or allow_legacy and len(path.parts) >= 2 and path.parts[0] == "ios"
            )
        ):
            raise ValueError(f"invalid source manifest path: {name}")
        target = self.workspace
        for part in path.parts:
            target /= part
            if target.is_symlink():
                raise ValueError(f"Mac source contains a symlink; resolve it before syncing: {name}")
        return target

    def check_local_edits(self, incoming):
        for name, expected in self.state["files"].items():
            target = self.source_path(name, allow_legacy=True)
            if not target.is_file() or file_hash(target) != expected:
                raise ValueError(
                    f"Mac source changed locally; preserve and resolve it before syncing: {name}"
                )
        for name, expected in incoming.items():
            target = self.source_path(name)
            if (
                name not in self.state["files"]
                and target.exists()
                and (not target.is_file() or file_hash(target) != expected)
            ):
                raise ValueError(f"Mac file conflicts with incoming source: {name}")

    def sync(self):
        try:
            return self._sync()
        except Exception as error:
            self.last_error = str(error)
            raise

    async def run(self):
        # Cancellation must not release the caller's lock while the filesystem
        # worker is still updating the Xcode copy.
        task = asyncio.create_task(asyncio.to_thread(self.sync))
        try:
            return await asyncio.shield(task)
        except asyncio.CancelledError:
            await task
            raise

    def save_state(self):
        temporary = self.state_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self.state, indent=2) + "\n")
        temporary.replace(self.state_path)

    def save_call(self):
        temporary = self.call_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self.last_call, indent=2) + "\n")
        temporary.replace(self.call_path)

    def generate_project(self):
        if self.state.get("needs_project_generation"):
            subprocess.run(
                ["xcodegen", "generate", "--spec", "project.yml"],
                cwd=self.workspace / "apps/ios",
                check=True,
            )
            self.state["needs_project_generation"] = False
            self.save_state()

    def _sync(self):
        manifest = json.loads(self.fetch("manifest.json"))
        incoming = manifest["files"]
        self.check_local_edits(incoming)
        if manifest["revision"] == self.state.get("revision"):
            self.generate_project()
            self.last_error = ""
            return False
        content = self.fetch(manifest["archive"])
        if hashlib.sha256(content).hexdigest() != manifest["revision"]:
            raise ValueError("snapshot checksum mismatch")
        regenerate = (
            not (self.workspace / "apps/ios/Sigmo.xcodeproj").exists()
            or incoming.get("apps/ios/project.yml")
            != self.state["files"].get("apps/ios/project.yml")
            or set(incoming) != set(self.state["files"])
        )
        with tempfile.TemporaryDirectory(
            prefix="sigmo-stage-", dir=self.workspace.parent
        ) as temporary:
            staging = Path(temporary)
            found = set()
            with tarfile.open(fileobj=io.BytesIO(content), mode="r:gz") as archive:
                for item in archive:
                    if (
                        not item.isfile()
                        or item.name not in incoming
                        or item.name in found
                    ):
                        raise ValueError("invalid snapshot entry")
                    found.add(item.name)
                    data = archive.extractfile(item).read()
                    if hashlib.sha256(data).hexdigest() != incoming[item.name]:
                        raise ValueError("snapshot file checksum mismatch")
                    target = staging / item.name
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(data)
            if found != set(incoming):
                raise ValueError("snapshot file list mismatch")
            # Check again after the network round trip before touching the active copy.
            self.check_local_edits(incoming)
            for name in incoming:
                if incoming[name] == self.state["files"].get(name):
                    continue
                target = self.source_path(name)
                target.parent.mkdir(parents=True, exist_ok=True)
                os.replace(staging / name, target)
            for name in self.state["files"].keys() - incoming.keys():
                self.source_path(name, allow_legacy=True).unlink()
        self.state = manifest
        self.state["needs_project_generation"] = regenerate
        # Record the installed files even if XcodeGen fails, allowing a retry
        # instead of misidentifying those files as edits made on the Mac.
        self.save_state()
        self.generate_project()
        self.last_error = ""
        LOG.info("Synced source %s", manifest["revision"][:12])
        return True


class SyncMiddleware(Middleware):
    def __init__(self, sync):
        self.sync = sync

    async def on_call_tool(self, context, call_next):
        if context.message.name in ("GatewayStatus", "SyncProject"):
            return await call_next(context)
        async with self.sync.lock:
            try:
                await self.sync.run()
            except Exception as error:
                raise ToolError(f"Source sync: {error}") from error
            started = time.monotonic()
            self.sync.last_call = {
                "tool": context.message.name,
                "revision": self.sync.state.get("revision"),
                "state": "running",
            }
            self.sync.save_call()
            task = asyncio.create_task(call_next(context))
            try:
                result = await asyncio.shield(task)
                self.sync.last_call["state"] = "completed"
                return result
            except asyncio.CancelledError:
                self.sync.last_call["state"] = "unknown"
                self.sync.save_call()
                # Keep source updates locked until the already-submitted Xcode call ends.
                try:
                    await task
                except Exception:
                    LOG.exception("Xcode call ended after its client disconnected")
                raise
            except ToolError:
                self.sync.last_call["state"] = "failed"
                raise
            except BaseException:
                # Transport loss/cancellation does not prove the Xcode operation stopped.
                self.sync.last_call["state"] = "unknown"
                raise
            finally:
                self.sync.last_call["seconds"] = round(time.monotonic() - started, 2)
                self.sync.save_call()
                LOG.info("MCP call %s", json.dumps(self.sync.last_call))


async def periodic_sync(sync, interval):
    while True:
        await asyncio.sleep(interval)
        async with sync.lock:
            try:
                await sync.run()
            except Exception:
                LOG.exception("Source sync")


def make_gateway(sync, bridge, interval):
    @asynccontextmanager
    async def lifespan(server):
        # One trusted remote developer shares a persistent Xcode bridge. The lock
        # serializes calls and source updates; no per-call agent process is needed.
        task = None
        try:
            async with bridge:
                task = asyncio.create_task(periodic_sync(sync, interval))
                try:
                    yield {}
                finally:
                    task.cancel()
        finally:
            # Also release the transport when startup fails before Client's context opens.
            try:
                await bridge.close()
            finally:
                # A cancelled sync can still be finishing filesystem work. It must
                # not delay releasing the separate bridge process during shutdown.
                if task is not None:
                    with suppress(asyncio.CancelledError):
                        await task

    proxy = FastMCPProxy(
        client_factory=lambda: bridge, name="Sigmo Xcode", lifespan=lifespan
    )
    proxy.add_middleware(SyncMiddleware(sync))

    @proxy.tool(name="GatewayStatus")
    def gateway_status() -> dict:
        """Read the synchronized revision and latest Xcode call status."""
        return sync.status()

    @proxy.tool(name="SyncProject")
    async def sync_project() -> dict:
        """Pull saved remote source before the next Xcode operation."""
        async with sync.lock:
            await sync.run()
            return sync.status()

    @proxy.custom_route("/health", methods=["GET"])
    async def health(request: Request):
        return JSONResponse(sync.status())

    return proxy


async def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-url", required=True)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", default=8765, type=int)
    parser.add_argument("--workspace", default="~/Library/Developer/SigmoMCP/workspace")
    parser.add_argument("--interval", default=3, type=float)
    args = parser.parse_args()
    if platform.system() != "Darwin":
        parser.error("run this gateway on the Mac with Xcode")
    if args.interval < 1:
        parser.error("sync interval must be at least one second")
    os.environ["PATH"] = (
        os.environ.get("PATH", "") + ":/opt/homebrew/bin:/usr/local/bin"
    )
    if not shutil.which("xcodegen"):
        parser.error("install XcodeGen on the Mac first: brew install xcodegen")
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(message)s")
    task = asyncio.current_task()
    stopping = False

    def stop(signum, frame):
        nonlocal stopping
        if not stopping:
            stopping = True
            task.cancel()

    # Bridge initialization precedes Uvicorn's signal handlers. Cancel the task
    # here so that even an early SIGTERM unwinds the stdio subprocess context.
    previous = signal.signal(signal.SIGTERM, stop)
    try:
        sync = SourceSync(args.source_url, args.workspace)
        await sync.run()
        await asyncio.to_thread(
            subprocess.run,
            ["open", "-a", "Xcode", str(sync.workspace / "apps/ios/Sigmo.xcodeproj")], check=True
        )
        bridge = Client(
            StdioTransport(
                "/usr/bin/xcrun", ["mcpbridge"], cwd=str(sync.workspace), keep_alive=False
            ),
            name="Sigmo remote development",
            init_timeout=180,
            timeout=1200,
        )
        proxy = make_gateway(sync, bridge, args.interval)
        LOG.info("Xcode may request permission for this bridge. Allow it in the Mac UI.")
        await proxy.run_async(transport="http", host=args.host, port=args.port, path="/mcp")
    except asyncio.CancelledError:
        LOG.info("Gateway stopped")
    finally:
        signal.signal(signal.SIGTERM, previous)


if __name__ == "__main__":
    asyncio.run(main())
