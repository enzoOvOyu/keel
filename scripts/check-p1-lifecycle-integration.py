#!/usr/bin/env python3
"""One isolated P1 lifecycle batch; no browser, image build or production writes.

Run at the agreed validation milestone. Builds one owner CLI, then exercises
OS composition/config previews, master rewrap, independent audit evidence and
signed outbound execution Webhooks against local HTTP receivers.
Agent/SSH transport and complete P1 acceptance retain their existing runners.
"""
import argparse
import json
import os
import re
from pathlib import Path
import subprocess
import tempfile
import urllib.parse
import uuid
from p1_evidence import go_test_evidence, repository_source, source_provenance, ztp_logical_batch_evidence

os.umask(0o077)
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--run', default='TestOSBootMediaComposition|TestProvisionPreviewUsesCurrentBOMAndApprovedOS|TestProvisionRequest|TestCredentialMasterAtomicRewrapAndRecovery|TestAuditEvidence|TestCloudInit|TestMasterRewrap|TestSignedOSBootRoles|TestSSHCredentialCLI|TestWebhook|TestZTP|TestExecutionCircuitBreaker|TestDHCP6|TestMerge136|TestMock136|TestKeaDHCP6', help='Narrow only after a diagnosed failure; default covers this lifecycle batch')
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
os.chdir(root)
source_before = repository_source(root)
work = Path(tempfile.mkdtemp(prefix='keel-p1-lifecycle-'))
binary_root = Path.home() / '.local/state/keel/evidence/p1-lifecycle-binaries'
binary_root.mkdir(parents=True, exist_ok=True, mode=0o700)
binary_dir = Path(tempfile.mkdtemp(prefix='p1-', dir=binary_root))
container = 'keel-auth-test-postgres'
database = 'keel_test_p1_lifecycle_' + uuid.uuid4().hex[:12]
created = False
print('P1 lifecycle evidence:', work, flush=True)
try:
    record = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
    assert record['Config'].get('Labels', {}).get('io.keel.test-environment') == 'disposable'
    # Refuse a non-loopback test service before obtaining connection material.
    ports = record['NetworkSettings']['Ports'].get('5432/tcp') or []
    assert any(item.get('HostIp') == '127.0.0.1' and item.get('HostPort') == '15433' for item in ports)
    config = dict(value.split('=', 1) for value in record['Config']['Env'] if '=' in value)
    owner = config['POSTGRES_USER']
    subprocess.run(['docker', 'exec', container, 'createdb', '-U', owner, database], check=True, capture_output=True)
    created = True
    env = {key: value for key, value in os.environ.items() if not key.startswith('KEEL_')}
    env['KEEL_TEST_DATABASE_URL'] = 'postgresql://' + urllib.parse.quote(owner, safe='') + ':' + urllib.parse.quote(config['POSTGRES_PASSWORD'], safe='') + '@127.0.0.1:15433/' + database + '?sslmode=disable'
    env['KEEL_TEST_P1_ADMIN_BIN'] = str(binary_dir / 'keel-admin')
    with (work / 'integration.log').open('x') as log:
        subprocess.run(['go', 'build', '-o', env['KEEL_TEST_P1_ADMIN_BIN'], './cmd/keel-admin'], env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
        result = subprocess.run(['go', 'test', '-p=1', '-race', '-count=1', '-v', './internal/auditevidence', './internal/webhook', './internal/mediasource', './internal/credentialvault', './internal/mediamanifest', './internal/provisionseed', './internal/fabricboot', './internal/platform', './cmd/keel-admin', '-run', args.run], env=env, stdout=log, stderr=subprocess.STDOUT)
    transcript = (work / 'integration.log').read_text()
    evidence = go_test_evidence(transcript, result.returncode, {
        'owner_cli': ['TestCredentialMasterAtomicRewrapAndRecovery', 'TestAuditEvidenceDetectsOwnerChangesWithoutExportingMetadata', 'TestWebhookDurableOutboxDeliveryAndIsolation'],
        'ztp_200': ['TestZTPP1LogicalBatch200Postgres'],
    })
    passed = evidence['passed_tests']
    owner_cli_verified = all(marker in transcript for marker in ['P1_LIFECYCLE_MASTER_CLI_OK', 'P1_LIFECYCLE_AUDIT_CLI_OK', 'P1_LIFECYCLE_WEBHOOK_CLI_OK']) and all(name in passed for name in ['TestCredentialMasterAtomicRewrapAndRecovery', 'TestAuditEvidenceDetectsOwnerChangesWithoutExportingMetadata', 'TestWebhookDurableOutboxDeliveryAndIsolation'])
    ztp_batches = [json.loads(match.group(1)) for match in re.finditer(r'P1_ZTP_LOGICAL_BATCH (\{[^\n]+\})', transcript)]
    ztp_200 = next((batch for batch in ztp_batches if batch.get('logical_targets') == 200), None)
    ztp_200_verified = evidence['verified_groups']['ztp_200'] and ztp_logical_batch_evidence(ztp_200)
    (work / 'result.json').write_text(json.dumps({'exit_code': result.returncode, 'test_filter': args.run, 'real_postgresql_configured': True, 'owner_cli_binary_built': True, 'owner_cli_lifecycle_verified': result.returncode == 0 and owner_cli_verified and evidence['verified_groups']['owner_cli'], 'source': source_provenance(source_before, repository_source(root)), **evidence, 'ztp_200_logical_verified': ztp_200_verified, 'ztp_logical_batches': ztp_batches, 'object_transport': 'in-memory fixture', 'browser_run': False, 'image_build': False, 'production_writes': False, 'full_p1_acceptance': False}))
    print('P1 lifecycle batch exit:', result.returncode, flush=True)
    raise SystemExit(result.returncode)
finally:
    if created:
        subprocess.run(['docker', 'exec', container, 'dropdb', '-U', owner, database], check=True, capture_output=True)
