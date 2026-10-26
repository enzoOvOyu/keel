#!/usr/bin/env python3
"""Check H-16 evidence completeness and file digests; never assert hardware PASS."""
import hashlib
import json
import re
import sys
from pathlib import Path

SHA = re.compile(r"[0-9a-f]{64}\Z")
REQUIRED = {"matched-239", "matched-143", "matched-60-43", "unknown-isolated", "conflict-rejected"}
KINDS = {"dhcp-pcap", "device-log", "platform-observation"}


def check(manifest: Path) -> list[str]:
    errors = []
    root = manifest.parent.resolve()
    data = json.loads(manifest.read_text())
    if data.get("item") != "H-16" or not data.get("environment") or not data.get("kea_approval_ref"):
        errors.append("H-16 environment and Kea approval reference required")
    before, after = data.get("kea_active_hash_before"), data.get("kea_active_hash_after_restart")
    if not isinstance(before, str) or not SHA.fullmatch(before) or before != after:
        errors.append("Kea active hash must be valid and stable after restart")
    cases = data.get("cases", [])
    if not isinstance(cases, list) or {c.get("name") for c in cases if isinstance(c, dict)} != REQUIRED or len(cases) != 5:
        errors.append("five unique H-16 cases are required")
        cases = []
    serials = set()
    for case in cases:
        name = case["name"]
        if not isinstance(case.get("serial"), str) or not case["serial"].strip():
            errors.append(f"{name}: serial required")
        if name.startswith("matched-") and isinstance(case.get("serial"), str) and case["serial"].strip():
            serials.add(case.get("serial"))
        channel, verdict = {"matched-239": ("239", "matched"), "matched-143": ("143", "matched"), "matched-60-43": ("60-43", "matched"), "unknown-isolated": ("239", "isolated"), "conflict-rejected": ("239", "conflict")}[name]
        if case.get("channel") != channel or case.get("verdict") != verdict:
            errors.append(f"{name}: channel/verdict mismatch")
        if name == "matched-60-43" and (not case.get("vendor_profile_ref") or not case.get("option60_value") or not isinstance(case.get("option43_sha256"), str) or not SHA.fullmatch(case["option43_sha256"])):
            errors.append("matched-60-43: explicit vendor profile, option60 and option43 payload digest required")
        artifacts = case.get("artifacts", [])
        if not isinstance(artifacts, list) or {a.get("kind") for a in artifacts if isinstance(a, dict)} != KINDS:
            errors.append(f"{name}: DHCP, device and platform artifacts required")
            continue
        for artifact in artifacts:
            verify_artifact(root, artifact, errors)
    if len(serials) < 2:
        errors.append("at least two matched physical switch serials required")
    for field in ("first_boot_minimal_config_artifact", "pushjob_approval_artifact", "golden_readback_artifact"):
        value = data.get(field)
        if not isinstance(value, dict):
            errors.append(f"{field} required")
        else:
            verify_artifact(root, value, errors)
    return errors


def verify_artifact(root: Path, item: dict, errors: list[str]) -> None:
    name, expected = item.get("path"), item.get("sha256")
    if not isinstance(name, str) or not isinstance(expected, str) or not SHA.fullmatch(expected):
        errors.append("artifact path and SHA256 required")
        return
    candidate = root / name
    file = candidate.resolve()
    if candidate.is_symlink() or not file.is_relative_to(root) or not file.is_file():
        errors.append(f"artifact missing or outside package: {name}")
        return
    digest = hashlib.sha256()
    with file.open('rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(chunk)
    if digest.hexdigest() != expected:
        errors.append(f"artifact digest mismatch: {name}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: validate.py evidence.json")
    problems = check(Path(sys.argv[1]))
    if problems:
        for problem in problems:
            print("BLOCKED", problem)
        raise SystemExit(1)
    print("READY_FOR_REVIEW: evidence files match manifest; H-16 PASS requires human review")
