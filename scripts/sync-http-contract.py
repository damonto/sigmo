"""Copy the canonical HTTP contract resources into the isolated iOS snapshot."""

import argparse
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    copies = {
        "internal/pkg/apierror/catalog.json": "ios/Sigmo/Resources/HTTPProblems.json",
        "internal/pkg/apierror/testdata/problems.json": "ios/SigmoTests/Fixtures/HTTPProblems.json",
    }
    for source, destination in copies.items():
        data = (root / source).read_bytes()
        target = root / destination
        if args.check:
            if not target.is_file() or target.read_bytes() != data:
                raise SystemExit(
                    f"{destination} is stale; run python3 scripts/sync-http-contract.py"
                )
        else:
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)


if __name__ == "__main__":
    main()
