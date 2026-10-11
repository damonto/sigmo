"""Build native PDF flag assets from the Web client's installed flag-icons package.

Requires rsvg-convert. PDF preserves SVG <use> references and clipping that
Xcode's SVG asset importer does not consistently render.
"""

import json
import subprocess
from pathlib import Path


def main():
    ios = Path(__file__).resolve().parents[1]
    source = ios.parent / "web/node_modules/flag-icons"
    if not (source / "flags/4x3").is_dir():
        raise FileNotFoundError("Install the Web dependencies before rebuilding flags")
    assets = ios / "Sigmo/Resources/Flags.xcassets"
    for svg in sorted((source / "flags/4x3").glob("*.svg")):
        if len(svg.stem) != 2:
            continue
        destination = assets / f"flag-{svg.stem}.imageset"
        destination.mkdir(parents=True, exist_ok=True)
        pdf = destination / f"{svg.stem}.pdf"
        subprocess.run(
            ["rsvg-convert", "--format=pdf", f"--output={pdf}", str(svg)], check=True
        )
        catalog = {
            "images": [{"filename": pdf.name, "idiom": "universal"}],
            "info": {"author": "xcode", "version": 1},
            "properties": {"preserves-vector-representation": True},
        }
        (destination / "Contents.json").write_text(json.dumps(catalog, indent=2) + "\n")
        # Remove the earlier source asset so actool does not warn about unassigned files.
        (destination / svg.name).unlink(missing_ok=True)


if __name__ == "__main__":
    main()
