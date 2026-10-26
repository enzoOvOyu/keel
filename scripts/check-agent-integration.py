#!/usr/bin/env python3
"""Real PostgreSQL + verified TLS/gRPC identity, never a hardware acceptance."""
import json
import argparse
import os
from pathlib import Path
import subprocess
import tempfile
import urllib.parse
import uuid
from p1_evidence import go_test_evidence, repository_source, source_provenance

os.umask(0o077)
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--browser', action='store_true', help='Also exercise built web UI with the actual Agent transport')
parser.add_argument('--run', default='TestAgent|TestProtected|TestWriteNew|TestAuthority|TestIssued|TestCSR|TestControlKV|TestHostFlow|TestHostMedia|TestArtifact|TestFlowYAML|TestWorkflowYAML|TestExecutionStream|TestExecutionQueue|TestProvision', help='Go test name filter for a targeted rerun')
parser.add_argument('--redis-address', help='Opt-in dedicated Redis fixture, literal loopback:16379; never the application Redis')
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
os.chdir(root)
source_before = repository_source(root)
work = Path(tempfile.mkdtemp(prefix='keel-agent-integration-'))
binary_root = Path.home() / '.local/state/keel/evidence/agent-binaries'
binary_root.mkdir(parents=True, exist_ok=True, mode=0o700)
binary_dir = Path(tempfile.mkdtemp(prefix='agent-', dir=binary_root))
container = 'keel-auth-test-postgres'
database = 'keel_test_agent_' + uuid.uuid4().hex[:12]
created = False
try:
    record = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
    assert record['Config'].get('Labels', {}).get('io.keel.test-environment') == 'disposable'
    ports = record['NetworkSettings']['Ports'].get('5432/tcp') or []
    assert any(item.get('HostIp') == '127.0.0.1' and item.get('HostPort') == '15433' for item in ports)
    config = dict(value.split('=', 1) for value in record['Config']['Env'] if '=' in value)
    owner = config['POSTGRES_USER']
    subprocess.run(['docker', 'exec', container, 'createdb', '-U', owner, database], check=True, capture_output=True)
    created = True
    env = {key: value for key, value in os.environ.items() if not key.startswith('KEEL_')}
    env['KEEL_TEST_AGENT_DATABASE_URL'] = 'postgresql://' + urllib.parse.quote(owner, safe='') + ':' + urllib.parse.quote(config['POSTGRES_PASSWORD'], safe='') + '@127.0.0.1:15433/' + database + '?sslmode=disable'
    if args.redis_address:
        env['KEEL_TEST_REDIS_ADDRESS'] = args.redis_address
    if args.browser:
        env['KEEL_TEST_AGENT_STATIC_DIR'] = str(root / 'web/dist')
        env['KEEL_TEST_AGENT_BROWSER_SCRIPT'] = str(root / 'scripts/check-agent-launch-browser.mjs')
        env['KEEL_TEST_AGENT_BROWSER_EVIDENCE_DIR'] = str(work)
        if os.environ.get('KEEL_PLAYWRIGHT_MODULE'):
            env['KEEL_PLAYWRIGHT_MODULE'] = os.environ['KEEL_PLAYWRIGHT_MODULE']
    with (work / 'go-integration.log').open('x') as log:
        for name in ('admin', 'agent', 'worker'):
            binary = binary_dir / ('keel-' + name)
            subprocess.run(['go', 'build', '-o', str(binary), './cmd/keel-' + name], env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
            env['KEEL_TEST_AGENT_' + name.upper() + '_BIN'] = str(binary)
        result = subprocess.run(['go', 'test', '-p=1', '-race', '-count=1', '-v', './internal/agentpki', './internal/agentinventory', './internal/agentidentity', './internal/agentclient', './internal/platform', './internal/execution', './internal/executionqueue', './internal/taskrunner', './internal/httpapi', '-run', args.run], env=env, stdout=log, stderr=subprocess.STDOUT)
    transcript = (work / 'go-integration.log').read_text()
    evidence = go_test_evidence(transcript, result.returncode, {
        'loopback_tls_grpc': ['TestAgentIdentityPostgresMTLSLifecycle', 'TestAgentTasksPostgresExecutionAndRecovery'],
        'worker_cli': ['TestAgentWorkerAndCLIExecutionLifecycle'],
        'browser': ['TestAgentReadOnlyLaunchBrowser'],
        'native_host_dag': ['TestHostFlowDAGAgentAdmissionAndReceiptsPostgres', 'TestHostFlowUnknownBlocksDependencyUntilExplicitReconcilePostgres', 'TestHostFlowFailureSkipsDependentAndPauseStopsDispatchPostgres'],
        'native_host_rerun': ['TestHostFlowRerunCreatesIndependentAgentDAGPostgres', 'TestHostFlowRerunRefusesRetiredFrozenArtifactPostgres', 'TestHostFlowRerunSnapshotRejectsOwnedWorkflows'],
        'control_kv': ['TestControlKVAtomicReceiptCASAndProjectIsolationPostgres', 'TestControlKVExpiredClaimHasNonCommitProofPostgres', 'TestControlKVPauseKillAndCurrentAuthorityPostgres'],
        'streams_postgresql': ['TestExecutionStreamsCommitBeforeACKAndDuplicatePostgres', 'TestExecutionStreamsRejectAndBusyRecoveryPostgres', 'TestExecutionStreamsRebuildAndFailureIsolationPostgres', 'TestExecutionStreamsHeartbeatDistinguishesQueueModePostgres'],
        'redis_streams_protocol': ['TestExecutionQueueRedisStreamsRecovery'],
        'redis_postgresql_scheduler': ['TestExecutionStreamsRedisPostgresScheduler'],
        'provision_admission': ['TestProvisionExecutionKernelPostgres', 'TestProvisionInstallationKernelPostgres'],
        'provision_boot_write': ['TestProvisionInstallationLiveBootPostgres', 'TestProvisionInstallationFileDeliveryPostgres', 'TestProvisionInstallationWritePermitPostgres', 'TestProvisionInstallationFirstBootPostgres'],
        'provision_ssh_configuration': ['TestProvisionInstallationFirstSSHPostgres', 'TestProvisionInstallationBaselinePostgres', 'TestProvisionInstallationConfigurationPostgres'],
        'provision_unknown_recovery': ['TestProvisionInstallationPrewriteRecoveryPostgres', 'TestProvisionInstallationRegisteredRecoveryPostgres', 'TestProvisionInstallationFirstSSHOrphanDispatchPostgres'],
        'provision_repair_rollout': ['TestProvisionInstallationKnownFailedSSHPostgres', 'TestProvisionInstallationSSHBatchPostgres', 'TestProvisionInstallationKeyRepairPostgres', 'TestProvisionInstallationRepairListPostgres', 'TestProvisionInstallationRolloutPostgres'],
        'provision_loopback_ssh': ['TestProvisionFirstSSHUsesNewAuthenticatedPinAndVaultKey', 'TestProvisionFirstSSHRejectsWrongHostPeerAndLostReceipt', 'TestProvisionFirstSSHFailedQueryAllowsSeparateReadOnlyProbe', 'TestProvisionKeyRepairFreshDomainHandshake', 'TestProvisionKeyRepairRejectsImplicitBootstrap'],
    })
    (work / 'result.json').write_text(json.dumps({
        'exit_code': result.returncode, 'test_filter': args.run,
        'real_postgresql_configured': True,
        'verified_loopback_tls_grpc': evidence['verified_groups']['loopback_tls_grpc'],
        'browser_requested': args.browser,
        'browser_verified': args.browser and evidence['verified_groups']['browser'],
        'physical_device_acceptance': False,
        'provision_transport': 'mock object/machine proofs; no actual PXE or physical installation',
        'source': source_provenance(source_before, repository_source(root)),
        **evidence,
    }))
    print('Agent evidence:', work, 'exit:', result.returncode)
    raise SystemExit(result.returncode)
finally:
    if created:
        subprocess.run(['docker', 'exec', container, 'dropdb', '-U', owner, database], check=True, capture_output=True)
