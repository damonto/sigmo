"""Exercise both SSH forwards and manual reconnection using loopback fixtures only."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import pwd
import os
import shutil
import socket
import subprocess
import tempfile
import threading
import time
import unittest
from urllib.request import ProxyHandler, build_opener

from test_mcp_start import StartFixture


@unittest.skipUnless(shutil.which("sshd") and shutil.which("ssh-keygen"), "OpenSSH server tools are required")
class TunnelTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="sigmo-ssh-test-")
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.ports = set()
        self.requests = []
        self.finish_stream = threading.Event()
        self.addCleanup(self.finish_stream.set)
        self.opener = build_opener(ProxyHandler({}))
        for name in ("host", "client"):
            subprocess.run(
                ["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(self.directory / name)],
                check=True,
            )
        self.ssh_port = self.free_port()
        username = pwd.getpwuid(os.getuid()).pw_name
        config = self.directory / "sshd_config"
        config.write_text(
            f"ListenAddress 127.0.0.1\nPort {self.ssh_port}\n"
            f"HostKey {self.directory / 'host'}\n"
            f"AuthorizedKeysFile {self.directory / 'client.pub'}\n"
            f"PidFile {self.directory / 'sshd.pid'}\nAllowUsers {username}\n"
            "UsePAM no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n"
            "StrictModes no\nPermitRootLogin prohibit-password\n"
            "AllowTcpForwarding yes\nGatewayPorts no\n"
            "ClientAliveInterval 1\nClientAliveCountMax 2\nLogLevel ERROR\n"
        )
        log = self.directory / "sshd.log"
        self.sshd = self.spawn([shutil.which("sshd"), "-D", "-e", "-f", str(config)], log)
        self.wait_port(self.ssh_port, self.sshd, log)
        known_hosts = self.directory / "known_hosts"
        known_hosts.write_text(f"[127.0.0.1]:{self.ssh_port} " + (self.directory / "host.pub").read_text())
        self.source_port = self.serve("source")
        self.gateway_port = self.serve("gateway")
        self.forward_source = self.free_port()
        self.forward_mcp = self.free_port()
        # Capture the launcher's real SSH arguments, then use them with the local server.
        fixture = StartFixture(self)
        fixture.environment["SIGMO_TEST_SSH_EXIT"] = "7"
        self.assertEqual(fixture.start().wait(timeout=8), 7, fixture.log.read_text())
        arguments = json.loads((fixture.directory / "ssh-arguments.json").read_text())
        arguments[-1] = f"{username}@127.0.0.1"
        self.command = [shutil.which("ssh"), *arguments]
        self.command[1:1] = [
            "-F", "/dev/null", "-p", str(self.ssh_port),
            "-i", str(self.directory / "client"),
            "-o", f"UserKnownHostsFile={known_hosts}",
        ]
        # Both fixture hosts share a network namespace, so their loopback ports must differ.
        self.command[self.command.index("-L") + 1] = f"127.0.0.1:{self.forward_source}:127.0.0.1:{self.source_port}"
        self.command[self.command.index("-R") + 1] = f"127.0.0.1:{self.forward_mcp}:127.0.0.1:{self.gateway_port}"

    def free_port(self):
        while True:
            with socket.socket() as listener:
                listener.bind(("127.0.0.1", 0))
                port = listener.getsockname()[1]
            if port not in self.ports:
                self.ports.add(port)
                return port

    def stop(self, process):
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)

    def spawn(self, command, log):
        output = log.open("wb")
        self.addCleanup(output.close)
        process = subprocess.Popen(command, stdout=output, stderr=subprocess.STDOUT)
        self.addCleanup(self.stop, process)
        return process

    def wait_port(self, port, process, log):
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            if process.poll() is not None:
                self.fail(f"SSH process exited: {log.read_text()}")
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                    return
            except OSError:
                time.sleep(0.05)
        self.fail(f"SSH forward did not open: {log.read_text()}")

    def serve(self, origin, port=0):
        test = self

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                test.requests.append((origin, self.path))
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream" if self.path == "/events" else "application/json")
                self.end_headers()
                if self.path == "/events":
                    self.wfile.write(b"data: first\n\n")
                    self.wfile.flush()
                    test.finish_stream.wait(timeout=5)
                    self.wfile.write(b"data: last\n\n")
                else:
                    self.wfile.write(json.dumps({"origin": origin, "path": self.path}).encode())

            def log_message(self, *args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()

        def close():
            test.finish_stream.set()
            server.shutdown()
            thread.join(timeout=5)
            server.server_close()

        self.addCleanup(close)
        self.ports.add(server.server_port)
        return server.server_port

    def get(self, port, path):
        with self.opener.open(f"http://127.0.0.1:{port}{path}", timeout=5) as response:
            return json.load(response)

    def start_tunnel(self):
        log = self.directory / f"tunnel-{time.monotonic_ns()}.log"
        process = self.spawn(self.command, log)
        self.wait_port(self.forward_mcp, process, log)
        self.wait_port(self.forward_source, process, log)
        return process

    def test_duplex_streams_and_manual_reconnect_preserve_gateway(self):
        tunnel = self.start_tunnel()
        with self.opener.open(f"http://127.0.0.1:{self.forward_mcp}/events", timeout=5) as stream:
            self.assertEqual(stream.readline(), b"data: first\n")
            self.assertEqual(self.get(self.forward_source, "/manifest.json")["origin"], "source")
            self.finish_stream.set()
            self.assertIn(b"data: last", stream.read())
        self.stop(tunnel)
        # The application remains reachable locally while only the tunnel is stopped.
        self.assertEqual(self.get(self.gateway_port, "/health")["origin"], "gateway")
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            try:
                with socket.create_connection(("127.0.0.1", self.forward_mcp), timeout=0.2):
                    time.sleep(0.05)
            except OSError:
                break
        self.start_tunnel()
        self.assertEqual(self.get(self.forward_mcp, "/mcp")["origin"], "gateway")
        self.assertEqual(self.get(self.forward_source, "/manifest.json")["origin"], "source")
        self.assertEqual(self.requests.count(("gateway", "/events")), 1)

    def test_remote_port_conflict_terminates_tunnel(self):
        self.serve("occupied", self.forward_mcp)
        result = subprocess.run(self.command, capture_output=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b"remote port forwarding failed", result.stderr)
        self.assertFalse(self.requests)


if __name__ == "__main__":
    unittest.main()
