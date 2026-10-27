"""Authored offline validator cases; execution deferred to the milestone."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

_spec = importlib.util.spec_from_file_location("keel_h13", Path(__file__).with_name("validate.py"))
validator = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(validator)


class H13EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.fixture = json.loads((Path(__file__).parents[2] / "sim/fixtures/nvidia-smi-topo/baseline.json").read_text())
        self.project = "1" * 32
        self.start = "2026-10-27T09:00:00Z"
        self.observed = "2026-10-27T09:00:01Z"
        self.exported = "2026-10-27T09:00:04Z"
        self.finish = "2026-10-27T09:00:05Z"
        self.expiry = "2026-10-27T10:00:00Z"

    def artifact(self, name, value):
        raw = value if isinstance(value, bytes) else json.dumps(value).encode()
        (self.root / name).write_bytes(raw)
        return {"path": name, "sha256": hashlib.sha256(raw).hexdigest()}

    def sample(self, family, arch, number):
        f = copy.deepcopy(self.fixture)
        asset = str(number) * 32
        model, record, gate = "a" * 32, str(number + 2) * 32, str(number + 4) * 32
        source = b"synthetic topology specification v1"
        bundle = {"origin": "real", "serial": family + "-SYNTHETIC", "arch": arch,
                  "started_at": self.start, "collected_at": self.observed,
                  "pci_nodes": f["snapshot"]["nodes"], "probes": []}
        for kind, text in f["raw"].items():
            bundle["probes"].append({"kind": kind, "target": "", "state": "ready", "stdout": text})
        bundle["probes"].append({"kind": "pci_topology", "target": "", "state": "ready", "stdout": "synthetic tree"})
        raw_ref = self.artifact(family + "-raw.json", bundle)
        scope = {"scope_hash": "b" * 64, "g1_id": gate, "precollect_id": record,
                 "precollect_scope_hash": "c" * 64, "model_content_hash": "d" * 64,
                 "expectation": f["expectation"]}
        snapshot = {"id": "f" * 32, "project_id": self.project, "asset_id": asset,
                    "origin": "real", "raw_sha256": raw_ref["sha256"], "scope_hash": scope["scope_hash"],
                    "g1_id": gate, "precollect_id": record, "model_version_id": model,
                    "verdict": "PASS", "effective_verdict": "PASS", "validity": "fresh",
                    "created_at": "2026-10-27T09:00:02Z", "valid_until": self.expiry,
                    "scope": scope, "snapshot": f["snapshot"]}
        g1 = {"effective_verdict": "PASS", "evidence": {"id": gate, "precollect_id": record,
              "scope_hash": scope["precollect_scope_hash"], "verdict": "PASS", "validity": "fresh", "valid_until": self.expiry}}
        published = {"id": model, "content_hash": scope["model_content_hash"], "expected_topology": f["expectation"]}
        receipt = {"result": "success", "attempt_id": "e" * 32, "command_id": "f" * 32,
                   "fencing_token": 1, "output": {"precollect_record_id": record, "stopped": True, "evidence_hash": raw_ref["sha256"]}}
        execution = {"execution": {"project_id": self.project, "mode": "ssh", "state": "success", "parameters": {"validation_kind": "precollect"}},
                     "attempts_total": 1, "attempts": [{"id": "e" * 32, "command_id": "f" * 32, "fencing_token": 1, "state": "success", "receipt": receipt}]}
        return {"family": family, "arch": arch, "asset_id": asset, "serial": bundle["serial"],
                "kernel": "synthetic", "nvidia_smi_version": "synthetic", "lspci_version": "synthetic", "exported_at": self.exported,
                "snapshot": self.artifact(family + "-snapshot.json", snapshot), "precollect_raw": raw_ref,
                "g1": self.artifact(family + "-g1.json", g1), "published_model": self.artifact(family + "-model.json", published),
                "independent_specification": self.artifact(family + "-spec.txt", source),
                "execution": self.artifact(family + "-execution.json", execution), "screen_capture": self.artifact(family + "-screen.txt", b"synthetic screen; no hardware claim")}

    def manifest(self):
        data = {"schema": "keel.h13.pcie/v1", "item": "H-13", "status": "COLLECTED",
                "project_id": self.project, "platform_commit": "a" * 40, "environment": "synthetic unit input", "approval_ref": "synthetic",
                "platform_health": self.artifact("health.json", {"status": "ok", "version": "a" * 40}),
                "started_at": self.start, "finished_at": self.finish,
                "samples": [self.sample("HGX_B300", "amd64", 1), self.sample("GB300", "arm64", 2)]}
        path = self.root / "manifest.json"
        path.write_text(json.dumps(data))
        return path, data

    def test_consistency_never_grants_hardware_pass(self):
        path, _ = self.manifest()
        result = validator.check(path)
        self.assertEqual(result["status"], "READY_FOR_REVIEW")
        self.assertEqual(result["hardware_acceptance"], "UNVERIFIED")

    def test_unrun_template_cannot_pass(self):
        template = Path(__file__).with_name("evidence.template.json")
        with self.assertRaisesRegex(ValueError, "has not been run"):
            validator.check(template)

    def test_cross_arch_missing_gpu_and_expired_export_rejected(self):
        for change in ("arch", "identity", "expiry", "sim", "receipt"):
            with self.subTest(change=change):
                path, data = self.manifest()
                sample = data["samples"][0]
                if change == "arch":
                    sample["arch"] = "arm64"
                elif change == "identity":
                    snap = json.loads((self.root / sample["snapshot"]["path"]).read_text())
                    snap["snapshot"]["devices"][0]["identity"] = "GPU-invented"
                    sample["snapshot"] = self.artifact(sample["snapshot"]["path"], snap)
                elif change == "expiry":
                    sample["exported_at"] = self.expiry
                elif change == "sim":
                    snap = json.loads((self.root / sample["snapshot"]["path"]).read_text())
                    snap["origin"] = "sim"
                    sample["snapshot"] = self.artifact(sample["snapshot"]["path"], snap)
                else:
                    execution = json.loads((self.root / sample["execution"]["path"]).read_text())
                    execution["attempts"][0]["receipt"]["output"]["stopped"] = False
                    sample["execution"] = self.artifact(sample["execution"]["path"], execution)
                path.write_text(json.dumps(data))
                with self.assertRaises(ValueError):
                    validator.check(path)

    def test_vendor_parser_preserves_nvidia_tokens_and_rejects_duplicates(self):
        cells, legend = validator.vendor_cells(self.fixture["raw"]["gpu_topology"])
        self.assertEqual(cells[("GPU0", "GPU1")], "NV2")
        self.assertEqual(legend["NIC0"], "eth0")
        with self.assertRaises(ValueError):
            validator.vendor_cells(self.fixture["raw"]["gpu_topology"] + "\nNIC0: other0\n")


if __name__ == "__main__":
    unittest.main()
