"""Check the single-terminal Mac launcher with disposable subprocess fixtures."""

import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from urllib.request import ProxyHandler, build_opener

from mcp_source import start_script

SCRIPT = Path(__file__).with_name("start-mac.sh")

FAKE_COMMAND = r'''
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

root = Path(os.environ["SIGMO_TEST_DIR"])
args = sys.argv[1:]
name = Path(sys.argv[0]).name

def event(name):
    with (root / "events").open("a") as output:
        output.write(json.dumps({"name": name, "args": args}) + "\n")

def serve(name):
    child = None
    def stop(signum, frame):
        (root / (name + ".stopped")).touch()
        if child:
            child.wait(timeout=3)
        sys.exit(0)
    signal.signal(signal.SIGTERM, stop)
    (root / (name + ".pid")).write_text(str(os.getpid()))
    event(name)
    if name == "gateway":
        child = subprocess.Popen([sys.executable, sys.argv[0], "--child"])
    while True:
        exit_file = root / (name + ".exit")
        if exit_file.exists():
            if child:
                child.terminate()
                child.wait(timeout=3)
            sys.exit(int(exit_file.read_text()))
        time.sleep(0.02)

if args == ["--child"]:
    serve("child")
elif name == "ssh":
    (root / "ssh-arguments.json").write_text(json.dumps(args))
    if "SIGMO_TEST_SSH_EXIT" in os.environ:
        sys.exit(int(os.environ["SIGMO_TEST_SSH_EXIT"]))
    serve("ssh")
elif name == "curl":
    url = next(arg for arg in args if arg.startswith("http://"))
    output = Path(args[args.index("-o") + 1])
    if url.endswith("/manifest.json"):
        if not (root / "source-ready").exists():
            sys.exit(22)
        event("source")
        output.write_text(json.dumps({"files": {"scripts/ios/mcp_gateway.py": "fixture"}}))
    else:
        event("download")
        if os.environ.get("SIGMO_TEST_BLOCK_DOWNLOAD") == "1":
            serve("download")
        fixture = root / "gateway-source.py"
        output.write_bytes(fixture.read_bytes() if fixture.exists() else b"# gateway fixture\n")
elif name == "uv":
    # uv must not consume any remaining bytes from curl's input pipe.
    if os.environ.get("SIGMO_TEST_READ_STDIN") == "1":
        sys.stdin.read()
    if not (root / "installed").exists():
        event("install")
        if os.environ.get("SIGMO_TEST_BLOCK_INSTALL") == "1":
            serve("install")
        code = int(os.environ.get("SIGMO_TEST_INSTALL_EXIT", "0"))
        if code:
            sys.exit(code)
    if (root / "gateway-source.py").exists():
        os.execv(sys.executable, [sys.executable, *args[args.index("--script") + 1:]])
    serve("gateway")
elif args[0] == "-":
    os.execv(sys.executable, [sys.executable, *args])
else:
    serve("child" if args[0] == "--child" else "gateway")
'''


class StartFixture:
    def __init__(self, test):
        self.test = test
        temporary = tempfile.TemporaryDirectory(prefix="sigmo start ")
        test.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.bin = self.directory / "bin"
        self.bin.mkdir()
        for name in ("ssh", "curl", "python", "uv"):
            command = self.bin / name
            command.write_text(f"#!{sys.executable}\n" + FAKE_COMMAND)
            command.chmod(0o755)
        (self.directory / "installed").touch()
        self.environment = {
            **os.environ,
            "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
            "SIGMO_MCP_DIR": str(self.directory / "service"),
            "SIGMO_TEST_DIR": str(self.directory),
        }
        with socket.socket() as source, socket.socket() as gateway:
            source.bind(("127.0.0.1", 0))
            gateway.bind(("127.0.0.1", 0))
            self.source_port = source.getsockname()[1]
            self.gateway_port = gateway.getsockname()[1]
        self.log = self.directory / "launcher.log"
        output = self.log.open("wb")
        test.addCleanup(output.close)
        self.output = output
        self.process = None
        test.addCleanup(self.close)

    def start(self, ready=True, piped=False):
        if ready:
            (self.directory / "source-ready").touch()
        command = ["bash", str(SCRIPT), "devbox", "--source-port", str(self.source_port),
                   "--mcp-port", str(self.gateway_port)]
        if piped:
            command = ["bash"]
        self.process = subprocess.Popen(
            command, stdin=subprocess.PIPE if piped else None,
            env=self.environment, stdout=self.output, stderr=subprocess.STDOUT, start_new_session=True,
        )
        return self.process

    def wait_file(self, name):
        path = self.directory / name
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            if path.exists():
                return path
            if self.process.poll() is not None:
                break
            time.sleep(0.02)
        self.test.fail(f"launcher did not create {name}: {self.log.read_text()}")

    def assert_stopped(self):
        for name in ("ssh", "gateway", "child", "install", "download"):
            path = self.directory / f"{name}.pid"
            if path.exists():
                with self.test.assertRaises(ProcessLookupError, msg=f"{name} remains alive:\n{self.log.read_text()}"):
                    os.kill(int(path.read_text()), 0)

    def wait_gateway(self):
        opener = build_opener(ProxyHandler({}))
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline and self.process.poll() is None:
            try:
                with opener.open(f"http://127.0.0.1:{self.gateway_port}/health", timeout=0.2):
                    return
            except OSError:
                time.sleep(0.02)
        self.test.fail(f"gateway did not start: {self.log.read_text()}")

    def real_gateway(self, bridge_source):
        (self.directory / "bridge.py").write_text(bridge_source)
        # Replace only Mac dependencies; run the actual gateway entry point and HTTP server.
        (self.directory / "gateway-source.py").write_text(f'''
import asyncio
import os
from pathlib import Path
import sys
from unittest.mock import AsyncMock
sys.path.insert(0, {str(SCRIPT.parent)!r})
import mcp_gateway
from fastmcp.client.transports import StdioTransport

directory = Path(os.environ["SIGMO_TEST_DIR"])
mcp_gateway.platform.system = lambda: "Darwin"
mcp_gateway.shutil.which = lambda command: command
mcp_gateway.SourceSync.run = AsyncMock()
mcp_gateway.subprocess.run = lambda *args, **kwargs: None
mcp_gateway.StdioTransport = lambda *args, **kwargs: StdioTransport(
    sys.executable, [str(directory / "bridge.py")], **kwargs
)
(directory / "gateway.pid").write_text(str(os.getpid()))
asyncio.run(mcp_gateway.main())
''')

    def close(self):
        if self.process and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=3)
        for name in ("ssh", "gateway", "child", "install", "download"):
            path = self.directory / f"{name}.pid"
            if path.exists():
                try:
                    os.kill(int(path.read_text()), signal.SIGKILL)
                except ProcessLookupError:
                    pass


class StartTests(unittest.TestCase):
    def test_http_bootstrap_runs_from_a_pipe_without_arguments(self):
        fixture = StartFixture(self)
        fixture.environment["SIGMO_TEST_READ_STDIN"] = "1"
        script = start_script(SCRIPT.parents[2], "developer@10.10.10.101", ("10.10.10.101", fixture.source_port))
        # Supply the fixture's free MCP port without adding command-line arguments.
        script = script.replace(b"sigmo_mcp_port=8765", f"sigmo_mcp_port={fixture.gateway_port}".encode())
        process = fixture.start(piped=True)
        try:
            process.stdin.write(script)
            process.stdin.flush()
            # Keep the input pipe open. A child reading it would hang gateway startup.
            fixture.wait_file("child.pid")
            arguments = json.loads((fixture.directory / "ssh-arguments.json").read_text())
            self.assertEqual(arguments[-1], "developer@10.10.10.101")
            self.assertEqual(arguments[arguments.index("-L") + 1],
                             f"127.0.0.1:{fixture.source_port}:10.10.10.101:{fixture.source_port}")
            process.send_signal(signal.SIGINT)
            self.assertEqual(process.wait(timeout=8), 130, fixture.log.read_text())
            fixture.assert_stopped()
        finally:
            process.stdin.close()

    def test_source_precedes_gateway_and_ctrl_c_cleans_children(self):
        fixture = StartFixture(self)
        process = fixture.start(ready=False)
        fixture.wait_file("ssh.pid")
        self.assertFalse((fixture.directory / "gateway.pid").exists())
        (fixture.directory / "source-ready").touch()
        fixture.wait_file("child.pid")
        process.send_signal(signal.SIGINT)
        self.assertEqual(process.wait(timeout=8), 130, fixture.log.read_text())
        fixture.assert_stopped()
        events = [json.loads(line)["name"] for line in (fixture.directory / "events").read_text().splitlines()]
        self.assertLess(events.index("source"), events.index("download"))
        self.assertLess(events.index("download"), events.index("gateway"))
        self.assertNotIn("install", events)

    def test_real_mcp_client_closes_its_separate_bridge_session(self):
        fixture = StartFixture(self)
        (fixture.directory / "bridge.py").write_text('''
import os
from pathlib import Path
from fastmcp import FastMCP
Path(__file__).with_name("child.pid").write_text(str(os.getpid()))
FastMCP("Xcode fixture").run(transport="stdio")
''')
        (fixture.directory / "gateway-source.py").write_text('''
import asyncio
import os
from pathlib import Path
import signal
import sys
from fastmcp import Client
from fastmcp.client.transports import StdioTransport

async def main():
    directory = Path(os.environ["SIGMO_TEST_DIR"])
    stopped = asyncio.Event()
    asyncio.get_running_loop().add_signal_handler(signal.SIGTERM, stopped.set)
    async with Client(StdioTransport(sys.executable, [str(directory / "bridge.py")]), init_timeout=5):
        (directory / "gateway.pid").write_text(str(os.getpid()))
        await stopped.wait()

asyncio.run(main())
''')
        process = fixture.start()
        gateway_pid = int(fixture.wait_file("gateway.pid").read_text())
        bridge_pid = int(fixture.wait_file("child.pid").read_text())
        self.assertEqual(os.getpgid(gateway_pid), gateway_pid)
        self.assertEqual(os.getpgid(bridge_pid), bridge_pid)
        process.send_signal(signal.SIGINT)
        self.assertEqual(process.wait(timeout=8), 130, fixture.log.read_text())
        fixture.assert_stopped()

    def test_shutdown_during_bridge_initialization_reaps_bridge(self):
        fixture = StartFixture(self)
        fixture.real_gateway('''
import os
from pathlib import Path
import signal
import time
signal.signal(signal.SIGTERM, signal.SIG_IGN)
Path(__file__).with_name("child.pid").write_text(str(os.getpid()))
time.sleep(1200)
''')
        process = fixture.start()
        bridge_pid = int(fixture.wait_file("child.pid").read_text())
        self.assertEqual(os.getpgid(bridge_pid), bridge_pid)
        process.send_signal(signal.SIGINT)
        self.assertEqual(process.wait(timeout=20), 130, fixture.log.read_text())
        fixture.assert_stopped()

    def test_shutdown_with_pending_tool_reaps_bridge(self):
        fixture = StartFixture(self)
        fixture.real_gateway('''
import asyncio
import os
from pathlib import Path
from fastmcp import FastMCP
directory = Path(__file__).parent
(directory / "child.pid").write_text(str(os.getpid()))
bridge = FastMCP("Xcode fixture")

@bridge.tool
async def hold() -> str:
    (directory / "call-started").touch()
    await asyncio.Event().wait()
    return "finished"

bridge.run(transport="stdio")
''')
        process = fixture.start()
        fixture.wait_gateway()
        client = subprocess.Popen(
            [sys.executable, str(SCRIPT.with_name("mcp_client.py")),
             "--url", f"http://127.0.0.1:{fixture.gateway_port}/mcp", "call", "hold"],
            stdout=fixture.output, stderr=subprocess.STDOUT,
        )
        try:
            fixture.wait_file("call-started")
            process.send_signal(signal.SIGINT)
            self.assertEqual(process.wait(timeout=20), 130, fixture.log.read_text())
            fixture.assert_stopped()
            state = json.loads((fixture.directory / "service/workspace/.sigmo-call.json").read_text())
            self.assertEqual(state["state"], "unknown")
        finally:
            if client.poll() is None:
                client.terminate()
            try:
                client.wait(timeout=3)
            except subprocess.TimeoutExpired:
                client.kill()
                client.wait(timeout=3)

    def test_either_service_exiting_stops_the_other(self):
        for name, code in (("ssh", 23), ("gateway", 24)):
            with self.subTest(service=name):
                fixture = StartFixture(self)
                process = fixture.start()
                fixture.wait_file("child.pid")
                (fixture.directory / f"{name}.exit").write_text(str(code))
                self.assertEqual(process.wait(timeout=8), code, fixture.log.read_text())
                fixture.assert_stopped()

    def test_dependency_failure_cleans_tunnel_before_gateway_starts(self):
        fixture = StartFixture(self)
        (fixture.directory / "installed").unlink()
        fixture.environment["SIGMO_TEST_INSTALL_EXIT"] = "25"
        process = fixture.start()
        self.assertEqual(process.wait(timeout=8), 25, fixture.log.read_text())
        self.assertFalse((fixture.directory / "gateway.pid").exists())
        fixture.assert_stopped()

    def test_tunnel_loss_interrupts_download_and_dependency_install(self):
        for step in ("download", "install"):
            with self.subTest(step=step):
                fixture = StartFixture(self)
                (fixture.directory / "installed").unlink()
                fixture.environment[f"SIGMO_TEST_BLOCK_{step.upper()}"] = "1"
                process = fixture.start()
                fixture.wait_file(f"{step}.pid")
                (fixture.directory / "ssh.exit").write_text("23")
                self.assertEqual(process.wait(timeout=13), 23, fixture.log.read_text())
                self.assertFalse((fixture.directory / "gateway.pid").exists())
                fixture.assert_stopped()

    def test_invalid_arguments_do_not_start_services(self):
        for args in ([], ["-oProxyCommand=command"], ["dev box"], ["dev\nbox"],
                     ["dev", "--source-port", "0"], ["dev", "--mcp-port", "65536"],
                     ["dev", "--ssh-port", "text"], ["dev", "--ssh-port"],
                     ["dev", "--source-port", "8765"]):
            with self.subTest(args=args):
                result = subprocess.run(["bash", str(SCRIPT), *args], capture_output=True, timeout=3)
                self.assertEqual(result.returncode, 2, result.stderr.decode())


if __name__ == "__main__":
    unittest.main()
