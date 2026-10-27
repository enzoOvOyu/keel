package execution

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"github.com/enzoOvOyu/keel/internal/wiring"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/pcitopology"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/precollect"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

// Only external hardware/transport is replaced. All snapshots, gates and
// business transitions below are produced by the actual platform services.
// A real-origin protocol branch in a disposable DB is not hardware PoC.
type p2PrecollectRunner struct {
	binding  taskrunner.Binding
	bundle   precollect.Bundle
	raw      []byte
	original taskrunner.Observation
}

func (r *p2PrecollectRunner) Binding(string, string) (taskrunner.Binding, error) {
	return r.binding, nil
}
func (r *p2PrecollectRunner) Do(_ context.Context, request taskrunner.Request) (taskrunner.Observation, error) {
	if request.Validate() != nil {
		return taskrunner.Observation{}, taskrunner.ErrRejected
	}
	if request.Action == "run" {
		r.bundle.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
		r.bundle.CollectedAt = time.Now().UTC().Truncate(time.Microsecond).Add(999 * time.Nanosecond)
		r.raw, _ = json.Marshal(r.bundle)
		r.original = taskrunner.Observation{Command: request.Command, State: "success", StartedAt: r.bundle.StartedAt, FinishedAt: time.Now().UTC(), Output: map[string]any{"stopped": true, "reason": "precollect_observed", "precollect_version": precollect.Version, "precollect_sha256": precollect.Hash(r.raw), "precollect_size": len(r.raw)}}
	}
	if request.Command != r.original.Command {
		return taskrunner.Observation{}, taskrunner.ErrRejected
	}
	if request.Action == "read" {
		if request.ChunkSHA256 != precollect.Hash(r.raw) || request.ChunkOffset >= int64(len(r.raw)) {
			return taskrunner.Observation{}, taskrunner.ErrRejected
		}
		chunk := r.raw[request.ChunkOffset:min(request.ChunkOffset+precollect.ChunkBytes, int64(len(r.raw)))]
		return taskrunner.Observation{Command: request.Command, State: "accepted", Output: map[string]any{"precollect_chunk_offset": request.ChunkOffset, "precollect_size": len(r.raw), "precollect_sha256": precollect.Hash(r.raw), "chunk_sha256": precollect.Hash(chunk), "chunk": base64.StdEncoding.EncodeToString(chunk)}}, nil
	}
	return r.original, nil
}

type p2Chain struct {
	*bmcVerificationFixture
	pkg, model, modelVersion, port, nic, switchID, switchPort, cable, switchBatch string
	runner                                                                        *p2PrecollectRunner
	topology                                                                      *platform.PciTopologySnapshot
}

func p2PositiveG1Topology(t *testing.T) *p2Chain {
	t.Helper()
	ctx := context.Background()
	v := credentialVerificationFixture(t, "real")
	f := &p2Chain{bmcVerificationFixture: v}
	store := v.store
	execute := func(query string, args ...any) {
		t.Helper()
		_, e := store.DB.ExecContext(ctx, query, args...)
		mustAgent(t, e)
	}
	var base struct {
		Expectation map[string]any       `json:"expectation"`
		Snapshot    pcitopology.Snapshot `json:"snapshot"`
		Raw         map[string]string    `json:"raw"`
	}
	raw, e := os.ReadFile("../../sim/fixtures/nvidia-smi-topo/baseline.json")
	mustAgent(t, e)
	mustAgent(t, json.Unmarshal(raw, &base))
	project, e := store.GetProject(ctx, v.actor, v.project)
	mustAgent(t, e)
	_, e = store.UpsertParamKey(ctx, v.actor, project, platform.UpsertParamKeyInput{Key: "gate.evidence_ttl", ValueType: "string", DefaultValue: "24h", CreateOnly: true}, "p2-chain")
	mustAgent(t, e)
	model, e := store.CreateDeviceModel(ctx, v.actor, project, platform.CreateDeviceModelInput{Name: "P2 independent protocol model " + platform.RandomID()[:8], Kind: "server"}, "p2-chain")
	mustAgent(t, e)
	version, e := store.PublishModelVersion(ctx, v.actor, project, model.ID, platform.PublishModelVersionInput{HwSpec: map[string]any{"kind": "server", "cpu_arch": "amd64", "gpu_count": 2}, BomExpect: map[string]any{"gpu_count": 2, "nic_count": 1}, ExpectedTopology: base.Expectation, DefaultRolePlan: map[string]any{"version": "p2-model-roles-v1"}, PanelLayout: map[string]any{"rows": 1, "columns": 1, "ports": []map[string]any{{"id": "nic1", "label": "NIC 1", "row": 1, "column": 1, "slot_no": 3}}}, Slots: []platform.ModelSlot{{SlotNo: 3, PcieSlot: "PCI-E3", ExpectNicModel: "P2 NIC", PanelPort: "1", DefaultRole: "compute"}}}, "p2-chain")
	mustAgent(t, e)
	f.model, f.modelVersion = model.ID, version.ID
	execute(`UPDATE project_assets SET model_id=$2,model_version_id=$3,version=version+1 WHERE id=$1`, v.asset, model.ID, version.ID)
	var serial string
	mustAgent(t, store.DB.QueryRowContext(ctx, `SELECT serial_number FROM machines WHERE id=$1`, v.machine).Scan(&serial))
	// Frozen transport configuration supplies only metadata to the platform; the
	// read-only command itself goes through the retained protocol runner above.
	_, private, e := ed25519.GenerateKey(rand.Reader)
	mustAgent(t, e)
	signer, e := ssh.NewSignerFromKey(private)
	mustAgent(t, e)
	directory := t.TempDir()
	key := filepath.Join(directory, "key.pem")
	der, e := x509.MarshalPKCS8PrivateKey(private)
	mustAgent(t, e)
	mustAgent(t, os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600))
	pin := ssh.FingerprintSHA256(signer.PublicKey())
	config := filepath.Join(directory, "ssh.json")
	raw, _ = json.Marshal(map[string]any{"targets": []taskrunner.TargetConfig{{ProjectID: v.project, MachineID: v.machine, Address: "127.0.0.1:65530", User: "keel-ctrl", HostKey: pin, CredentialRef: "p2-fixture-key", PrivateKeyFile: key}}})
	mustAgent(t, os.WriteFile(config, raw, 0600))
	store.SSHRunner, e = taskrunner.Load(config)
	mustAgent(t, e)
	t.Cleanup(func() { store.SSHRunner.Close() })
	binding, e := store.SSHRunner.Binding(v.project, v.machine)
	mustAgent(t, e)
	execute(`UPDATE machines SET ssh_trust='established',ssh_trust_source='ssh',host_key_fingerprint=$2,bmc_address='127.0.0.1' WHERE id=$1`, v.machine, pin)
	f.pkg, f.nic, f.port, f.switchID, f.switchPort, f.cable = platform.RandomID(), platform.RandomID(), platform.RandomID(), platform.RandomID(), platform.RandomID(), platform.RandomID()
	profile := platform.RandomID()
	f.switchBatch = "p2-switch-" + platform.RandomID()[:8]
	execute(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at)VALUES($1,$2,$3,'approved',$4,now())`, profile, v.project, f.pkg, v.actor.User.ID)
	execute(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at)VALUES($1,$2,$1,'bom-import/v1',$1,'sha256:'||repeat('b',64),'independent protocol fixture','{}',8,'committed',$3,now())`, f.pkg, v.project, v.actor.User.ID)
	revision := func(id string, line int, values map[string]string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"values": values})
		execute(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at)VALUES($1,$2,$3,$4,$5,now())`, f.pkg, id, line, v.project, raw)
	}
	revision(v.machine, 1, map[string]string{"model_ref": version.ID, "credential_batch_ref": v.identity.Ref, "serial_number": serial})
	for i, component := range []struct{ id, kind, serial, slot string }{{platform.RandomID(), "gpu", serial + "-GPU0", "PCI-E1"}, {platform.RandomID(), "gpu", serial + "-GPU1", "PCI-E2"}, {f.nic, "nic", serial + "-NIC", "PCI-E3"}} {
		execute(`INSERT INTO bom_items(item_uid,category,parent_uid,serial,source_of_record,first_project_id,created_at,updated_at)VALUES($1,$2,$3,$4,'design_bom',$5,now(),now())`, component.id, component.kind, v.machine, component.serial, v.project)
		execute(`INSERT INTO components(id,machine_id,category,serial_number,slot,created_at)VALUES($1,$2,$3,$4,$5,now())`, component.id, v.machine, component.kind, component.serial, component.slot)
		revision(component.id, i+2, map[string]string{"slot": component.slot})
	}
	mac := "02:" + f.port[:2] + ":" + f.port[2:4] + ":" + f.port[4:6] + ":" + f.port[6:8] + ":" + f.port[8:10]
	execute(`INSERT INTO bom_items(item_uid,category,parent_uid,port_index,port_name,source_of_record,first_project_id,created_at,updated_at)VALUES($1,'port',$2,'1','nic-compute-p1','design_bom',$3,now(),now())`, f.port, f.nic, v.project)
	execute(`INSERT INTO physical_ports(id,machine_id,parent_uid,port_index,mac_address,port_name,created_at)VALUES($1,$2,$3,'1',$4,'nic-compute-p1',now())`, f.port, v.machine, f.nic, mac)
	execute(`UPDATE bom_items SET port_role='compute',rail='rail-1' WHERE item_uid=$1`, f.port)
	revision(f.port, 5, map[string]string{"port_index": "1", "port_name": "nic-compute-p1", "port_role": "compute", "rail": "rail-1"})
	execute(`INSERT INTO bom_items(item_uid,category,serial,credential_batch_ref,source_of_record,first_project_id,created_at,updated_at)VALUES($1,'switch',$4,$2,'design_bom',$3,now(),now())`, f.switchID, f.switchBatch, v.project, "P2-SWITCH-"+f.switchID[:8])
	execute(`INSERT INTO switches(id,serial_number,hostname,created_at,updated_at)VALUES($1,$2,'p2-switch',now(),now())`, f.switchID, "P2-SWITCH-"+f.switchID[:8])
	revision(f.switchID, 6, map[string]string{"credential_batch_ref": f.switchBatch})
	execute(`INSERT INTO bom_credential_batches(project_id,credential_batch_ref,scope,rotation_policy,vault_path,owner,package_id,updated_at)VALUES($1,$2,'switch','manual','vault://p2/switch','fixture',$3,now())`, v.project, f.switchBatch, f.pkg)
	execute(`INSERT INTO bom_items(item_uid,category,parent_uid,port_index,port_name,source_of_record,first_project_id,created_at,updated_at)VALUES($1,'port',$2,'1','swp1','design_bom',$3,now(),now())`, f.switchPort, f.switchID, v.project)
	execute(`INSERT INTO physical_ports(id,switch_id,parent_uid,port_index,port_name,created_at)VALUES($1,$2,$2,'1','swp1',now())`, f.switchPort, f.switchID)
	execute(`UPDATE bom_items SET rail='rail-1' WHERE item_uid=$1`, f.switchPort)
	revision(f.switchPort, 7, map[string]string{"port_index": "1", "port_name": "swp1"})
	a, b := f.port, f.switchPort
	if a > b {
		a, b = b, a
	}
	execute(`INSERT INTO bom_items(item_uid,category,endpoint_a_uid,endpoint_b_uid,source_of_record,first_project_id,created_at,updated_at)VALUES($1,'cable',$2,$3,'design_bom',$4,now(),now())`, f.cable, a, b, v.project)
	revision(f.cable, 8, map[string]string{})
	v.documentsMu.Lock()
	v.documents = map[string][]byte{}
	document := func(path string, value any) {
		value.(map[string]any)["@odata.id"] = path
		v.documents[path], _ = json.Marshal(value)
	}
	document("/redfish/v1/Managers", map[string]any{"@odata.type": "#ManagerCollection.ManagerCollection", "Members@odata.count": 1, "Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/BMC"}}})
	document("/redfish/v1/Managers/BMC", map[string]any{"@odata.type": "#Manager.v1_0_0.Manager", "Id": "BMC", "EthernetInterfaces": map[string]string{"@odata.id": "/redfish/v1/Managers/BMC/EthernetInterfaces"}})
	document("/redfish/v1/Managers/BMC/EthernetInterfaces", map[string]any{"@odata.type": "#EthernetInterfaceCollection.EthernetInterfaceCollection", "Members@odata.count": 1, "Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/BMC/EthernetInterfaces/mgmt0"}}})
	document("/redfish/v1/Managers/BMC/EthernetInterfaces/mgmt0", map[string]any{"@odata.type": "#EthernetInterface.v1_0_0.EthernetInterface", "Id": "mgmt0", "MACAddress": "02:00:00:00:00:ff", "IPv4Addresses": []map[string]string{{"Address": "127.0.0.1"}}})
	v.documentsMu.Unlock()
	var assetVersion int64
	mustAgent(t, store.DB.QueryRowContext(ctx, `SELECT version FROM project_assets WHERE id=$1`, v.asset).Scan(&assetVersion))
	bmcExecution, e := v.service.CreateBMCCollection(ctx, v.actor, v.project, v.asset, platform.BMCCollectionInput{Level: "L3", ExpectedAssetVersion: assetVersion, Reason: "P2 protocol identity and address"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	mustAgent(t, v.service.RunClaim(ctx, claimBMC(t, v.agentKernelFixture, bmcExecution.ID)))
	gpu0, gpu1 := "GPU-"+v.machine, "GPU-"+f.nic
	replace := strings.NewReplacer("GPU-aaaaaaaa-aaaa", gpu0, "GPU-bbbbbbbb-bbbb", gpu1, "SIM-GPU-GPU0", serial+"-GPU0", "SIM-GPU-GPU1", serial+"-GPU1")
	for key, value := range base.Raw {
		base.Raw[key] = replace.Replace(value)
	}
	runner := &p2PrecollectRunner{binding: binding}
	f.runner = runner
	configBefore := []precollect.NetConfig{{Name: "eth0", MAC: mac, MTU: 1500, AdminUp: true}}
	runner.bundle = precollect.Bundle{Version: precollect.Version, Origin: "real", Serial: serial, Arch: "amd64", OSRelease: "ID=p2-protocol-fixture", PCINodes: base.Snapshot.Nodes, ConfigBefore: configBefore, ConfigAfter: configBefore, Interfaces: []precollect.InterfaceSeed{{Name: "eth0", BDF: "0000:05:00.0"}}, RDMADevices: []string{"mlx5_0"}, Probes: []precollect.Probe{
		{Kind: "gpu", State: "ready", Stdout: gpu0 + ", 00000000:03:00.0, 81920, fixture-driver, 0\n" + gpu1 + ", 00000000:04:00.0, 81920, fixture-driver, 0\n"},
		{Kind: "gpu_identity", State: "ready", Stdout: base.Raw["gpu_identity"]}, {Kind: "gpu_topology", State: "ready", Stdout: base.Raw["gpu_topology"]}, {Kind: "nvlink", State: "ready", Stdout: base.Raw["nvlink"]}, {Kind: "pci_topology", State: "ready", Stdout: "retained synthetic sysfs PCIe tree"},
		{Kind: "nic_permanent", Target: "eth0", State: "ready", Stdout: "Permanent address: " + mac + "\n"}, {Kind: "nic_link", Target: "eth0", State: "ready", Stdout: "Speed: 100000Mb/s\nLink detected: yes\n"}, {Kind: "nic_driver", Target: "eth0", State: "ready", Stdout: "driver: mlx5_core\nversion: fixture-driver\nbus-info: 0000:05:00.0\nfirmware-version: fixture\n"}, {Kind: "nic_dom", Target: "eth0", State: "ready", Stdout: "Module temperature: 35.00 degrees C\nReceiver signal average optical power: 1.0 mW / -2.0 dBm\nLaser output power: 1.0 mW / -1.0 dBm\n"}, {Kind: "rdma_links", State: "ready", Stdout: `[{"ifname":"mlx5_0","netdev":"eth0","port":1,"state":"ACTIVE"}]`}, {Kind: "ibv_devices", State: "ready", Stdout: "mlx5_0 0000000000000001\n"},
	}}
	store.PrecollectEnabled = true
	v.service.MediaStore = store
	v.service.SSHRunner = runner
	current := f.collect(t)
	if current.EffectiveVerdict != "PASS" {
		t.Fatalf("positive G1: %+v", current)
	}
	f.topology = f.validateTopology(t)
	if f.topology.EffectiveVerdict != "PASS" {
		t.Fatalf("positive topology: %+v", f.topology)
	}
	t.Logf("software protocol chain G1=%s topology=%s; no hardware PoC", current.Evidence.ID, f.topology.ID)
	return f
}
func TestP2PositiveOriginalG1AndTopologyPostgres(t *testing.T) {
	f := p2PositiveG1Topology(t)
	relations, e := f.store.TopologyRelations(context.Background(), f.actor, f.project, f.asset, f.topology.ID)
	mustAgent(t, e)
	if relations.EffectiveVerdict != "PASS" || len(relations.Rows) != 2 {
		t.Fatalf("original Step1 relations: %+v", relations)
	}
}

func (f *p2Chain) collect(t *testing.T) *platform.CurrentG1 {
	t.Helper()
	ctx := context.Background()
	preview, e := f.store.PreviewPrecollect(ctx, f.actor, f.project, f.asset)
	mustAgent(t, e)
	if !preview.Ready {
		t.Fatalf("positive G1 prerequisites: %s", preview.Code)
	}
	collection, e := f.service.CreatePrecollect(ctx, f.actor, f.project, PrecollectCreateInput{Targets: []PrecollectCreateTarget{{AssetID: f.asset, ExpectedScopeHash: preview.ScopeHash}}, Reason: "Complete retained protocol capture"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	claim := f.claim(t, collection.ID)
	mustAgent(t, f.service.RunClaim(ctx, claim))
	_, e = f.store.ReconcilePrecollect(ctx, 10)
	mustAgent(t, e)
	current, e := f.store.CurrentG1(ctx, f.actor, f.project, f.asset)
	mustAgent(t, e)
	return current
}
func (f *p2Chain) validateTopology(t *testing.T) *platform.PciTopologySnapshot {
	t.Helper()
	ctx := context.Background()
	scope, e := f.store.PreviewTopology(ctx, f.actor, f.project, f.asset)
	mustAgent(t, e)
	if !scope.Ready {
		t.Fatalf("positive topology scope: %s", scope.Code)
	}
	snapshot, e := f.store.ValidateTopology(ctx, f.actor, f.project, f.asset, platform.TopologyValidateInput{ScopeHash: scope.ScopeHash, Reason: "Independent model comparison"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	return snapshot
}
func TestP2PositiveG1WaiverConsumptionAndRevocationPostgres(t *testing.T) {
	f := p2PositiveG1Topology(t)
	ctx := context.Background()
	_, e := f.store.DB.ExecContext(ctx, `INSERT INTO param_keys(id,scope_key,key,value_type,default_value,created_by,created_at,updated_at)VALUES($1,'project:'||$2,'gate.g1.waiver_fields','string','["nic.dom"]',$3,now(),now())`, platform.RandomID(), f.project, f.actor.User.ID)
	mustAgent(t, e)
	for i := range f.runner.bundle.Probes {
		if f.runner.bundle.Probes[i].Kind == "nic_dom" {
			f.runner.bundle.Probes[i].State = "read_failed"
			f.runner.bundle.Probes[i].Stdout = ""
		}
	}
	current := f.collect(t)
	if current.EffectiveVerdict != "BLOCKED" {
		t.Fatalf("missing DOM original must block: %+v", current)
	}
	scope, e := f.store.GateWaiverScope(ctx, f.actor, f.project, f.asset)
	mustAgent(t, e)
	if !scope.CanApprove {
		t.Fatalf("allowed optional DOM waiver blocked: %+v", scope)
	}
	waiver, e := f.store.ApproveGateWaiver(ctx, f.actor, f.project, f.asset, platform.GateWaiverInput{SourceHash: scope.SourceHash, Reason: "Documented unsupported DOM", RemediationPlan: "Compare approved optics in hardware PoC", ValidUntil: time.Now().Add(time.Minute)}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	current, e = f.store.CurrentG1(ctx, f.actor, f.project, f.asset)
	mustAgent(t, e)
	if current.EffectiveVerdict != "WAIVED" || current.Evidence.Verdict != "BLOCKED" {
		t.Fatalf("waiver rewrote raw evidence: %+v", current)
	}
	topo := f.validateTopology(t)
	if topo.EffectiveVerdict != "PASS" || topo.Scope.G1WaiverID != waiver.ID || topo.ValidUntil.After(waiver.ValidUntil) {
		t.Fatalf("downstream waiver scope lost: %+v", topo)
	}
	mustAgent(t, f.store.RevokeGateWaiver(ctx, f.actor, f.project, f.asset, waiver.ID, "Original waiver withdrawn", "p2-chain"))
	current, e = f.store.CurrentG1(ctx, f.actor, f.project, f.asset)
	mustAgent(t, e)
	if current.EffectiveVerdict != "BLOCKED" {
		t.Fatal("revoked waiver still authorizes G1")
	}
	scopeAfter, e := f.store.PreviewTopology(ctx, f.actor, f.project, f.asset)
	mustAgent(t, e)
	if scopeAfter.Ready {
		t.Fatal("revoked waiver still authorizes topology")
	}
}
func TestP2PositiveT7FirmwareMockAndFreshRetestPostgres(t *testing.T) {
	f := p2PositiveG1Topology(t)
	ctx := context.Background()
	original := append([]pcitopology.Node{}, f.runner.bundle.PCINodes...)
	for i := range f.runner.bundle.PCINodes {
		if f.runner.bundle.PCINodes[i].BDF == "0000:03:00.0" {
			width := 8
			f.runner.bundle.PCINodes[i].Width = &width
		}
	}
	current := f.collect(t)
	if current.EffectiveVerdict != "PASS" {
		t.Fatalf("fully collected but mismatched topology must retain G1: %+v", current)
	}
	failed := f.validateTopology(t)
	if failed.EffectiveVerdict != "FAIL" {
		t.Fatalf("expected T7 mismatch: %+v", failed.Evaluation)
	}
	var issueID string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT issue_id FROM topology_issue_bindings WHERE asset_id=$1 ORDER BY issue_id LIMIT 1`, f.asset).Scan(&issueID))
	detail, e := f.store.GetIssue(ctx, f.actor, issueID)
	mustAgent(t, e)
	ne, name := ordinaryTestActor(t, f.store)
	mustAgent(t, f.store.SetMembership(ctx, name, f.project, "NE", false))
	// Membership changes revoke the old session: use only the current login.
	pre, _, e := f.store.NewPreSession(ctx)
	mustAgent(t, e)
	_, ne, e = f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: name, Password: "isolated-scoped-user-test-password", Method: "local"}, "198.51.100.187", "p2-chain")
	mustAgent(t, e)
	item, e := f.store.ActOnIssue(ctx, f.actor, issueID, "assign", platform.IssueActionInput{Version: detail.Issue.Version, Reason: "Diagnose observed model mismatch", AssigneeID: ne.User.ID}, "p2-chain")
	mustAgent(t, e)
	item, e = f.store.ActOnIssue(ctx, ne, issueID, "start", platform.IssueActionInput{Version: item.Version, Reason: "Begin approved firmware simulation"}, "p2-chain")
	mustAgent(t, e)
	item, e = f.store.ActOnIssue(ctx, ne, issueID, "repair", platform.IssueActionInput{Version: item.Version, RepairAction: "flash_bios", Reason: "Approved simulation only; separate collection required"}, "p2-chain")
	mustAgent(t, e)
	media, e := f.store.IngestMedia(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, platform.RegisterMediaInput{Type: "firmware", Name: "P2 simulated approved BIOS", Version: "v1", Arch: "amd64", OsScope: "linux", Deps: map[string]any{"compatibility": map[string]any{"source": "independent-protocol-fixture", "version": "1", "report_ref": "review://p2/firmware-compatibility", "requires": map[string]any{"device_model_version": []string{f.modelVersion}, "repair_action": []string{"flash_bios"}}}}}, bytes.NewReader([]byte("SIMULATION ONLY; no executable firmware")), "p2-chain")
	mustAgent(t, e)
	reviewerName := "p2-firmware-reviewer-" + platform.RandomID()[:10]
	_, e = f.store.CreateUser(ctx, reviewerName, reviewerName, "isolated-p2-firmware-reviewer-password", true, false)
	mustAgent(t, e)
	pre, _, e = f.store.NewPreSession(ctx)
	mustAgent(t, e)
	_, reviewer, e := f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: reviewerName, Password: "isolated-p2-firmware-reviewer-password", Method: "local"}, "198.51.100.186", "p2-chain")
	mustAgent(t, e)
	for _, state := range []string{"verified", "approved"} {
		media, e = f.store.TransitionMedia(ctx, reviewer, &platform.ProjectSummary{ID: f.project}, media.ID, state, "p2-chain")
		mustAgent(t, e)
	}
	f.store.ArtifactTransfersEnabled = true
	f.store.TopologyFirmwareMockEnabled = true
	f.service.IssueStore = f.store
	plan, e := f.store.CreateTopologyFirmwarePlan(ctx, ne, issueID, platform.TopologyFirmwareInput{IssueVersion: item.Version, MediaID: media.ID, Action: "flash_bios", Runbook: "poc://P2/simulated-BIOS-runbook", RecoveryPlan: "Keep original BIOS and recover using approved operator runbook", Reason: "Simulate approved repair without hardware writes", RebootApproved: true}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	approve := platform.TopologyFirmwareApproval{ScopeHash: plan.ScopeHash, Reason: "Independent review of immutable simulated repair"}
	if _, e = f.store.ApproveTopologyFirmwarePlan(ctx, ne, issueID, plan.ID, approve, "p2-chain"); e == nil {
		t.Fatal("author self-approved firmware plan")
	}
	plan, e = f.store.ApproveTopologyFirmwarePlan(ctx, f.actor, issueID, plan.ID, approve, "p2-chain")
	mustAgent(t, e)
	x, e := f.service.CreateTopologyFirmwareExecution(ctx, ne, f.project, issueID, plan.ID, approve, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	claim := f.claim(t, x.ID)
	if claim == nil {
		t.Fatal("firmware simulation unclaimable")
	}
	mustAgent(t, f.service.RunClaim(ctx, claim))
	done, e := f.service.Get(ctx, f.project, x.ID)
	mustAgent(t, e)
	if done.Execution.State != "success" {
		t.Fatalf("simulation did not settle: %s", done.Execution.State)
	}
	after, e := f.store.GetIssue(ctx, f.actor, issueID)
	mustAgent(t, e)
	if after.Issue.State != "in_repair" || after.Issue.Version != item.Version {
		t.Fatal("simulation changed repair evidence/closed issue")
	}
	if _, e = f.store.ActOnIssue(ctx, ne, issueID, "close", platform.IssueActionInput{Version: item.Version, Reason: "Simulation succeeded"}, "p2-chain"); e == nil {
		t.Fatal("simulation substituted for revalidation")
	}
	f.runner.bundle.PCINodes = original
	f.collect(t)
	passed := f.validateTopology(t)
	if passed.EffectiveVerdict != "PASS" {
		t.Fatalf("fresh repaired topology: %+v", passed.Evaluation)
	}
	retest, e := f.store.CreateTopologyRetest(ctx, ne, issueID, platform.TopologyRetestInput{Version: item.Version, SnapshotID: passed.ID, Reason: "New original collection after approved repair"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	if retest.Verdict != "PASS" || !retest.CanClose {
		t.Fatalf("fresh original retest did not authorize confirmation: %+v", retest)
	}
	closed, e := f.store.ActOnIssue(ctx, ne, issueID, "close", platform.IssueActionInput{Version: retest.AppliedIssueVersion, Reason: "Confirm independently revalidated topology"}, "p2-chain")
	mustAgent(t, e)
	if closed.State != "closed" {
		t.Fatal("explicit successful close failed")
	}
}

func p2Role(t *testing.T, f *p2Chain, role string) *platform.AuthSession {
	t.Helper()
	ctx := context.Background()
	name := "p2-" + strings.ToLower(role) + "-" + platform.RandomID()[:10]
	password := "isolated-p2-role-password"
	_, e := f.store.CreateUser(ctx, name, name, password, false, false)
	mustAgent(t, e)
	mustAgent(t, f.store.SetMembership(ctx, name, f.project, role, false))
	pre, _, e := f.store.NewPreSession(ctx)
	mustAgent(t, e)
	_, actor, e := f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: name, Password: password, Method: "local"}, "198.51.100.188", "p2-chain")
	mustAgent(t, e)
	return actor
}
func (f *p2Chain) networkReady(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	project := &platform.ProjectSummary{ID: f.project}
	path := os.Getenv("KEEL_P2_BUSINESS_PROBES_FILE")
	if path == "" {
		t.Skip("private P2 protocol transport absent")
	}
	var e error
	f.store.FabricProbes, e = fabricboot.LoadProbeSuite(path)
	mustAgent(t, e)
	var serial, host, mac, hostname string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT serial_number FROM switches WHERE id=$1`, f.switchID).Scan(&serial))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT serial_number FROM machines WHERE id=$1`, f.machine).Scan(&host))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT mac_address FROM physical_ports WHERE id=$1`, f.port).Scan(&mac))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT hostname FROM project_assets WHERE id=$1`, f.asset).Scan(&hostname))
	raw, _ := json.Marshal(map[string]any{"project_id": f.project, "switch_id": f.switchID, "switch_serial": serial, "host_serial": host, "host_mac": mac, "host_port_id": f.port, "hostname": hostname})
	mustAgent(t, os.WriteFile(os.Getenv("KEEL_P2_BUSINESS_SCOPE_FILE"), raw, 0600))
	_, e = f.store.DB.ExecContext(ctx, `UPDATE bom_items SET position='{"dc":"DC1","rack":"R1","idx":"1"}',pod='P1',role='leaf' WHERE item_uid=$1`, f.switchID)
	mustAgent(t, e)
	for i, kind := range []string{"mgmt", "base"} {
		m := "02:" + f.switchID[:2] + ":" + f.switchID[2:4] + ":" + f.switchID[4:6] + ":" + f.switchID[6:8] + ":" + map[int]string{0: "10", 1: "11"}[i]
		_, e = f.store.DB.ExecContext(ctx, `INSERT INTO switch_mac_bindings(switch_id,kind,mac,package_id,updated_at)VALUES($1,$2,$3,$4,now())`, f.switchID, kind, m, f.pkg)
		mustAgent(t, e)
	}
	baseline := "p2-baseline-" + platform.RandomID()[:8]
	_, e = f.store.CreateNetworkBaseline(ctx, f.actor, project, platform.CreateNetworkBaselineInput{VersionRef: baseline, SourceRef: "software-protocol-fixture-independent-approved-input", EngineeringLimits: platform.NetworkEngineeringLimits{SwitchBandwidthRatioLimits: map[string]float64{"role:leaf": 2}, Management: &fabricboot.ManagementRules{CIDR: "192.0.2.0/24", FirstAddress: "192.0.2.10", Gateway: "192.0.2.1", BatchPrefix: "p2", BatchBy: "rack"}, ConfigurationVariables: map[string]map[string]any{serial: {"asn": 65000}}}}, "p2-chain")
	mustAgent(t, e)
	_, e = f.store.ApproveNetworkBaseline(ctx, f.actor, project, baseline, platform.ApproveNetworkBaselineInput{CustomerApprovalRef: "fixture:independent-customer", NetworkApprovalRef: "fixture:independent-network"}, "p2-chain")
	mustAgent(t, e)
	preview, e := f.store.PreviewZTPManagementPlan(ctx, f.actor, project, platform.ZTPManagementPlanInput{BaselineRef: baseline})
	mustAgent(t, e)
	plan, e := f.store.CommitZTPManagementPlan(ctx, f.actor, project, platform.ZTPManagementPlanInput{BaselineRef: baseline, ExpectedHash: preview.ContentHash}, "p2-chain")
	mustAgent(t, e)
	if len(plan.Assignments) != 1 {
		t.Fatal("expected one explicit switch")
	}
	batch := plan.Assignments[0].Batch
	manifest, e := f.store.PreviewZTPManagementArtifacts(ctx, f.actor, project, batch)
	mustAgent(t, e)
	media, e := f.store.IngestMedia(ctx, f.actor, project, platform.RegisterMediaInput{Type: "ztp_script", Name: "P2 personalized management", Version: "v1", Arch: "noarch", OsScope: "linux", ManifestRef: "generated:ztp-management-manifest/v1"}, strings.NewReader(manifest.Content), "p2-chain")
	mustAgent(t, e)
	reviewer := p2Role(t, f, "Admin")
	for _, state := range []string{"verified", "approved"} {
		media, e = f.store.TransitionMedia(ctx, reviewer, project, media.ID, state, "p2-chain")
		mustAgent(t, e)
	}
	for _, channel := range []string{"239", "143"} {
		environment, e := f.store.RunZTPEnvironmentCheck(ctx, f.actor, project, platform.ZTPEnvironmentInput{BatchRef: batch, BootChannel: channel}, "p2-chain")
		mustAgent(t, e)
		if !environment.Ready {
			t.Fatalf("environment: %+v", environment)
		}
	}
	deployment, e := f.store.StageKeaDeployment(ctx, f.actor, project, platform.KeaDeploymentInput{BatchRef: batch, PolicyMediaID: media.ID}, "p2-chain")
	mustAgent(t, e)
	f.service.KeaStore = f.store
	run, e := f.service.CreateKea(ctx, f.actor, f.project, KeaCreateInput{DeploymentID: deployment.ID}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	mustAgent(t, f.service.RunClaim(ctx, f.claim(t, run.ID)))
	deployments, e := f.store.ListKeaDeployments(ctx, f.actor, project)
	mustAgent(t, e)
	if len(deployments) != 1 || deployments[0].State != "boot_ready" {
		t.Fatalf("original DORA/readback failed: %+v", deployments)
	}
	boot, e := url.Parse(deployment.Spec.Options.Option239URL)
	mustAgent(t, e)
	var mgmt, baseMAC string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT mac FROM switch_mac_bindings WHERE switch_id=$1 AND kind='mgmt'`, f.switchID).Scan(&mgmt))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT mac FROM switch_mac_bindings WHERE switch_id=$1 AND kind='base'`, f.switchID).Scan(&baseMAC))
	_, e = f.store.ServeZTP239(ctx, f.project, batch, "N-S4", media.ID, plan.Assignments[0].Address, "https", boot.Host, boot.Path, boot.RawQuery, "p2-chain", platform.ZTP239Claim{Serial: serial, MgmtMAC: mgmt, BaseMAC: baseMAC, ProductName: "software-fixture", Version: "5.9", Build: "test-only"})
	mustAgent(t, e)
	count, e := f.store.ReconcileZTPDeviceSessions(ctx, 10)
	mustAgent(t, e)
	if count != 1 {
		t.Fatalf("authenticated software management did not settle: %d", count)
	}
	template, e := f.store.CreateNetworkTemplate(ctx, f.actor, project, platform.NetworkTemplateInput{Name: "P2 independent Golden", SourceRef: "fixture:customer-approved", Definition: netbaseline.Template{Variables: map[string]netbaseline.Variable{"asn": {Type: "integer", Required: true}}, Source: `{"hostname":{{device.hostname|keeljson}},"management":{"address":{{device.management_ip|keeljson}},"gateway":{{device.gateway|keeljson}}},"asn":{{asn|keeljson}},"description":"approved"}`}}, "p2-chain")
	mustAgent(t, e)
	input := platform.NetworkPlanInput{TemplateID: template.ID, BaselineRef: baseline, BatchRef: batch}
	_, e = f.store.TestNetworkTemplate(ctx, f.actor, project, input, "p2-chain")
	mustAgent(t, e)
	_, e = f.store.ReleaseNetworkTemplate(ctx, reviewer, project, template.ID, "p2-chain")
	mustAgent(t, e)
	job, e := f.store.CreateNetworkPush(ctx, f.actor, project, input, "p2-chain")
	mustAgent(t, e)
	f.service.NetworkStore = f.store
	run, e = f.service.CreateNetwork(ctx, f.actor, f.project, NetworkExecutionInput{JobID: job.ID, Phase: "prepare"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	mustAgent(t, f.service.RunClaim(ctx, f.claim(t, run.ID)))
	jobs, e := f.store.ListNetworkPush(ctx, f.actor, project)
	mustAgent(t, e)
	job = &jobs[0]
	if job.State != "READY" {
		t.Fatalf("network Doublecheck not ready: %+v", job)
	}
	for _, role := range []string{"NE", "DE", "NH"} {
		actor := p2Role(t, f, role)
		job, e = f.store.DecideNetworkPush(ctx, actor, project, job.ID, job.ReviewHash, "Independent frozen diff review", "approve", "p2-chain")
		mustAgent(t, e)
	}
	run, e = f.service.CreateNetwork(ctx, f.actor, f.project, NetworkExecutionInput{JobID: job.ID, Phase: "push"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	mustAgent(t, f.service.RunClaim(ctx, f.claim(t, run.ID)))
	done, e := f.service.Get(ctx, f.project, run.ID)
	mustAgent(t, e)
	if done.Execution.State != "success" {
		t.Fatalf("Golden original stopped receipt: %s", done.Execution.State)
	}
	_, e = f.store.CollectFabric(ctx, f.actor, project, "p2-chain")
	mustAgent(t, e)
	f.store.WiringEnabled = true
}
func TestP2PositiveNetworkReadinessAndWiringPostgres(t *testing.T) {
	if os.Getenv("KEEL_P2_BUSINESS_PROBES_FILE") == "" {
		t.Skip("private P2 business fixture absent")
	}
	f := p2PositiveG1Topology(t)
	f.networkReady(t)
	facts, e := f.store.PreviewWiring(context.Background(), f.actor, f.project, []platform.WiringTargetInput{{AssetID: f.asset, PhysicalPortIDs: []string{f.port}}})
	mustAgent(t, e)
	if !facts.Ready {
		t.Fatalf("original G1/topology/Golden/management chain blocked: %+v", facts.Blockers)
	}
	f.publishG2G3(t)
	f.portMonitorRepair(t)
}

// This private transport double exercises the real-origin branch in an isolated
// DB. It owns independent observed facts and never wraps/relabels a sim driver.
type p2WireDriver struct {
	state                       wiring.LocalState
	serial, hostname, switchMAC string
	plan                        wiring.Plan
	started                     time.Time
	stopped                     bool
}

func (*p2WireDriver) Origin() string { return "real" }
func (d *p2WireDriver) Inspect(context.Context, wiring.Expectation) ([]wiring.LocalState, error) {
	return []wiring.LocalState{d.state}, nil
}
func (d *p2WireDriver) Begin(_ context.Context, plan wiring.Plan, _ []wiring.LocalState) (wiring.Session, error) {
	d.plan = plan
	d.started = time.Now().UTC()
	return d, nil
}
func (d *p2WireDriver) Stop(context.Context) (wiring.StopProof, error) {
	d.stopped = true
	return wiring.StopProof{PlanDigest: d.plan.Digest, Stopped: true, Restored: true}, nil
}
func (d *p2WireDriver) Observe(context.Context) (*wiring.Observation, error) {
	document := func(chassis, port, name, description string) string {
		raw, _ := json.Marshal(map[string]any{"lldp": []any{map[string]any{"interface": []any{map[string]any{"name": "eth0", "via": "LLDP", "chassis": []any{map[string]any{"id": []any{map[string]string{"type": "local", "value": chassis}}, "name": []any{map[string]string{"value": name}}, "descr": []any{map[string]string{"value": description}}}}, "port": []any{map[string]any{"id": []any{map[string]string{"type": "local", "value": port}}}}}}}}})
		return string(raw)
	}
	probes := []wiring.RawProbe{{PhysicalPortID: d.state.PhysicalPortID, Kind: "local", State: "success", Stdout: document(d.state.PermanentMAC, d.state.PhysicalPortID, d.hostname, wiring.Description(d.serial, "rail-1", "1"))}, {PhysicalPortID: d.state.PhysicalPortID, Kind: "neighbors", State: "success", Stdout: document(d.switchMAC, "swp1", "p2-switch", "software transport fixture")}}
	return &wiring.Observation{PlanDigest: d.plan.Digest, AdvertisingAt: d.started, CollectedAt: time.Now().UTC(), WindowComplete: true, States: []wiring.LocalState{d.state}, Probes: probes}, nil
}

type p2WireAdapters struct {
	driver    *p2WireDriver
	collector *fabricboot.ProbeSuite
}

func (*p2WireAdapters) HostOrigin() string   { return "real" }
func (*p2WireAdapters) SwitchOrigin() string { return "device" }
func (a *p2WireAdapters) Open(*platform.WiringCommand) (wiring.Driver, wiring.RecollectionClient, error) {
	return a.driver, a.collector, nil
}
func (f *p2Chain) publishG2G3(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var mac, serial, hostname, switchMAC string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT mac_address FROM physical_ports WHERE id=$1`, f.port).Scan(&mac))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT serial_number FROM machines WHERE id=$1`, f.machine).Scan(&serial))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT hostname FROM project_assets WHERE id=$1`, f.asset).Scan(&hostname))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT mac FROM switch_mac_bindings WHERE switch_id=$1 AND kind='base'`, f.switchID).Scan(&switchMAC))
	up := true
	driver := &p2WireDriver{state: wiring.LocalState{PhysicalPortID: f.port, Interface: "eth0", BDF: "0000:05:00.0", PermanentMAC: mac, CurrentMAC: mac, AdminUp: true, MTU: 1500, Addresses: []string{}, LinkUp: &up}, serial: serial, hostname: hostname, switchMAC: switchMAC}
	f.service.WiringStore = f.store
	f.service.WiringAdapters = &p2WireAdapters{driver: driver, collector: f.store.FabricProbes}
	input := platform.WiringHandshakeInput{Targets: []platform.WiringTargetInput{{AssetID: f.asset, PhysicalPortIDs: []string{f.port}}}, Reason: "Fresh original G1/topology/NP1/2/4 handshake"}
	handshake, e := f.store.CreateWiringHandshake(ctx, f.actor, f.project, input, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	batch, e := f.store.CreateWiringBatch(ctx, f.actor, f.project, platform.WiringBatchInput{HandshakeID: handshake.ID, FactsHash: handshake.Facts.FactsHash, Reason: "Freeze independent Step1 GPU/NIC relations"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	handshake, e = f.store.CreateWiringHandshake(ctx, f.actor, f.project, input, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	x, e := f.service.CreateWiringExecution(ctx, f.actor, f.project, platform.WiringRunInput{BatchID: batch.ID, HandshakeID: handshake.ID, FactsHash: handshake.Facts.FactsHash, Reason: "Original host/switch double-ended capture"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	mustAgent(t, f.service.RunClaim(ctx, f.claim(t, x.ID)))
	batch, e = f.store.GetWiringBatch(ctx, f.actor, f.project, batch.ID)
	mustAgent(t, e)
	var step3 string
	for _, report := range batch.Targets[0].Reports {
		if report.Step == 3 {
			if report.EffectiveVerdict != "PASS" {
				t.Fatalf("original double-ended wire checks: %+v", report)
			}
			step3 = report.ID
		}
	}
	if step3 == "" {
		t.Fatal("original Step3 not projected")
	}
	confirmation, e := f.store.ConfirmWiring(ctx, f.actor, f.project, batch.ID, platform.WiringConfirmationInput{AssetID: f.asset, Step3ID: step3, Reason: "Confirm original host-switch-BOM three-way comparison"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	if confirmation.EffectiveVerdict != "PASS" {
		t.Fatalf("G2 confirmation: %+v", confirmation)
	}
	candidate, e := f.store.PreviewMapping(ctx, f.actor, f.project, f.asset)
	mustAgent(t, e)
	if !candidate.Ready || candidate.Verdict != "PASS" {
		t.Fatalf("G3 complete denominator: %+v", candidate.Conflicts)
	}
	published, e := f.store.PublishMapping(ctx, f.actor, f.project, f.asset, platform.MappingPublishInput{ScopeHash: candidate.ScopeHash, Reason: "Publish one physical port and both original GPU relations"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	if published.Revision == nil || published.Revision.EffectiveVerdict != "PASS" || published.Candidate.Coverage.MappedGPUs != 2 {
		t.Fatalf("G3 publication: %+v", published)
	}
	t.Log("PASS original software G1 -> topology -> authenticated ZTP/DORA -> reviewed Golden -> Step1/2/3/4 G2 -> complete G3; no hardware PoC")
}

func (f *p2Chain) portMonitorRepair(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	f.store.MonitorEnabled = true
	f.store.FabricIssueVerificationEnabled = true
	f.service.IssueStore = f.store
	var mac string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT mac_address FROM physical_ports WHERE id=$1`, f.port).Scan(&mac))
	runner := &monitorSSHFixtureRunner{binding: f.runner.binding, samples: []monitor.Sample{{Key: "link.state", Entity: "eth0", Quality: "good", State: "down", Interface: "eth0", PermanentMAC: mac, BDF: "0000:05:00.0"}}}
	f.service.SSHRunner = runner
	x, e := f.service.CreateMonitorSSHSnapshot(ctx, f.actor, f.project, MonitorSSHInput{AssetID: f.asset, NetworkOnly: true, Reason: "Retain original post-G3 physical port observation"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	mustAgent(t, f.service.RunClaim(ctx, f.claim(t, x.ID)))
	count, e := f.store.TickMonitorSSHSources(ctx, 200)
	mustAgent(t, e)
	if count < 1 {
		t.Fatal("original monitor observation was not consumed")
	}
	var issueID string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT issue_id FROM portmon_issue_bindings WHERE asset_id=$1 AND rule_id='link-down'`, f.asset).Scan(&issueID))
	detail, e := f.store.GetIssue(ctx, f.actor, issueID)
	mustAgent(t, e)
	if detail.Issue.Source != "portmon" || detail.Issue.Category != "T1" {
		t.Fatal("monitor discovery relabeled")
	}
	ne := p2Role(t, f, "NE")
	item, e := f.store.ActOnIssue(ctx, f.actor, issueID, "assign", platform.IssueActionInput{Version: detail.Issue.Version, Reason: "Assign observed physical port outage", AssigneeID: ne.User.ID}, "p2-chain")
	mustAgent(t, e)
	item, e = f.store.ActOnIssue(ctx, ne, issueID, "start", platform.IssueActionInput{Version: item.Version, Reason: "Start physical repair"}, "p2-chain")
	mustAgent(t, e)
	item, e = f.store.ActOnIssue(ctx, ne, issueID, "repair", platform.IssueActionInput{Version: item.Version, Reason: "Reseated the original connection", RepairAction: "reseat"}, "p2-chain")
	mustAgent(t, e)
	verify := func(want string) {
		t.Helper()
		scope, e := f.store.IssueVerificationScope(ctx, ne, issueID)
		mustAgent(t, e)
		if !scope.ExecutionAvailable || scope.PortMonitor == nil {
			t.Fatalf("current original monitor repair scope: %+v", scope)
		}
		x, e := f.service.CreateIssueVerification(ctx, ne, f.project, issueID, platform.IssueVerificationInput{Version: item.Version, ScopeHash: scope.ScopeHash, Reason: "Original scoped post-repair physical readback"}, platform.RandomID(), "p2-chain")
		mustAgent(t, e)
		mustAgent(t, f.service.RunClaim(ctx, f.claim(t, x.ID)))
		done, e := f.service.Get(ctx, f.project, x.ID)
		mustAgent(t, e)
		if want == "PASS" && done.Execution.State != "success" || want == "FAIL" && done.Execution.State != "failed" {
			t.Fatalf("port verification result: %+v", done.Execution)
		}
		updated, e := f.store.GetIssue(ctx, ne, issueID)
		mustAgent(t, e)
		item = &updated.Issue
	}
	verify("PASS")
	if item.State != "verify" {
		t.Fatal("revalidation bypassed manual confirmation")
	}
	closed, e := f.store.ActOnIssue(ctx, ne, issueID, "close", platform.IssueActionInput{Version: item.Version, Reason: "Confirm original scoped link restoration"}, "p2-chain")
	mustAgent(t, e)
	if closed.State != "closed" || closed.Source != "portmon" {
		t.Fatal("shared port repair did not close original monitor issue")
	}

	// A subsequent physical failure creates another original incident. Two failed
	// repairs use separate commands and escalate once to the current DL.
	dl := p2Role(t, f, "DL")
	raw, e := os.ReadFile(os.Getenv("KEEL_P2_BUSINESS_SCOPE_FILE"))
	mustAgent(t, e)
	var boundary map[string]any
	mustAgent(t, json.Unmarshal(raw, &boundary))
	boundary["diagnostic_link"] = "down"
	raw, _ = json.Marshal(boundary)
	mustAgent(t, os.WriteFile(os.Getenv("KEEL_P2_BUSINESS_SCOPE_FILE"), raw, 0600))
	x, e = f.service.CreateMonitorSSHSnapshot(ctx, f.actor, f.project, MonitorSSHInput{AssetID: f.asset, NetworkOnly: true, Reason: "Retain a subsequent independent physical outage"}, platform.RandomID(), "p2-chain")
	mustAgent(t, e)
	mustAgent(t, f.service.RunClaim(ctx, f.claim(t, x.ID)))
	_, e = f.store.TickMonitorSSHSources(ctx, 200)
	mustAgent(t, e)
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT issue_id FROM portmon_issue_bindings WHERE asset_id=$1 AND issue_id<>$2 ORDER BY created_at DESC LIMIT 1`, f.asset, closed.ID).Scan(&issueID))
	detail, e = f.store.GetIssue(ctx, f.actor, issueID)
	mustAgent(t, e)
	item, e = f.store.ActOnIssue(ctx, f.actor, issueID, "assign", platform.IssueActionInput{Version: detail.Issue.Version, Reason: "Assign new retained outage", AssigneeID: ne.User.ID}, "p2-chain")
	mustAgent(t, e)
	item, e = f.store.ActOnIssue(ctx, ne, issueID, "start", platform.IssueActionInput{Version: item.Version, Reason: "Start another physical repair"}, "p2-chain")
	mustAgent(t, e)
	for i := 0; i < 2; i++ {
		if item.State == "reopened" {
			item, e = f.store.ActOnIssue(ctx, f.actor, issueID, "assign", platform.IssueActionInput{Version: item.Version, Reason: "Assign the reopened physical failure", AssigneeID: ne.User.ID}, "p2-chain")
			mustAgent(t, e)
			item, e = f.store.ActOnIssue(ctx, ne, issueID, "start", platform.IssueActionInput{Version: item.Version, Reason: "Begin the next repair after failed revalidation"}, "p2-chain")
			mustAgent(t, e)
		}
		item, e = f.store.ActOnIssue(ctx, ne, issueID, "repair", platform.IssueActionInput{Version: item.Version, Reason: "Recorded distinct repair attempt", RepairAction: "reseat"}, "p2-chain")
		mustAgent(t, e)
		verify("FAIL")
		var escalations int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM issue_escalations WHERE issue_id=$1`, issueID).Scan(&escalations))
		if escalations != i {
			t.Fatalf("failure %d produced %d escalations", i+1, escalations)
		}
	}
	history, e := f.store.IssueVerifications(ctx, ne, issueID, platform.Pagination{Page: 1, PageSize: 10})
	mustAgent(t, e)
	if len(history.Items) != 2 || !history.Items[0].Escalated || !slices.Contains(history.Items[0].RecipientIDs, dl.User.ID) {
		t.Fatalf("DL escalation lineage: %+v", history.Items)
	}
	if _, e = f.store.ActOnIssue(ctx, ne, issueID, "close", platform.IssueActionInput{Version: item.Version, Reason: "Must not close failed physical repair"}, "p2-chain"); e == nil {
		t.Fatal("failed port repairs allowed closure")
	}
	t.Log("PASS original G3 -> SSH sample -> physical monitor incident -> immutable binding -> assigned repair -> shared issue.verify -> explicit close")
}
