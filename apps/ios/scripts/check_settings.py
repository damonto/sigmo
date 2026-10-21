#!/usr/bin/env python3
"""Check settings fixtures and localized resources without Xcode or a live backend."""
import json
from pathlib import Path
import plistlib
import re
import sys

ROOT = Path(__file__).resolve().parents[3]


def localized_strings(locale):
    path = ROOT / f"apps/ios/Sigmo/Resources/{locale}.lproj/Localizable.strings"
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'("(?:[^"\\]|\\.)*")\s*=\s*("(?:[^"\\]|\\.)*");', line.strip())
        if not match:
            if line.strip() and not line.lstrip().startswith("//"):
                raise ValueError(f"Unparsed string in {path}: {line}")
            continue
        key, value = map(json.loads, match.groups())
        if key in result:
            raise ValueError(f"Duplicate localization key: {locale}/{key}")
        result[key] = value
    return result


def intent_phrases(locale):
    path = ROOT / f"apps/ios/SigmoIntents/{locale}.lproj/AppIntentVocabulary.plist"
    with path.open("rb") as file:
        vocabulary = plistlib.load(file)
    phrases = {
        item["IntentName"]: item.get("IntentExamples", [])
        for item in vocabulary.get("IntentPhrases", [])
    }
    examples = phrases.get("INStartCallIntent", [])
    assert examples and all(isinstance(example, str) and example.strip() for example in examples), \
        f"Missing INStartCallIntent examples: {path}"
    return phrases


def main():
    en, zh = localized_strings("en"), localized_strings("zh-Hans")
    assert en.keys() == zh.keys(), f"Locale key mismatch: {en.keys() ^ zh.keys()}"
    source = (ROOT / "apps/ios/Sigmo/Settings/SettingsFixtureData.swift").read_text()
    fixture = json.loads(source.split('#"""', 1)[1].split('"""#', 1)[0])
    schema = fixture["schema"]
    expected_channels = {"telegram", "bark", "gotify", "sc3", "lark", "wecom", "http", "email"}
    assert {channel["key"] for channel in schema["channels"]} == expected_channels
    fixture_fields = schema["auth"] + schema["proxy"] + [field for channel in schema["channels"] for field in channel["fields"]]

    go_schema = (ROOT / "internal/app/handler/settings/schema.go").read_text()
    controls = {"text": "text", "password": "password", "number": "number", "list": "list", "select": "select"}
    expected_fields = {}
    for control, key, label in re.findall(r'(text|password|number|list|select)Field\(\s*"([^"]+)"\s*,\s*"([^"]+)"', go_schema):
        expected_fields[label] = (key, controls[control])
    for key, label, control in re.findall(r'Key:\s*"([^"]+)"[^{}]*?Label:\s*"([^"]+)"[^{}]*?Control:\s*control(\w+)', go_schema):
        expected_fields[label] = (key, control[0].lower() + control[1:])
    actual_fields = {field["label"]: (field["key"], field["control"]) for field in fixture_fields}
    assert actual_fields == expected_fields, f"Fixture/schema mismatch: {actual_fields.items() ^ expected_fields.items()}"

    def check_texts(value):
        if isinstance(value, dict):
            for key, item in value.items():
                if key in {"label", "description", "placeholder"}:
                    assert item in en, f"Missing schema localization: {item}"
                else:
                    check_texts(item)
        elif isinstance(value, list):
            for item in value:
                check_texts(item)
    check_texts(schema)
    used = set()
    ui_sources = list((ROOT / "apps/ios/Sigmo/Settings").glob("*.swift"))
    ui_sources.append(ROOT / "apps/ios/Sigmo/DiagnosticsView.swift")
    ui_sources += list((ROOT / "apps/ios/Sigmo/Devices").glob("ModemSettings*.swift"))
    ui_sources += list((ROOT / "apps/ios/Sigmo/Calls").glob("*.swift"))
    for path in ui_sources:
        if "Fixture" in path.name:
            continue
        used.update(re.findall(r'"((?:settings|modemSettings|calls|diagnostics)\.[A-Za-z][A-Za-z0-9_.]*)"', path.read_text()))
    # Prefixes used to construct dynamic keys are checked through fixture metadata.
    missing = {key for key in used if not key.endswith(".") and key not in en}
    assert not missing, f"Missing UI localizations: {missing}"
    for key in en:
        if key.startswith(("settings.", "modemSettings.", "calls.", "apiError.", "diagnostics.")):
            assert re.findall(r'%[0-9$]*[@dflds]', en[key]) == re.findall(r'%[0-9$]*[@dflds]', zh[key]), key
    intent_phrases("en")
    intent_phrases("zh-Hans")
    print(f"PASS: {len(en)} bilingual keys; {len(used)} settings/Calls/diagnostics UI keys; "
          f"{len(fixture_fields)} backend fields; 8 notification channels; 2 Siri intent phrase sets")


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, ValueError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
