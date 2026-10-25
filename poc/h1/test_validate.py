"""Synthetic protocol-shaped evidence; these tests never assert hardware PASS."""
import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import validate


class EvidenceChecks(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="keel-h1-evidence-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.project = "a" * 32
        self.when = "2026-01-01T01:00:00Z"
        self.manifest = {"schema_version": 1, "item": "H-1", "project_id": self.project,
                         "platform_revision": "b" * 40, "environment": "SYNTHETIC-TEST-ONLY",
                         "operator": "test fixture", "provisioning_release_ref": "fixture-not-a-release",
                         "hardware_family": "HGX B300", "started_at": "2026-01-01T00:00:00Z", "finished_at": "2026-01-01T02:00:00Z",
                         "operator_observations": dict.fromkeys(("http_first_boot", "automatic_trust", "customer_keys_preserved", "existing_dhcp_unaffected"), True), "machines": []}
        assets, requests, bom = [], [], ["SN,hostname"]
        for index in range(10):
            identity = f"{index + 1:032x}"
            serial, hostname = f"TEST-{index}", f"fixture-{index}"
            bom.append(f"{serial},{hostname}")
            key = b"\x00\x00\x00\x0bssh-ed25519\x00\x00\x00\x20" + bytes([index]) * 32
            public = b"ssh-ed25519 " + base64.b64encode(key)
            pin = validate.fingerprint(public)
            request_id, execution_id, attempt_id, command_id = [f"{index + offset:032x}" for offset in (100, 200, 300, 400)]
            assets.append(dict(id=identity, machine_id=identity, serial_number=serial, hostname=hostname, version=1, state="provisioned", ssh_trust="established", ssh_trust_source="ssh"))
            requests.append(dict(id=request_id, project_id=self.project, machine_id=identity, action="trust.bootstrap", state="succeeded", host_key=pin, created_by="creator", approved_by="approver", execution_id=f"{index + 500:032x}", created_at=self.when))
            receipt = dict(command_id=command_id, attempt_id=attempt_id, fencing_token=1, result="success", started_at=self.when, finished_at=self.when, output=dict(mode="ssh", source="host", transport="ssh", hardware_evidence=False, operation="host.identity", observation=dict(hostname=hostname, os="fixture-os", arch="fixture-arch")))
            detail = dict(execution=dict(id=execution_id, project_id=self.project, mode="ssh", state="success", snapshot=dict(mode="ssh", targets=[dict(id=identity, resource_key=identity, project_id=self.project, mode="ssh", revision=1, name=hostname, ssh=dict(host_key=pin))])), attempts=[dict(id=attempt_id, target_id=identity, execution_id=execution_id, command_id=command_id, fencing_token=1, state="success", receipt=receipt)])
            self.manifest["machines"].append(dict(serial=serial, trust_request_id=request_id, host_public_key=self.artifact(f"key-{index}.pub", public), identity_execution=self.artifact(f"identity-{index}.json", detail), boot_log=self.artifact(f"boot-{index}.log", b"SYNTHETIC HTTP-FIRST FIXTURE"), key_preservation=self.artifact(f"keys-{index}.diff", b"SYNTHETIC KEY PRESERVATION FIXTURE")))
        self.manifest["bom"] = self.artifact("bom.csv", "\n".join(bom).encode())
        self.manifest["platform_health"] = self.artifact("health.json", dict(status="ok", version="b" * 40))
        self.manifest["assets"] = self.artifact("assets.json", dict(items=assets, total=10, page=1, page_size=50))
        self.manifest["trust_requests"] = self.artifact("requests.json", dict(items=requests, total=10, page=1, page_size=50))

    def artifact(self, name, value):
        raw = value if isinstance(value, bytes) else json.dumps(value).encode()
        (self.root / name).write_bytes(raw)
        return dict(path=name, sha256=hashlib.sha256(raw).hexdigest())

    def check(self):
        file = self.root / "evidence.json"
        file.write_text(json.dumps(self.manifest))
        return validate.check(file)

    def change(self, descriptor, transform):
        value = json.loads((self.root / descriptor["path"]).read_text())
        transform(value)
        descriptor.update(self.artifact(descriptor["path"], value))

    def test_complete_bundle_only_requests_review(self):
        result = self.check()
        self.assertEqual(result["status"], "READY_FOR_REVIEW")
        self.assertEqual(result["hardware_acceptance"], "UNVERIFIED")
        self.assertEqual(result["machines"], 10)

    def test_missing_or_ambiguous_proof_rejected(self):
        original = copy.deepcopy(self.manifest)
        changes = [lambda: self.manifest["operator_observations"].pop("http_first_boot"),
                   lambda: self.manifest["operator_observations"].update(automatic_trust=False),
                   lambda: self.manifest["machines"].pop(),
                   lambda: self.manifest["machines"].__setitem__(1, self.manifest["machines"][0]),
                   lambda: self.manifest.update(finished_at="9999-01-01T00:00:00Z"),
                   lambda: self.manifest["machines"][0]["boot_log"].update(sha256="0" * 64)]
        for mutation in changes:
            with self.subTest(mutation=changes.index(mutation)):
                self.manifest = copy.deepcopy(original)
                mutation()
                with self.assertRaises(ValueError): self.check()

    def test_simulated_cross_project_and_partial_exports_rejected(self):
        for field, transform in [("assets", lambda value: value["items"][0].update(ssh_trust_source="sim")),
                                 ("assets", lambda value: value.update(total=11)),
                                 ("trust_requests", lambda value: value["items"][0].update(project_id="c" * 32)),
                                 ("trust_requests", lambda value: value["items"][0].update(action="trust.repair")),
                                 ("trust_requests", lambda value: value["items"][0].update(approved_by="creator"))]:
            with self.subTest(field=field, transform=repr(transform)):
                descriptor = self.manifest[field]
                before = (self.root / descriptor["path"]).read_bytes()
                self.change(descriptor, transform)
                with self.assertRaises(ValueError): self.check()
                descriptor.update(self.artifact(descriptor["path"], before))

    def test_receipt_identity_hostname_and_pin_must_match(self):
        descriptor = self.manifest["machines"][0]["identity_execution"]
        before = (self.root / descriptor["path"]).read_bytes()
        for transform in [lambda value: value["attempts"][0]["receipt"].update(command_id="d" * 32),
                          lambda value: value["attempts"][0]["receipt"]["output"]["observation"].update(hostname="wrong"),
                          lambda value: value["execution"]["snapshot"]["targets"][0]["ssh"].update(host_key="wrong"),
                          lambda value: value["attempts"][0]["receipt"].update(fencing_token=2),
                          lambda value: value["execution"].update(mode="sim"),
                          lambda value: value["attempts"][0]["receipt"].update(finished_at="2025-01-01T00:00:00Z")]:
            self.change(descriptor, transform)
            with self.assertRaises(ValueError): self.check()
            descriptor.update(self.artifact(descriptor["path"], before))

    def test_unsafe_artifacts_and_duplicate_json_rejected(self):
        for path in ["../outside", "/etc/passwd", "a/../bom.csv", ""]:
            with self.subTest(path=path), self.assertRaises(ValueError): validate.read_file(self.root, path)
        (self.root / "link").symlink_to(self.root / "bom.csv")
        (self.root / "parent-link").symlink_to(self.root, target_is_directory=True)
        for path in ["link", "parent-link/bom.csv"]:
            with self.subTest(path=path), self.assertRaises(OSError): validate.read_file(self.root, path)
        with self.assertRaises(ValueError): validate.decode('{"item":"H-1","item":"H-16"}')
        with self.assertRaises(ValueError): validate.decode('{"number":NaN}')
        os.mkfifo(self.root / "fifo")
        with self.assertRaises(ValueError): validate.read_file(self.root, "fifo")
        (self.root / "oversized").write_bytes(b"x" * (validate.LIMIT + 1))
        with self.assertRaises(ValueError): validate.read_file(self.root, "oversized")

    def test_platform_version_must_match_captured_health(self):
        self.manifest["platform_revision"] = "c" * 40
        with self.assertRaises(ValueError): self.check()

    def test_cli_fails_without_secrets_or_traceback(self):
        file = self.root / "evidence.json"
        file.write_text('{"secret_fixture":"must-not-appear",')
        process = subprocess.run(["python3", str(Path(validate.__file__)), str(file)], capture_output=True, text=True)
        self.assertEqual(process.returncode, 1)
        self.assertEqual(json.loads(process.stdout)["status"], "BLOCKED")
        self.assertNotIn("must-not-appear", process.stdout + process.stderr)
        self.assertNotIn("Traceback", process.stderr)

    def test_unfilled_template_and_missing_wrapper_argument_block(self):
        template = Path(validate.__file__).with_name("evidence.template.json")
        process = subprocess.run(["python3", validate.__file__, str(template)], capture_output=True, text=True)
        self.assertEqual(process.returncode, 1)
        self.assertEqual(json.loads(process.stdout)["status"], "BLOCKED")
        wrapper = Path(validate.__file__).parent.parent / "run-h1.sh"
        process = subprocess.run(["sh", str(wrapper)], capture_output=True, text=True)
        self.assertEqual(process.returncode, 2)


if __name__ == "__main__":
    unittest.main()
