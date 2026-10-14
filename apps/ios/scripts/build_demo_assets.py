#!/usr/bin/env python3
"""Bundle the public MCP skill for offline demo downloads."""

from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parents[3]
SOURCE = ROOT / "skills"
OUTPUT = ROOT / "apps/ios/Sigmo/Resources/DemoControl.zip"


def main() -> None:
    with zipfile.ZipFile(OUTPUT, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for path in sorted((SOURCE / "sigmo-control").rglob("*")):
            if not path.is_file():
                continue
            entry = zipfile.ZipInfo(path.relative_to(SOURCE).as_posix())
            entry.compress_type = zipfile.ZIP_DEFLATED
            entry.external_attr = 0o100644 << 16
            archive.writestr(entry, path.read_bytes())
    print(f"Wrote {OUTPUT.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
