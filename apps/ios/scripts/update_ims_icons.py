"""Build template PDF assets from the hand-drawn IMS SVGs using rsvg-convert."""

import json
import subprocess
from pathlib import Path


def main():
    source = Path(__file__).resolve().parent
    assets = source.parent / "Sigmo/Resources/Radio.xcassets"
    for name in ("ims-vowifi", "ims-volte"):
        destination = assets / f"{name}.imageset"
        destination.mkdir(parents=True, exist_ok=True)
        pdf = destination / f"{name}.pdf"
        subprocess.run(
            [
                "rsvg-convert",
                "--format=pdf",
                f"--output={pdf}",
                str(source / f"{name}.svg"),
            ],
            check=True,
        )
        catalog = {
            "images": [{"filename": pdf.name, "idiom": "universal"}],
            "info": {"author": "xcode", "version": 1},
            "properties": {
                "preserves-vector-representation": True,
                "template-rendering-intent": "template",
            },
        }
        (destination / "Contents.json").write_text(json.dumps(catalog, indent=2) + "\n")


if __name__ == "__main__":
    main()
