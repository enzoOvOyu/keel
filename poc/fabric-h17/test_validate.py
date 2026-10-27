"""Synthetic offline validator cases, never real H-17 device evidence."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("keel_h17_validator", Path(__file__).with_name("validate.py"))
validator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(validator)


class H17PackageTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.data = json.loads(Path(__file__).with_name("evidence.template.json").read_text())
        self.data.update(status="COLLECTED", project_id="a"*32, platform_commit="b"*40, started_at="2026-01-01T00:00:00Z", finished_at="2026-01-01T00:10:00Z")
        self.documents = {}
        for case in self.data["cases"]:
            kind, state, category = validator.CASES[case["name"]]
            report = {"id":"c"*32,"project_id":"a"*32,"stage":"N-S5","np3":"BLOCKED","evidence_source":"device","collected_at":"2026-01-01T00:01:00Z"}
            finding = {"source":"switch","rule_ref":"SYNTHETIC-TEST@1","issue_type":category,"verdict":state,"state":state}
            if kind == "cable":
                finding.update(cable_uid=case["cable_id"],reason="server_side_pending" if case["name"]=="uninstalled-host" else "switch_evidence_missing" if case["name"]=="missing-side" else "synthetic")
                report["findings"]=[finding]
            else:
                finding.update(subtype="rail",threshold_ref="fabric.ddm.*")
                report.update(ports=[{"port":case["port"],"in_use":True,"checks":[finding]}],ddm_limits={"rx_dbm":{"warn_min":-10}})
                report={"report":report}
            self.documents[case["name"]]=report
            self.save(case["artifact"],report)
            self.save(case["raw_device_artifact"],{"synthetic":"not physical evidence"})
        repair=self.data["repair"];repair["issue_id"]="d"*32
        self.issue={"issue":{"id":"d"*32,"project_id":"a"*32,"source":"fabric","category":"T1","state":"closed","version":8,"updated_at":"2026-01-01T00:03:00Z"}}
        request={"id":"e"*32,"issue_id":"d"*32,"execution_id":"f"*32,"scope_hash":"1"*64,"created_at":"2026-01-01T00:02:00Z","snapshot":{"evidence_origin":"device"}}
        self.verification={"page":1,"total":1,"items":[{"verdict":"PASS","request":request,"evidence_id":"2"*32,"applied_issue_version":7,"valid_until":"2026-01-01T00:07:00Z"}]}
        receipt={"attempt_id":"3"*32,"command_id":"4"*32,"fencing_token":1,"result":"success","output":{"evidence_id":"2"*32,"issue_verification_id":"e"*32,"evidence_source":"device"}}
        self.execution={"execution":{"id":"f"*32,"project_id":"a"*32,"mode":"fabric","state":"success","parameters":{"issue_verification_id":"e"*32,"issue_id":"d"*32,"scope_hash":"1"*64}},"attempts_total":1,"attempts":[{"id":"3"*32,"command_id":"4"*32,"state":"success","fencing_token":1,"receipt":receipt}]}
        self.save(repair["issue_artifact"],self.issue);self.save(repair["verification_artifact"],self.verification);self.save(repair["execution_artifact"],self.execution)

    def save(self,ref,value):
        raw=json.dumps(value).encode();path=self.root/ref["path"];path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(raw);ref["sha256"]=hashlib.sha256(raw).hexdigest()

    def check(self):
        manifest=self.root/"evidence.json";manifest.write_text(json.dumps(self.data));return validator.check(manifest)

    def test_consistent_files_remain_review_only(self):
        result=self.check()
        self.assertEqual(result["status"],"READY_FOR_REVIEW")
        self.assertEqual(result["hardware_acceptance"],"UNVERIFIED")
        self.assertEqual(result["g2_np3"],"NOT_RUN")
        self.assertEqual(result["full_h17"],"NOT_RUN")

    def test_fixture_origin_and_false_np3_block(self):
        case=self.data["cases"][0];report=self.documents[case["name"]]
        for field,value in (("evidence_source","protocol_fixture"),("np3","PASS")):
            with self.subTest(field=field):
                original=report[field];report[field]=value;self.save(case["artifact"],report)
                with self.assertRaises(ValueError):self.check()
                report[field]=original

    def test_closing_after_expiry_and_foreign_fence_block(self):
        self.issue["issue"]["updated_at"]="2026-01-01T00:09:00Z";self.save(self.data["repair"]["issue_artifact"],self.issue)
        with self.assertRaises(ValueError):self.check()
        self.issue["issue"]["updated_at"]="2026-01-01T00:03:00Z";self.save(self.data["repair"]["issue_artifact"],self.issue)
        self.execution["attempts"][0]["receipt"]["fencing_token"]=2;self.save(self.data["repair"]["execution_artifact"],self.execution)
        with self.assertRaises(ValueError):self.check()

    def test_missing_cases_and_path_traversal_block(self):
        case=self.data["cases"].pop()
        with self.assertRaises(ValueError):self.check()
        self.data["cases"].append(case);case["artifact"]["path"]="../outside.json"
        with self.assertRaises(ValueError):self.check()
