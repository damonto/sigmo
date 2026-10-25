#!/usr/bin/env python3
"""Keep the iOS WebKit regression fixture identical to the server bridge."""

import argparse
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Verify the fixture without changing it")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[3]
    source = root / "internal/app/websheet/bridge.js"
    fixture = root / "apps/ios/SigmoTests/Fixtures/carrier_bridge.js"
    expected = source.read_bytes()
    if args.check:
        if not fixture.exists() or fixture.read_bytes() != expected:
            parser.exit(1, "Carrier bridge fixture is stale; run python3 apps/ios/scripts/sync_carrier_bridge.py\n")
        print("Carrier bridge fixture matches internal/app/websheet/bridge.js")
    else:
        fixture.write_bytes(expected)
        print("Updated apps/ios/SigmoTests/Fixtures/carrier_bridge.js")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
