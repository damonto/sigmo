"""Serve saved iOS source snapshots for the Mac's pull-only development workflow."""

import argparse
import getpass
import gzip
import hashlib
import io
import json
import re
import shlex
import subprocess
import tarfile
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

REPO = Path(__file__).resolve().parents[2]


class Snapshots:
    def __init__(self, repo):
        self.repo = Path(repo)
        self.directory = self.repo / "build/ios/snapshots"
        self.lock = threading.Lock()
        self.manifest = None

    def source_files(self):
        names = subprocess.check_output(
            ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "ios", "scripts/ios"],
            cwd=self.repo,
        ).decode().split("\0")
        files = {}
        for name in sorted(set(names) - {""}):
            path = self.repo / name
            if path.is_symlink():
                raise ValueError(f"source symlinks are not supported: {name}")
            if path.is_file():
                files[name] = hashlib.sha256(path.read_bytes()).hexdigest()
        if not {"ios/project.yml", "scripts/ios/mcp_gateway.py"}.issubset(files):
            raise ValueError("iOS project and MCP gateway source are required")
        return files

    def archive_path(self, revision):
        if not re.fullmatch(r"[a-f0-9]{64}", revision):
            raise ValueError("invalid snapshot revision")
        return self.directory / f"{revision}.tar.gz"

    def current(self):
        with self.lock:
            files = self.source_files()
            commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=self.repo, text=True).strip()
            if self.manifest and files == self.manifest["files"] and commit == self.manifest["commit"]:
                return self.manifest
            buffer = io.BytesIO()
            with tarfile.open(fileobj=buffer, mode="w") as archive:
                for name, expected in files.items():
                    content = (self.repo / name).read_bytes()
                    if hashlib.sha256(content).hexdigest() != expected:
                        raise ValueError("source changed during snapshot; retry after saving files")
                    info = tarfile.TarInfo(name)
                    info.size = len(content)
                    info.mode = 0o644
                    archive.addfile(info, io.BytesIO(content))
            if files != self.source_files():
                raise ValueError("source changed during snapshot; retry after saving files")
            # Fixed timestamps keep a saved source revision stable across restarts.
            content = gzip.compress(buffer.getvalue(), mtime=0)
            revision = hashlib.sha256(content).hexdigest()
            self.directory.mkdir(parents=True, exist_ok=True)
            with tempfile.TemporaryDirectory(prefix=".snapshot-", dir=self.directory) as temporary:
                archive_path = Path(temporary) / "source.tar.gz"
                archive_path.write_bytes(content)
                # Publish only the complete archive. Readers never see a partial file.
                archive_path.replace(self.archive_path(revision))
            self.manifest = {
                "revision": revision,
                "commit": commit,
                "archive": f"/snapshots/{revision}.tar.gz",
                "files": files,
            }
            return self.manifest


def start_script(repo, ssh_target, source_address):
    """Embed connection defaults while keeping downloaded files usable with bash."""
    host, port = source_address
    if host == "0.0.0.0":
        host = "127.0.0.1"
    script = (Path(repo) / "scripts/ios/start-mac.sh").read_text()
    shebang, _, body = script.partition("\n")
    return (
        f"{shebang}\n"
        f"sigmo_default_ssh_target={shlex.quote(ssh_target)}\n"
        f"sigmo_default_source_host={shlex.quote(host)}\n"
        f"sigmo_default_source_port={port}\n"
        f"{body}"
    ).encode()


def handler(snapshots, ssh_target=None):
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            path = urlsplit(self.path).path
            try:
                if path == "/manifest.json":
                    return self.send_bytes(json.dumps(snapshots.current()).encode(), "application/json")
                if path == "/mcp_gateway.py":
                    return self.send_bytes((snapshots.repo / "scripts/ios/mcp_gateway.py").read_bytes(), "text/x-python")
                if path == "/start-mac.sh":
                    target = ssh_target
                    if not target:
                        host = urlsplit("//" + self.headers.get("Host", "")).hostname
                        if not host:
                            self.send_error(400, "Host header is required for the SSH target")
                            return
                        target = f"{getpass.getuser()}@{host}"
                    script = start_script(snapshots.repo, target, self.server.server_address)
                    return self.send_bytes(script, "text/x-shellscript")
                if path.startswith("/snapshots/") and path.endswith(".tar.gz"):
                    revision = path.removeprefix("/snapshots/").removesuffix(".tar.gz")
                    if not re.fullmatch(r"[a-f0-9]{64}", revision):
                        self.send_error(404)
                        return
                    archive = snapshots.archive_path(revision)
                    if archive.is_file():
                        return self.send_bytes(archive.read_bytes(), "application/gzip")
                self.send_error(404)
            except (OSError, ValueError, subprocess.SubprocessError) as error:
                self.send_error(503, str(error))

        def send_bytes(self, content, content_type):
            self.send_response(200)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(content)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(content)

        def log_message(self, format_string, *args):
            # Manifest polls are frequent; report errors and snapshot transfers only.
            if "/manifest.json" not in str(args):
                super().log_message(format_string, *args)

    return Handler


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", default=8766, type=int)
    parser.add_argument("--ssh-target", help="SSH alias or user@hostname to embed in the Mac bootstrap")
    args = parser.parse_args()
    if args.ssh_target and (args.ssh_target.startswith("-") or any(char.isspace() for char in args.ssh_target)):
        parser.error("SSH target must be a hostname, user@hostname, or SSH config alias")
    snapshots = Snapshots(REPO)
    source_url = f"http://{args.host}:{args.port}"
    server = ThreadingHTTPServer((args.host, args.port), handler(snapshots, args.ssh_target))
    print(f"Source snapshots: {source_url}/manifest.json", flush=True)
    print(f"Mac bootstrap: {source_url}/start-mac.sh", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
