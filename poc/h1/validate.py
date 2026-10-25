#!/usr/bin/env python3
"""Validate H-1 evidence consistency offline; never grant hardware acceptance."""
import argparse
import base64
import csv
import datetime as dt
import hashlib
import io
import json
import os
from pathlib import Path
import re
import stat

ID = re.compile(r"[0-9a-f]{32}\Z")
REVISION = re.compile(r"[0-9a-f]{40}\Z")
SHA = re.compile(r"[0-9a-f]{64}\Z")
LIMIT = 4 * 1024 * 1024


class EvidenceError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise EvidenceError(message)


def object_pairs(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON property")
        result[key] = value
    return result


def decode(raw):
    return json.loads(raw, object_pairs_hook=object_pairs, parse_constant=lambda _: require(False, "non-finite JSON number rejected"))


def read_file(root, name):
    """Open each relative path component without following symlinks."""
    require(isinstance(name, str) and name and not Path(name).is_absolute(), "relative artifact path required")
    parts = name.split("/")
    require(all(part not in ("", ".", "..") for part in parts), "artifact path traversal rejected")
    descriptor = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        file = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=descriptor)
        with os.fdopen(file, "rb") as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= LIMIT, "artifact must be a nonempty regular file of at most 4 MiB")
            raw = stream.read(LIMIT + 1)
            require(len(raw) <= LIMIT, "artifact exceeds size limit")
            return raw
    finally:
        os.close(descriptor)


def artifact(root, item):
    require(isinstance(item, dict) and isinstance(item.get("sha256"), str) and SHA.fullmatch(item["sha256"]), "artifact path and SHA256 required")
    raw = read_file(root, item.get("path"))
    require(hashlib.sha256(raw).hexdigest() == item["sha256"], "artifact digest mismatch")
    return raw


def instant(value):
    require(isinstance(value, str), "UTC observation timestamp required")
    parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(parsed.utcoffset() == dt.timedelta(0), "UTC observation timestamp required")
    return parsed


def fingerprint(raw):
    require(len(raw.decode("ascii").strip().splitlines()) == 1, "exactly one host public key required")
    fields = raw.decode("ascii").strip().split()
    require(len(fields) >= 2 and fields[0] == "ssh-ed25519", "independently collected Ed25519 host public key required")
    blob = base64.b64decode(fields[1], validate=True)
    require(blob[:19] == b"\x00\x00\x00\x0bssh-ed25519\x00\x00\x00\x20" and len(blob) == 51, "invalid Ed25519 public key")
    return "SHA256:" + base64.b64encode(hashlib.sha256(blob).digest()).decode().rstrip("=")


def page(raw):
    value = decode(raw)
    require(isinstance(value, dict) and isinstance(value.get("items"), list), "complete platform page required")
    require(value.get("page") == 1 and value.get("total") == len(value["items"]), "platform page is incomplete; use a dedicated ten-machine PoC project")
    require(all(isinstance(item, dict) for item in value["items"]), "invalid platform page item")
    return value["items"]


def check(manifest):
    root = manifest.parent.resolve()
    data = decode(read_file(root, manifest.name))
    require(isinstance(data, dict) and data.get("schema_version") == 1 and data.get("item") == "H-1", "H-1 schema_version 1 required")
    project = data.get("project_id", "")
    require(isinstance(project, str) and ID.fullmatch(project), "explicit project ID required")
    require(isinstance(data.get("platform_revision"), str) and REVISION.fullmatch(data["platform_revision"]), "exact platform revision required")
    health = decode(artifact(root, data.get("platform_health")))
    require(isinstance(health, dict) and health.get("status") == "ok" and health.get("version") == data["platform_revision"], "captured platform health version differs from the recorded release")
    require(all(isinstance(data.get(key), str) and data[key].strip() for key in ("environment", "operator", "provisioning_release_ref")), "environment, operator and provisioning release reference required")
    require(data.get("hardware_family") == "HGX B300", "H-1 requires ten HGX B300 machines")
    started, finished = instant(data.get("started_at")), instant(data.get("finished_at"))
    require(started <= finished <= dt.datetime.now(dt.timezone.utc), "invalid or future observation window")
    observations = data.get("operator_observations", {})
    require(isinstance(observations, dict) and all(observations.get(key) is True for key in ("http_first_boot", "automatic_trust", "customer_keys_preserved", "existing_dhcp_unaffected")), "all four operator observations must be explicitly true; missing PXE/automatic-trust evidence blocks H-1")
    rows = list(csv.DictReader(io.StringIO(artifact(root, data.get("bom")).decode("utf-8-sig"))))
    require(len(rows) == 10 and all(row.get("SN") and row.get("hostname") for row in rows), "BOM requires exactly ten serial/hostname rows")
    expected = {row["SN"]: row["hostname"] for row in rows}
    require(len(expected) == 10 and len(set(expected.values())) == 10, "BOM serials and hostnames must be unique")
    assets = page(artifact(root, data.get("assets")))
    require(len(assets) == 10, "dedicated PoC project must contain exactly ten assets")
    by_serial = {asset.get("serial_number"): asset for asset in assets}
    require(set(by_serial) == set(expected), "platform asset serials differ from frozen BOM")
    requests = page(artifact(root, data.get("trust_requests")))
    machines = data.get("machines")
    require(isinstance(machines, list) and len(machines) == 10 and all(isinstance(machine, dict) for machine in machines), "ten machine evidence records required")
    require({machine.get("serial") for machine in machines} == set(expected), "machine evidence differs from BOM")
    identities = set()
    commands = set()
    for machine in machines:
        serial = machine["serial"]
        asset = by_serial[serial]
        machine_id = asset.get("machine_id", "")
        require(isinstance(machine_id, str) and ID.fullmatch(machine_id) and machine_id not in identities, "unique platform machine identity required")
        identities.add(machine_id)
        require(asset.get("hostname") == expected[serial] and asset.get("state") == "provisioned" and asset.get("ssh_trust") == "established" and asset.get("ssh_trust_source") == "ssh", "current provisioned asset with real SSH trust and BOM hostname required")
        pin = fingerprint(artifact(root, machine.get("host_public_key")))
        matches = [request for request in requests if request.get("id") == machine.get("trust_request_id")]
        require(len(matches) == 1, "exact trust request required")
        request = matches[0]
        require(request.get("project_id") == project and request.get("machine_id") == machine_id and request.get("action") == "trust.bootstrap" and request.get("state") == "succeeded" and request.get("host_key") == pin, "successful pinned bootstrap proof required; repair does not prove automatic first boot")
        require(request.get("created_by") and request.get("approved_by") and request["created_by"] != request["approved_by"] and ID.fullmatch(request.get("execution_id", "")), "independent bootstrap approval and execution required")
        require(started <= instant(request.get("created_at")) <= finished, "bootstrap proof lies outside observation window")
        detail = decode(artifact(root, machine.get("identity_execution")))
        execution = detail.get("execution", {})
        require(execution.get("project_id") == project and execution.get("mode") == "ssh" and execution.get("state") == "success" and ID.fullmatch(execution.get("id", "")), "successful same-project SSH identity execution required")
        snapshot = execution.get("snapshot", {})
        targets = [target for target in snapshot.get("targets", []) if target.get("resource_key") == machine_id]
        require(snapshot.get("mode") == "ssh" and len(targets) == 1, "exact SSH target snapshot required")
        target = targets[0]
        require(target.get("project_id") == project and target.get("mode") == "ssh" and target.get("revision") == asset.get("version") and target.get("name") == expected[serial] and target.get("ssh", {}).get("host_key") == pin, "identity snapshot differs from current asset or independent host key")
        attempts = [attempt for attempt in detail.get("attempts", []) if attempt.get("target_id") == target.get("id")]
        require(len(attempts) == 1, "one exact identity attempt required")
        attempt = attempts[0]
        receipt = attempt.get("receipt") or {}
        require(isinstance(attempt.get("id"), str) and ID.fullmatch(attempt["id"]) and isinstance(target.get("id"), str) and ID.fullmatch(target["id"]) and attempt.get("state") == "success" and attempt.get("execution_id") == execution["id"] and receipt.get("result") == "success" and receipt.get("command_id") == attempt.get("command_id") and receipt.get("attempt_id") == attempt.get("id") and receipt.get("fencing_token") == attempt.get("fencing_token"), "matching successful durable receipt required")
        command = receipt.get("command_id", "")
        require(isinstance(command, str) and ID.fullmatch(command) and command not in commands and type(receipt.get("fencing_token")) is int and receipt["fencing_token"] > 0, "unique command and positive fencing token required")
        commands.add(command)
        output = receipt.get("output", {})
        observation = output.get("observation", {})
        require(output.get("mode") == "ssh" and output.get("source") == "host" and output.get("transport") == "ssh" and output.get("operation") == "host.identity" and observation.get("hostname") == expected[serial] and observation.get("os") and observation.get("arch"), "actual host.identity receipt and matching hostname required")
        require(started <= instant(receipt.get("started_at")) <= instant(receipt.get("finished_at")) <= finished, "identity receipt lies outside observation window")
        artifact(root, machine.get("boot_log"))
        artifact(root, machine.get("key_preservation"))
    return {"item": "H-1", "status": "READY_FOR_REVIEW", "machines": len(machines), "project_id": project, "hardware_acceptance": "UNVERIFIED", "note": "Offline consistency and file digests only. Operator must authenticate hardware, HTTP-first automation and customer-key preservation; no H-1 PASS is granted."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    args = parser.parse_args()
    try:
        result = check(args.manifest)
    except (OSError, ValueError, TypeError, KeyError, AttributeError, UnicodeError) as error:
        # Do not print file contents, cookies, configuration or exception text.
        print(json.dumps({"item": "H-1", "status": "BLOCKED", "hardware_acceptance": "UNVERIFIED", "reason": str(error) if isinstance(error, EvidenceError) else "missing, malformed or unsafe evidence"}))
        return 1
    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
