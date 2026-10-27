#!/usr/bin/env python3
"""Offline H-13 evidence consistency; never grants real hardware acceptance."""
import argparse
import csv
import hashlib
import importlib.util
import io
import json
from pathlib import Path

# Existing reader bounds files, checks hashes, rejects symlinks/duplicate keys.
_spec = importlib.util.spec_from_file_location("keel_h13_reader", Path(__file__).parents[1] / "h1" / "validate.py")
_reader = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_reader)
require, artifact, decode, instant = _reader.require, _reader.artifact, _reader.decode, _reader.instant


def document(root, reference):
    value = decode(artifact(root, reference))
    require(isinstance(value, dict), "JSON object evidence required")
    return value


def canonical_bdf(value):
    value = value.strip().lower()
    return value[4:] if len(value) == 16 and value.startswith("0000") else value


def vendor_cells(text):
    columns, cells, legend = [], {}, {}
    for line in text.splitlines():
        fields = line.split()
        if not fields:
            continue
        if ":" in line:
            key, name = line.strip().split(":", 1)
            if key.startswith("NIC") and key[3:].isdigit():
                require(key not in legend, "duplicate NVIDIA NIC Legend")
                legend[key] = name.strip()
        if not columns and fields[0].startswith("GPU") and fields[0][3:].isdigit():
            for field in fields:
                if (field.startswith("GPU") or field.startswith("NIC")) and field[3:].isdigit():
                    columns.append(field)
                else:
                    break
            continue
        if columns and fields[0] in columns:
            require(len(fields) >= len(columns) + 1, "incomplete NVIDIA affinity row")
            for column, token in zip(columns, fields[1:]):
                key = (fields[0], column)
                require(key not in cells, "duplicate NVIDIA affinity cell")
                cells[key] = token
    require(columns and cells, "NVIDIA topology matrix required")
    return cells, legend


def check_sample(root, data, sample, start, finish):
    require(sample.get("family") in ("HGX_B300", "GB300"), "B300/GB300 family required")
    expected_arch = "amd64" if sample["family"] == "HGX_B300" else "arm64"
    require(sample.get("arch") == expected_arch, "family/architecture mismatch")
    require(all(isinstance(sample.get(key), str) and sample[key].strip() for key in ("serial", "kernel", "nvidia_smi_version", "lspci_version")), "hardware and tool versions required")
    snapshot = document(root, sample.get("snapshot"))
    raw = artifact(root, sample.get("precollect_raw"))
    bundle = decode(raw)
    require(isinstance(bundle, dict), "original precollect object required")
    require(snapshot.get("project_id") == data["project_id"] and snapshot.get("asset_id") == sample.get("asset_id"), "snapshot project/asset mismatch")
    require(snapshot.get("origin") == bundle.get("origin") == "real", "simulated evidence cannot satisfy H-13")
    require(bundle.get("arch") == expected_arch and bundle.get("serial") == sample["serial"], "architecture/serial differs from original capture")
    require(hashlib.sha256(raw).hexdigest() == snapshot.get("raw_sha256"), "snapshot is not bound to original raw bytes")
    require(start <= instant(bundle.get("started_at")) <= instant(bundle.get("collected_at")) <= finish, "capture outside approved evidence window")
    require((instant(bundle["collected_at"]) - instant(bundle["started_at"])).total_seconds() <= 30, "capture exceeds 30 seconds")
    require(snapshot.get("verdict") == snapshot.get("effective_verdict") == "PASS" and snapshot.get("validity") == "fresh", "current topology PASS export required")
    exported = instant(sample.get("exported_at"))
    require(start <= instant(snapshot.get("created_at")) <= exported <= finish and exported < instant(snapshot.get("valid_until")), "snapshot was expired at evidence export")
    scope = snapshot.get("scope", {})
    require(scope.get("scope_hash") == snapshot.get("scope_hash") and scope.get("g1_id") == snapshot.get("g1_id") and scope.get("precollect_id") == snapshot.get("precollect_id"), "frozen topology identity differs")
    require(_reader.SHA.fullmatch(scope.get("scope_hash", "")), "scope digest required")
    gate = document(root, sample.get("g1"))
    evidence = gate.get("evidence", {})
    require(gate.get("effective_verdict") == evidence.get("verdict") == "PASS" and evidence.get("validity") == "fresh" and exported < instant(evidence.get("valid_until")), "effective G1 PASS required")
    require(evidence.get("id") == snapshot["g1_id"] and evidence.get("precollect_id") == snapshot["precollect_id"] and evidence.get("scope_hash") == scope.get("precollect_scope_hash"), "G1 and topology refer to different original captures")
    published = document(root, sample.get("published_model"))
    versions = published.get("items", [published])
    versions = [version for version in versions if version.get("id") == snapshot.get("model_version_id")]
    require(len(versions) == 1, "exact published model version required")
    model = versions[0]
    require(model.get("content_hash") == scope.get("model_content_hash") and model.get("expected_topology") == scope.get("expectation"), "independent published expectation differs")
    provenance = model.get("expected_topology", {}).get("provenance", {})
    require(provenance.get("kind") in ("vendor_spec", "approved_golden") and provenance.get("reference"), "independent specification provenance required")
    baseline = artifact(root, sample.get("independent_specification"))
    require(hashlib.sha256(baseline).hexdigest() == provenance.get("sha256"), "independent source document digest differs")
    capture = snapshot.get("snapshot", {})
    require(capture.get("version") == "keel.pcie-topology/v1" and not capture.get("missing"), "complete PCIe snapshot required")
    require(bundle.get("pci_nodes") == capture.get("nodes"), "snapshot sysfs fields differ from retained observations")
    probes = {(probe.get("kind"), probe.get("target", "")): probe for probe in bundle.get("probes", [])}
    for name in ("gpu_identity", "gpu_topology", "nvlink", "pci_topology"):
        require(probes.get((name, ""), {}).get("state") == "ready", f"successful original {name} read required")
    cells, legend = vendor_cells(probes[("gpu_topology", "")].get("stdout", ""))
    observed = {(cell.get("from"), cell.get("to")): cell for cell in capture.get("affinity", [])}
    require(set(cells) == set(observed), "platform and NVIDIA affinity scopes differ")
    for key, value in cells.items():
        require(observed[key].get("observed") == value, "NVIDIA affinity token was rewritten")
        if not value.startswith("NV"):
            require(observed[key].get("computed") == value, "sysfs affinity does not match NVIDIA semantics")
    identities = {}
    for row in csv.reader(io.StringIO(probes[("gpu_identity", "")].get("stdout", ""))):
        require(len(row) == 4, "GPU index/UUID/BDF/serial evidence required")
        label = "GPU" + str(int(row[0].strip()))
        require(label not in identities, "duplicate GPU index")
        identities[label] = (row[1].strip(), canonical_bdf(row[2]), row[3].strip())
    for device in capture.get("devices", []):
        if device.get("kind") == "gpu":
            require(identities.get(device.get("label")) == (device.get("identity"), device.get("bdf"), device.get("serial")), "GPU labels/serial/BDF do not match original NVIDIA identity")
        else:
            require(legend.get(device.get("label")) == device.get("identity"), "NIC identity was inferred from ordering")
    require(capture.get("nvlinks") and all(link.get("state") == "active" for link in capture["nvlinks"]), "active NVIDIA NVLink observations required for these GPU platforms")
    nodes = {node.get("bdf"): node for node in capture.get("nodes", [])}
    for device in capture.get("devices", []):
        node = nodes.get(device.get("bdf"), {})
        require(node.get("root") and isinstance(node.get("upstream"), list) and node.get("numa") is not None, "sysfs path and NUMA required")
        require(all(node.get(field) is not None for field in ("max_speed_gt", "max_width", "speed_gt", "width")), "LnkCap/LnkSta observations required")
    detail = document(root, sample.get("execution"))
    execution = detail.get("execution", {})
    require(execution.get("project_id") == data["project_id"] and execution.get("mode") == "ssh" and execution.get("state") == "success" and execution.get("parameters", {}).get("validation_kind") == "precollect", "successful original dedicated SSH precollect execution required")
    attempts = detail.get("attempts", [])
    require(detail.get("attempts_total") == len(attempts), "complete attempt export required")
    matching = []
    for attempt in attempts:
        receipt = attempt.get("receipt") or {}
        output = receipt.get("output") or {}
        if output.get("precollect_record_id") != snapshot["precollect_id"]:
            continue
        require(attempt.get("state") == receipt.get("result") == "success" and receipt.get("attempt_id") == attempt.get("id") and receipt.get("command_id") == attempt.get("command_id") and receipt.get("fencing_token", 0) > 0 and receipt.get("fencing_token") == attempt.get("fencing_token"), "original stopped fenced receipt differs")
        require(output.get("stopped") is True and output.get("evidence_hash") == snapshot["raw_sha256"], "receipt does not bind original raw object")
        matching.append(attempt)
    require(len(matching) == 1, "exactly one original matching attempt required")
    artifact(root, sample.get("screen_capture"))


def check(manifest):
    manifest = Path(manifest)
    root = manifest.parent.resolve()
    data = decode(_reader.read_file(root, manifest.name))
    require(data.get("schema") == "keel.h13.pcie/v1" and data.get("item") == "H-13", "H-13 schema required")
    require(data.get("status") == "COLLECTED", "H-13 has not been run; collect real evidence first")
    require(_reader.ID.fullmatch(data.get("project_id", "")) and _reader.REVISION.fullmatch(data.get("platform_commit", "")), "exact project and platform commit required")
    require(data.get("environment") and data.get("approval_ref"), "controlled environment and approved window required")
    start, finish = instant(data.get("started_at")), instant(data.get("finished_at"))
    require(start < finish, "valid evidence window required")
    health = document(root, data.get("platform_health"))
    require(health.get("status") == "ok" and health.get("version") == data["platform_commit"], "platform revision differs from manifest")
    samples = data.get("samples", [])
    require(isinstance(samples, list) and len(samples) >= 2 and {sample.get("family") for sample in samples} == {"HGX_B300", "GB300"}, "both B300 and GB300 real samples required")
    require(len({sample.get("asset_id") for sample in samples}) == len(samples), "duplicate PoC asset")
    for sample in samples:
        check_sample(root, data, sample, start, finish)
    return {"item": "H-13", "status": "READY_FOR_REVIEW", "hardware_acceptance": "UNVERIFIED", "note": "Offline consistency only; owner review of hardware identity, independent specification, UI fidelity and B300/GB300 capability remains required."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(check(args.manifest), ensure_ascii=False))
    except (ValueError, OSError, TypeError, KeyError, OverflowError) as exc:
        print(json.dumps({"item": "H-13", "status": "BLOCKED", "hardware_acceptance": "UNVERIFIED", "error": str(exc)}, ensure_ascii=False))
        raise SystemExit(1)


if __name__ == "__main__":
    main()
