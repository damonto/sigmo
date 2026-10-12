"""Exercise immutable source snapshots and HTTP delivery without Apple tools."""

from concurrent.futures import ThreadPoolExecutor
import hashlib
from http.server import ThreadingHTTPServer
import io
import json
import getpass
from pathlib import Path
import subprocess
import tarfile
import tempfile
import threading
import unittest
from unittest.mock import patch
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from mcp_source import Snapshots, handler, start_script


class SnapshotTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repo = Path(self.temporary.name)
        (self.repo / "apps/ios").mkdir(parents=True)
        (self.repo / "scripts/ios").mkdir(parents=True)
        (self.repo / ".gitignore").write_text("build/\n*.local.xcconfig\n")
        (self.repo / "apps/ios/project.yml").write_text("name: Sigmo\n")
        (self.repo / "apps/ios/App.swift").write_text("// committed source\n")
        (self.repo / "scripts/ios/mcp_gateway.py").write_text("# gateway fixture\n")
        (self.repo / "scripts/ios/start-mac.sh").write_bytes(Path(__file__).with_name("start-mac.sh").read_bytes())
        (self.repo / "server-private.txt").write_text("outside the source scope\n")
        for command in (
            ["git", "init", "-q"],
            ["git", "add", "."],
            ["git", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
             "commit", "-qm", "test(ios): Add snapshot fixture"],
        ):
            subprocess.run(command, cwd=self.repo, check=True)
        self.snapshots = Snapshots(self.repo)

    def contents(self, manifest):
        content = self.snapshots.archive_path(manifest["revision"]).read_bytes()
        self.assertEqual(hashlib.sha256(content).hexdigest(), manifest["revision"])
        with tarfile.open(fileobj=io.BytesIO(content), mode="r:gz") as archive:
            files = {item.name: archive.extractfile(item).read() for item in archive}
        self.assertEqual(
            {name: hashlib.sha256(data).hexdigest() for name, data in files.items()}, manifest["files"]
        )
        return files

    def test_saved_uncommitted_files_are_included_and_private_files_are_excluded(self):
        (self.repo / "apps/ios/App.swift").write_text("// saved edit\n")
        (self.repo / "apps/ios/New.swift").write_text("// new source\n")
        (self.repo / "apps/ios/Signing.local.xcconfig").write_text("private configuration\n")
        files = self.contents(self.snapshots.current())
        self.assertEqual(files["apps/ios/App.swift"], b"// saved edit\n")
        self.assertIn("apps/ios/New.swift", files)
        self.assertNotIn("apps/ios/Signing.local.xcconfig", files)
        self.assertNotIn("server-private.txt", files)
        self.assertFalse(any(name.startswith("build/") for name in files))

    def test_concurrent_polls_and_restarts_reuse_the_same_revision(self):
        with ThreadPoolExecutor(max_workers=2) as pool:
            manifests = list(pool.map(lambda _: self.snapshots.current(), range(2)))
        self.assertEqual(manifests[0], manifests[1])
        self.assertEqual(Snapshots(self.repo).current(), manifests[0])
        self.assertEqual(len(list(self.snapshots.directory.glob("*.tar.gz"))), 1)

    def test_edits_and_deletions_preserve_earlier_snapshots(self):
        first = self.snapshots.current()
        original = self.contents(first)
        (self.repo / "apps/ios/App.swift").unlink()
        (self.repo / "apps/ios/New.swift").write_text("// replacement\n")
        second = self.snapshots.current()
        self.assertNotEqual(first["revision"], second["revision"])
        self.assertNotIn("apps/ios/App.swift", self.contents(second))
        self.assertEqual(self.contents(first), original)

    def test_source_changes_during_snapshot_do_not_publish_a_manifest(self):
        original_add = tarfile.TarFile.addfile

        def add_and_change(archive, info, content):
            original_add(archive, info, content)
            if info.name == "apps/ios/App.swift":
                (self.repo / "apps/ios/App.swift").write_text("// concurrent edit\n")

        with patch.object(tarfile.TarFile, "addfile", add_and_change):
            with self.assertRaisesRegex(ValueError, "source changed during snapshot"):
                self.snapshots.current()
        self.assertIsNone(self.snapshots.manifest)
        self.assertFalse(self.snapshots.directory.exists())

    def test_symlinks_and_missing_project_are_rejected(self):
        for kind in ("symlink", "missing project"):
            with self.subTest(kind=kind):
                if kind == "symlink":
                    link = self.repo / "apps/ios/External.swift"
                    link.symlink_to(self.repo / "server-private.txt")
                    with self.assertRaisesRegex(ValueError, "symlinks"):
                        self.snapshots.current()
                    link.unlink()
                else:
                    (self.repo / "apps/ios/project.yml").unlink()
                    with self.assertRaisesRegex(ValueError, "required"):
                        self.snapshots.current()

    def test_http_delivers_verified_snapshots_and_rejects_unmanaged_paths(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), handler(self.snapshots))
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        try:
            base = f"http://127.0.0.1:{server.server_port}"
            with urlopen(base + "/manifest.json") as response:
                manifest = json.load(response)
            with urlopen(base + manifest["archive"]) as response:
                self.assertEqual(hashlib.sha256(response.read()).hexdigest(), manifest["revision"])
            with urlopen(base + "/start-mac.sh") as response:
                script = response.read()
                self.assertEqual(script, start_script(self.repo, f"{getpass.getuser()}@127.0.0.1", server.server_address))
                subprocess.run(["bash", "-n"], input=script, check=True)
            for suffix in ("/server-private.txt", "/snapshots/../../server-private.txt.tar.gz", "/snapshots/missing.tar.gz"):
                with self.subTest(path=suffix), self.assertRaises(HTTPError) as error:
                    urlopen(base + suffix)
                with error.exception as response:
                    self.assertEqual(response.code, 404)
        finally:
            server.shutdown()
            thread.join()
            server.server_close()

    def test_bootstrap_uses_the_requested_host_and_configured_ssh_target(self):
        for target in (None, "developer@stable-dev"):
            with self.subTest(target=target):
                server = ThreadingHTTPServer(("127.0.0.1", 0), handler(self.snapshots, target))
                thread = threading.Thread(target=server.serve_forever)
                thread.start()
                try:
                    request = Request(
                        f"http://127.0.0.1:{server.server_port}/start-mac.sh",
                        headers={"Host": "10.10.10.101:8766"},
                    )
                    with urlopen(request) as response:
                        script = response.read()
                    expected = target or f"{getpass.getuser()}@10.10.10.101"
                    self.assertEqual(script, start_script(self.repo, expected, server.server_address))
                finally:
                    server.shutdown()
                    thread.join()
                    server.server_close()

    def test_bootstrap_quotes_defaults_and_preserves_the_source_binding(self):
        for host, expected_host in (("10.10.10.101", "10.10.10.101"), ("0.0.0.0", "127.0.0.1")):
            with self.subTest(host=host):
                # Only evaluate the generated assignments, not the actual launcher.
                target = "developer@host'$(exit 99)"
                script = start_script(self.repo, target, (host, 8766)).decode()
                defaults = "\n".join(script.splitlines()[:4])
                result = subprocess.run(
                    ["bash", "-c", defaults + '\nprintf "%s\\n%s" "$sigmo_default_ssh_target" "$sigmo_default_source_host"'],
                    capture_output=True, text=True, check=True,
                )
                self.assertEqual(result.stdout, target + "\n" + expected_host)


if __name__ == "__main__":
    unittest.main()
