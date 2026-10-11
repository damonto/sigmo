"""Check synchronization without Xcode, a Mac, or a real source server."""

import hashlib
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import threading
import unittest
from unittest.mock import patch

from mcp_gateway import SourceSync


class SyncTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.sync = SourceSync("http://localhost:8766", Path(self.temporary.name) / "workspace")
        self.files = {"ios/project.yml": b"name: Sigmo\n", "ios/Sigmo/App.swift": b"// initial source\n"}

    def remote_files(self, files):
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w:gz") as output:
            for name, content in files.items():
                info = tarfile.TarInfo(name)
                info.size = len(content)
                output.addfile(info, io.BytesIO(content))
        data = archive.getvalue()
        manifest = {
            "revision": hashlib.sha256(data).hexdigest(),
            "archive": "/snapshots/example.tar.gz",
            "commit": "test",
            "files": {name: hashlib.sha256(content).hexdigest() for name, content in files.items()},
        }
        return {"manifest.json": json.dumps(manifest).encode(), "/snapshots/example.tar.gz": data}

    def generate(self, *args, **kwargs):
        (self.sync.workspace / "ios/Sigmo.xcodeproj").mkdir(exist_ok=True)

    def install(self, files):
        responses = self.remote_files(files)
        with patch.object(self.sync, "fetch", side_effect=responses.__getitem__), \
                patch("mcp_gateway.subprocess.run", side_effect=self.generate):
            self.sync.sync()

    def test_saved_changes_and_deletions_are_applied(self):
        self.install(self.files)
        changed = {"ios/project.yml": self.files["ios/project.yml"], "ios/Sigmo/New.swift": b"// new source\n"}
        self.install(changed)
        self.assertFalse((self.sync.workspace / "ios/Sigmo/App.swift").exists())
        self.assertEqual((self.sync.workspace / "ios/Sigmo/New.swift").read_bytes(), b"// new source\n")

    def test_local_edits_are_not_overwritten(self):
        self.install(self.files)
        path = self.sync.workspace / "ios/Sigmo/App.swift"
        path.write_bytes(b"// Mac edit\n")
        with self.assertRaisesRegex(ValueError, "changed locally"):
            self.install({**self.files, "ios/Sigmo/App.swift": b"// remote edit\n"})
        self.assertEqual(path.read_bytes(), b"// Mac edit\n")

    def test_bad_archive_is_rejected_before_writing(self):
        responses = self.remote_files(self.files)
        responses["/snapshots/example.tar.gz"] += b"corrupted"
        with patch.object(self.sync, "fetch", side_effect=responses.__getitem__):
            with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                self.sync.sync()
        self.assertFalse((self.sync.workspace / "ios/project.yml").exists())

    def test_paths_outside_the_source_scope_are_rejected(self):
        for name in ("../escape", "/ios/escape", "ios/../escape", "scripts/outside.py", "ios/./App.swift"):
            with self.subTest(path=name):
                responses = self.remote_files({name: b"bad"})
                with patch.object(self.sync, "fetch", side_effect=responses.__getitem__):
                    with self.assertRaisesRegex(ValueError, "invalid source manifest"):
                        self.sync.sync()

    def test_symlinked_parent_is_rejected_without_changing_its_files(self):
        self.install(self.files)
        original = self.sync.workspace / "ios/Sigmo"
        outside = Path(self.temporary.name) / "outside"
        original.rename(outside)
        original.symlink_to(outside, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "symlink"):
            self.install({**self.files, "ios/Sigmo/App.swift": b"// remote edit\n"})
        self.assertEqual((outside / "App.swift").read_bytes(), self.files["ios/Sigmo/App.swift"])

    def test_fetch_bypasses_environment_proxy(self):
        directory = Path(self.temporary.name)
        (directory / "manifest.json").write_text("source fixture")
        server = ThreadingHTTPServer(
            ("127.0.0.1", 0), partial(SimpleHTTPRequestHandler, directory=str(directory))
        )
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        source_url = f"http://127.0.0.1:{server.server_port}"
        try:
            # A fresh process prevents urllib's cached global opener hiding the proxy setting.
            result = subprocess.run(
                [sys.executable, "-c", """
import sys
from mcp_gateway import SourceSync
sync = SourceSync(sys.argv[1], sys.argv[2])
sys.stdout.buffer.write(sync.fetch("manifest.json"))
""", source_url, str(directory / "proxy-workspace")],
                cwd=Path(__file__).parent,
                env={**os.environ, "http_proxy": source_url, "no_proxy": "", "NO_PROXY": ""},
                capture_output=True, timeout=10,
            )
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertEqual(result.stdout, b"source fixture")
        finally:
            server.shutdown()
            thread.join()
            server.server_close()

    def test_xcodegen_failure_can_be_retried(self):
        responses = self.remote_files(self.files)
        with patch.object(self.sync, "fetch", side_effect=responses.__getitem__), \
                patch("mcp_gateway.subprocess.run", side_effect=subprocess.CalledProcessError(1, "xcodegen")):
            with self.assertRaises(subprocess.CalledProcessError):
                self.sync.sync()
        self.assertTrue(self.sync.state["needs_project_generation"])
        self.install(self.files)
        self.assertFalse(self.sync.state["needs_project_generation"])
        self.assertEqual(self.sync.last_error, "")

    def test_source_recovers_after_tunnel_disconnect(self):
        self.install(self.files)
        revision = self.sync.state["revision"]
        with patch.object(self.sync, "fetch", side_effect=ConnectionError("tunnel offline")):
            with self.assertRaises(ConnectionError):
                self.sync.sync()
        self.assertEqual(self.sync.state["revision"], revision)
        self.assertTrue(self.sync.last_error)
        self.install({**self.files, "ios/Sigmo/App.swift": b"// reconnected\n"})
        self.assertEqual(self.sync.last_error, "")
        self.assertEqual((self.sync.workspace / "ios/Sigmo/App.swift").read_bytes(), b"// reconnected\n")


if __name__ == "__main__":
    unittest.main()
