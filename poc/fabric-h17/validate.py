#!/usr/bin/env python3
"""Validate the N-S5 subset of H-17 offline; never grant hardware or G2 PASS."""
import argparse
import datetime as dt
import importlib.util
import json
from pathlib import Path

# Reuse the existing bounded, no-symlink, duplicate-key rejecting evidence reader.
_spec = importlib.util.spec_from_file_location("keel_h17_evidence_reader", Path(__file__).parents[1] / "h1" / "validate.py")
_reader = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_reader)
require, artifact, decode = _reader.require, _reader.artifact, _reader.decode

CASES = {
    "switch-link-up": ("cable", "precheck_pass", ""),
    "uninstalled-host": ("cable", "pending", ""),
    "no-light": ("cable", "anomaly", "T1"),
    "wrong-port": ("cable", "anomaly", "T2"),
    "missing-side": ("cable", "pending", ""),
    "optics-overlimit": ("port", "anomaly", "T3"),
    "wrong-rail": ("port", "anomaly", "T2"),
    "physical-crc": ("port", "anomaly", "T4"),
    "configuration-mismatch": ("port", "anomaly", "T6"),
}


def document(root, ref):
    value = decode(artifact(root, ref))
    require(isinstance(value, dict), "JSON object evidence required")
    return value


def check(manifest):
    manifest = Path(manifest)
    root = manifest.parent.resolve()
    data = decode(_reader.read_file(root, manifest.name))
    require(data.get("schema") == "keel.h17.ns5/v1" and data.get("item") == "H-17" and data.get("scope") == "N-S5", "H-17 N-S5 evidence schema required")
    require(data.get("status") == "COLLECTED", "PoC has not been run; collect device evidence before review")
    require(_reader.ID.fullmatch(data.get("project_id", "")) and _reader.REVISION.fullmatch(data.get("platform_commit", "")), "exact project and platform commit required")
    require(data.get("environment") and data.get("approval_ref"), "controlled environment and approval reference required")
    start, end = _reader.instant(data.get("started_at")), _reader.instant(data.get("finished_at"))
    require(start < end <= dt.datetime.now(dt.timezone.utc), "ordered observation window required")
    equipment = data.get("equipment", {})
    for key, minimum in (("switches", 2), ("hosts", 8)):
        devices = equipment.get(key, [])
        require(isinstance(devices, list) and len(devices) >= minimum, f"H-17 requires at least {minimum} {key}")
        serials = [d.get("serial") for d in devices if isinstance(d, dict)]
        require(len(serials) == len(devices) and all(isinstance(s, str) and s.strip() for s in serials) and len(set(serials)) == len(serials), "unique physical serials required")
    cases = data.get("cases", [])
    require(isinstance(cases, list) and len(cases) == len(CASES) and {c.get("name") for c in cases if isinstance(c, dict)} == set(CASES), "nine unique N-S5 positive and negative cases required")
    for case in cases:
        name = case["name"]
        kind, state, category = CASES[name]
        report = document(root, case.get("artifact"))
        if kind == "port":
            report = report.get("report", {})
            require(report.get("project_id") == data["project_id"], f"{name}: project mismatch")
            observed = _reader.instant(report.get("collected_at"))
            candidates = [check for port in report.get("ports", []) if port.get("port") == case.get("port") and port.get("in_use") for check in port.get("checks", [])]
            matches = [c for c in candidates if c.get("state") == state and c.get("issue_type") == category]
            if name == "wrong-rail":
                matches = [c for c in matches if c.get("subtype") == "rail"]
            if name == "optics-overlimit":
                matches = [c for c in matches if c.get("threshold_ref") and report.get("ddm_limits")]
        else:
            require(_reader.ID.fullmatch(report.get("id", "")), f"{name}: immutable collection reference required")
            require(report.get("project_id") == data["project_id"], f"{name}: project mismatch")
            observed = _reader.instant(report.get("collected_at"))
            matches = [f for f in report.get("findings", []) if f.get("cable_uid") == case.get("cable_id") and f.get("verdict") == state and f.get("issue_type", "") == category]
            if name == "uninstalled-host":
                matches = [f for f in matches if f.get("reason") == "server_side_pending"]
            if name == "missing-side":
                matches = [f for f in matches if f.get("reason") == "switch_evidence_missing"]
        require(report.get("source", "switch") == "switch", f"{name}: switch source required")
        require(report.get("stage") == "N-S5" and report.get("np3") == "BLOCKED" and report.get("evidence_source") == "device", f"{name}: N-S5 device evidence must not manufacture NP-3")
        require(start <= observed <= end, f"{name}: observation outside PoC window")
        require(matches and all(m.get("source", "switch") == "switch" and "@" in m.get("rule_ref", "") for m in matches), f"{name}: expected classification and versioned switch rule missing")
        # Original NOS outputs are reviewed by the owner; file completeness and
        # digests do not prove a real device or a particular fault injection.
        artifact(root, case.get("raw_device_artifact"))
    repair = data.get("repair", {})
    issue = document(root, repair.get("issue_artifact")).get("issue", {})
    require(issue.get("project_id") == data["project_id"] and issue.get("id") == repair.get("issue_id") and issue.get("source") == "fabric" and issue.get("category") in ("T1", "T2") and issue.get("state") == "closed", "closed original T1/T2 issue required")
    history = _reader.page(artifact(root, repair.get("verification_artifact")))
    passes = [v for v in history if v.get("verdict") == "PASS" and v.get("request", {}).get("issue_id") == issue["id"]]
    require(passes, "repair has no persisted PASS verification")
    detail = document(root, repair.get("execution_artifact"))
    execution = detail.get("execution", {})
    require(execution.get("project_id") == data["project_id"] and execution.get("mode") == "fabric" and execution.get("state") == "success", "successful scoped verification execution required")
    verified = [v for v in passes if v["request"].get("execution_id") == execution.get("id") and v["request"].get("snapshot", {}).get("evidence_origin") == "device" and v.get("applied_issue_version") == issue.get("version", 0)-1]
    require(verified, "closed issue version, device provenance and verification execution differ")
    verification = verified[0]
    request = verification["request"]
    params = execution.get("parameters", {})
    require(params.get("issue_verification_id") == request.get("id") and params.get("issue_id") == issue["id"] and params.get("scope_hash") == request.get("scope_hash"), "execution is not the frozen issue verification")
    require(_reader.SHA.fullmatch(request.get("scope_hash", "")) and _reader.ID.fullmatch(verification.get("evidence_id", "")), "immutable scope and evidence references required")
    attempts = detail.get("attempts", [])
    require(detail.get("attempts_total") == 1 and len(attempts) == 1, "complete single-link verification attempt required")
    attempt = attempts[0]
    receipt = attempt.get("receipt", {})
    require(attempt.get("state") == "success" and receipt.get("result") == "success" and receipt.get("command_id") == attempt.get("command_id") and receipt.get("attempt_id") == attempt.get("id") and receipt.get("fencing_token", 0) > 0 and receipt.get("fencing_token") == attempt.get("fencing_token"), "matching successful fenced collector receipt required")
    require(receipt.get("output", {}).get("evidence_id") == verification["evidence_id"] and receipt.get("output", {}).get("issue_verification_id") == request.get("id") and receipt.get("output", {}).get("evidence_source") == "device", "receipt does not reference the original device verification evidence")
    require(_reader.instant(request.get("created_at")) <= _reader.instant(issue.get("updated_at")) <= _reader.instant(verification.get("valid_until")), "issue was not closed within verification validity")

    return {"item": "H-17", "scope": "N-S5", "status": "READY_FOR_REVIEW", "hardware_acceptance": "UNVERIFIED", "full_h17": "NOT_RUN", "g2_np3": "NOT_RUN", "note": "Offline consistency only. Owner review and later G2 three-source wiring/PortModel evidence remain required."}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(check(args.manifest), ensure_ascii=False))
    except (ValueError, OSError, TypeError, KeyError) as exc:
        print(json.dumps({"item": "H-17", "scope": "N-S5", "status": "BLOCKED", "hardware_acceptance": "UNVERIFIED", "error": str(exc)}))
        raise SystemExit(1)
