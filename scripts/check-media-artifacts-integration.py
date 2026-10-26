#!/usr/bin/env python3
"""Real PostgreSQL + disposable MinIO + API + browser media verification."""
import argparse,json,os,platform,secrets,socket,subprocess,tempfile,time,urllib.parse,urllib.request,uuid
from pathlib import Path
from media_integrity_fault_proxy import MediaIntegrityFaultProxy
from media_source_fixture import MediaSourceFixture
parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--catalog-only',action='store_true',help='Run only the catalog API/worker/browser check in a fresh isolated environment; skip Go and unchanged media checks');parser.add_argument('--management-only',action='store_true',help='Run only the catalog management browser check in a fresh isolated environment');parser.add_argument('--global-media-only',action='store_true',help='Run only global media API/browser checks in a fresh isolated environment');parser.add_argument('--media-conflict-only',action='store_true',help='Run only media confirmation concurrency browser checks');parser.add_argument('--dependencies-only',action='store_true',help='Run pinned artifact dependency Go and browser checks');parser.add_argument('--filters-only',action='store_true',help='Run media filters API/browser checks');parser.add_argument('--large-artifact-only',action='store_true',help='Run one 500MB real storage/approval/Range/offline bundle streaming check');parser.add_argument('--matrix-only',action='store_true',help='Run compatibility matrix import API/browser checks');parser.add_argument('--source-only',action='store_true',help='Run allowed external source Go and real HTTP/MinIO/API/browser checks');parser.add_argument('--integrity-only',action='store_true',help='Run bounded retry/quarantine Go and fault-injected real MinIO/API/browser checks');parser.add_argument('--manifest-only',action='store_true',help='Run signed media manifest Go, offline CLI and browser checks');parser.add_argument('--browser-only',action='store_true',help='Run all HTTP/browser checks, skipping unchanged Go checks');parser.add_argument('--provision-only',action='store_true',help='Run scoped boot/preflight/overlay software, approved preparation and browser checks; no actual PXE listener or hardware');args=parser.parse_args()
if sum([args.provision_only,args.catalog_only,args.management_only,args.global_media_only,args.media_conflict_only,args.dependencies_only,args.manifest_only,args.integrity_only,args.source_only,args.matrix_only,args.large_artifact_only,args.filters_only,args.browser_only])>1:parser.error('Choose one focused check')
full_browser=not any([args.provision_only,args.catalog_only,args.management_only,args.global_media_only,args.media_conflict_only,args.dependencies_only,args.manifest_only,args.integrity_only,args.source_only,args.matrix_only,args.large_artifact_only,args.filters_only])
os.umask(0o077);root=Path(__file__).resolve().parents[1];os.chdir(root)
probe=socket.socket();probe.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);probe.bind(('127.0.0.1',3102));probe.close()
record=json.loads(subprocess.check_output(['docker','inspect','keel-auth-test-postgres']))[0]
assert record['Config']['Labels'].get('io.keel.test-environment')=='disposable'
config=dict(v.split('=',1) for v in record['Config']['Env'] if '=' in v);owner,password=config['POSTGRES_USER'],config['POSTGRES_PASSWORD']
database='keel_test_media_'+uuid.uuid4().hex[:12];unit_database=database+'_unit';provision_database='keel_test_agent_provision_'+uuid.uuid4().hex[:12];work=Path(tempfile.mkdtemp(prefix='keel-media-'));api=worker=None;fault_proxy=source_fixture=None
# Keep real /tmp credential directories and the existing MinIO path guard;
# retain only the large binaries on disk because /tmp is a memory filesystem.
evidence_root=Path.home()/'.local/state/keel/evidence/media';evidence_root.mkdir(parents=True,exist_ok=True,mode=0o700);evidence_root.chmod(0o700)
evidence=evidence_root/work.name;evidence.mkdir(mode=0o700)
print('Media evidence directory: '+str(work),flush=True)
def checked(command,env=None):
 result=subprocess.run(command,env=env,text=True,capture_output=True)
 if result.returncode:
  (work/'failure.log').write_text(result.stdout+result.stderr)
  raise RuntimeError('Media integration command failed: '+command[0]+'; inspect protected failure.log')
 return result.stdout
try:
 for name in [database,unit_database,provision_database]:checked(['docker','exec','keel-auth-test-postgres','createdb','-U',owner,name])
 env=os.environ.copy();env.update(KEEL_ENV='development',KEEL_INSECURE_DEV_COOKIE='true',KEEL_PUBLIC_ORIGINS='http://127.0.0.1:3102',KEEL_LISTEN_ADDR='127.0.0.1:3102',KEEL_STATIC_DIR=str(root/'web/dist'))
 checked(['python3','scripts/test-media-environment.py','start','--directory',str(work)])
 env.update(dict(line.split('=',1) for line in (work/'media.env').read_text().splitlines()))
 dsn='postgresql://'+urllib.parse.quote(owner,safe='')+':'+urllib.parse.quote(password,safe='')+'@127.0.0.1:15433/'
 env.update(KEEL_DATABASE_URL=dsn+database+'?sslmode=disable',KEEL_TEST_DATABASE_URL=dsn+unit_database+'?sslmode=disable')
 for target in ['api','admin','worker']:
  executable=evidence/('keel-'+target);checked(['go','build','-o',str(executable),'./cmd/keel-'+target],env);(work/('keel-'+target)).symlink_to(executable)
 if (args.provision_only or full_browser) and not args.browser_only:
  helper_env=env.copy();helper_env.update(CGO_ENABLED='0',GOOS='linux',GOARCH={'x86_64':'amd64','aarch64':'arm64'}.get(platform.machine(),''))
  if not helper_env['GOARCH']:raise RuntimeError('Unsupported native architecture for installer helpers')
  for target in ['preflight','ssh-runner']:
   executable=evidence/('keel-'+target);checked(['go','build','-o',str(executable),'./cmd/keel-'+target],helper_env)
  env.update(KEEL_TEST_PROVISION_ADMIN_BIN=str(work/'keel-admin'),KEEL_TEST_PROVISION_PREFLIGHT_BIN=str(evidence/'keel-preflight'),KEEL_TEST_PROVISION_SSH_HELPER_BIN=str(evidence/'keel-ssh-runner'))
  result=checked(['go','test','-p=1','-race','-count=1','-v','./internal/mediaboot','./internal/provisionboot','./internal/provisionpreflight','./internal/provisionimage','./internal/provisionstorage','./internal/provisioninitrd','./internal/provisionwriter','./internal/provisionfirstboot'],env)
  (work/'go-provision-foundation.log').write_text(result)
 checked([str(work/'keel-admin'),'media-init'],env)
 if args.source_only or full_browser:
  source_fixture=MediaSourceFixture(work);env['KEEL_MEDIA_FETCH_ORIGINS']=source_fixture.start();env['KEEL_MEDIA_SOURCE_FIXTURE_ORIGIN']=env['KEEL_MEDIA_FETCH_ORIGINS']
 if args.integrity_only or full_browser:
  fault_proxy=MediaIntegrityFaultProxy(work,env['KEEL_MEDIA_ENDPOINT']);env['KEEL_MEDIA_ENDPOINT']=fault_proxy.start()
 if args.integrity_only or args.manifest_only or args.matrix_only or args.large_artifact_only or full_browser:
  checked([str(work/'keel-admin'),'media-signing-keygen','--private-key-file',str(work/'manifest-private.pem'),'--public-key-file',str(work/'manifest-public.pem')],env)
  env.update(KEEL_MEDIA_SIGNING_KEY_FILE=str(work/'manifest-private.pem'),KEEL_MEDIA_MANIFEST_PUBLIC_KEY_FILE=str(work/'manifest-public.pem'),KEEL_ADMIN_BINARY=str(work/'keel-admin'))
 if args.provision_only or full_browser:
  env.update(KEEL_PROVISION_PREPARE_ENABLED='true',KEEL_TEST_AGENT_DATABASE_URL=dsn+provision_database+'?sslmode=disable')
 if args.provision_only:
  result=checked(['go','test','-p=1','-race','./internal/mediamanifest','./internal/provisionseed','./internal/platform','./internal/execution','-run','TestOSBoot|TestSignedOSBootRoles|TestSignedBootRecipe|TestInstallerOverlay|TestInstallerInitrd|TestProvisionPreview|TestProvisionRequest|TestProvisionBootScope|TestProvisionBootAuthority|TestProvisionBootDetached|TestProvisionExecutionKernelPostgres|TestProvisionInstallationKernelPostgres|TestProvisionInstallationBaselinePostgres|TestProvisionInstallationRolloutPostgres|TestProvisionRollout|TestProvisionInstallationLiveBootPostgres|TestProvisionInstallationWritePermitPostgres|TestProvisionInstallationFirstBootPostgres|TestProvisionInstallationFirstSSHPostgres|TestProvisionInstallationFirstSSHOrphanDispatchPostgres|TestProvisionInstallationRepairListPostgres|TestProvisionInstallationConfigurationPostgres|TestProvisionInstallationKnownFailedSSHPostgres|TestProvisionInstallationSSHBatchPostgres|TestProvisionInstallationFileDeliveryPostgres|TestProvisionInstallationRegisteredRecoveryPostgres|TestProvisionInstallationPrewriteRecoveryPostgres|TestProvisionRack|TestProvisionProgress|TestCloudInit|TestModelPreflight','-count=1'],env)
  (work/'go-integration.log').write_text(result);print(result,flush=True)
 elif args.filters_only:
  result=checked(['go','test','-race','./internal/platform','-run','TestMediaIntegrityRetriesAndQuarantine','-count=1'],env)
  (work/'go-integration.log').write_text(result);print(result,flush=True)
 elif args.source_only:
  result=checked(['go','test','-race','./internal/mediasource','./internal/platform','-run','TestSource|TestMediaSourceFetch','-count=1'],env)
  (work/'go-integration.log').write_text(result);print(result,flush=True)
 elif args.integrity_only or args.manifest_only:
  result=checked(['go','test','-race','./internal/mediamanifest','./internal/platform','-run','TestMediaIntegrity|TestSignedMediaManifest|TestMediaArtifactDependenc|TestSignedManifest|TestMediaBundle|TestSigning','-count=1'],env)
  (work/'go-integration.log').write_text(result);print(result,flush=True)
 elif args.dependencies_only:
  result=checked(['go','test','-race','./internal/platform','-run','TestMediaArtifactDependenc|TestCatalogArtifactAdmission|TestMediaArtifactsLifecycleAndIntegrity','-count=1'],env)
  (work/'go-integration.log').write_text(result);print(result,flush=True)
 elif not (args.catalog_only or args.management_only or args.global_media_only or args.media_conflict_only or args.matrix_only or args.large_artifact_only or args.filters_only or args.browser_only):
  result=checked(['go','test','-race','./internal/mediaartifact','./internal/mediamanifest','./internal/mediasource','./internal/platform','-run','TestSource|TestMediaSourceFetch|TestMediaIntegrity|TestSignedManifest|TestMediaBundle|TestSigning|TestSignedOSBootRoles|TestSignedBootRecipe|TestInstallerOverlay|TestInstallerInitrd|TestSignedMediaManifest|TestRejectUnsafeMediaConfiguration|TestRealMinIOVersionPinning|TestMediaArtifactsLifecycleAndIntegrity|TestMediaArtifactDependenc|TestMediaConditionalTransitions|TestCatalogArtifactAdmission|TestCatalogCorrelatedCombinations|TestCatalogManagement|TestGlobalCatalogScopeBoundaries|TestCompatibility|TestZTP239ApprovedIsolationScript|TestSZTP143|TestZTPMockKeaDurableStageCommit|TestBOMPackage','-count=1'],env)
  (work/'go-integration.log').write_text(result);print(result,flush=True)
 if full_browser and not args.browser_only:
  result=checked(['go','test','-p=1','-race','-count=1','./internal/platform','./internal/execution','-run','TestOSBoot|TestProvisionPreview|TestProvisionRequest|TestProvisionBootScope|TestProvisionBootAuthority|TestProvisionBootDetached|TestProvisionExecutionKernelPostgres|TestProvisionInstallationKernelPostgres|TestProvisionInstallationBaselinePostgres|TestProvisionInstallationRolloutPostgres|TestProvisionRollout|TestProvisionInstallationLiveBootPostgres|TestProvisionInstallationWritePermitPostgres|TestProvisionInstallationFirstBootPostgres|TestProvisionInstallationFirstSSHPostgres|TestProvisionInstallationFirstSSHOrphanDispatchPostgres|TestProvisionInstallationRepairListPostgres|TestProvisionInstallationConfigurationPostgres|TestProvisionInstallationKnownFailedSSHPostgres|TestProvisionInstallationSSHBatchPostgres|TestProvisionInstallationFileDeliveryPostgres|TestProvisionInstallationRegisteredRecoveryPostgres|TestProvisionInstallationPrewriteRecoveryPostgres|TestProvisionRack|TestProvisionProgress|TestModelPreflight'],env)
  (work/'go-provision-domain.log').write_text(result)
 checked([str(work/'keel-admin'),'migrate'],env)
 if args.provision_only or full_browser:
  checked([str(work/'keel-admin'),'credential-master-keygen','--output',str(work/'provision-master')],env)
  env.update(KEEL_ADMIN_BINARY=str(work/'keel-admin'),KEEL_PROVISION_TEST_MASTER_FILE=str(work/'provision-master'))
 pw=secrets.token_urlsafe(28);secret=work/'password';secret.write_text(pw)
 checked([str(work/'keel-admin'),'init-admin','--username','mediaadmin','--display-name','Media Admin','--password-file',str(secret)],env)
 api=subprocess.Popen([str(work/'keel-api')],env=env,stdout=(work/'api.log').open('w'),stderr=subprocess.STDOUT)
 for _ in range(100):
  if api.poll() is not None:raise RuntimeError('Isolated API exited; inspect protected api.log')
  try:urllib.request.urlopen('http://127.0.0.1:3102/healthz',timeout=1);break
  except Exception:time.sleep(.1)
 else:raise RuntimeError('API did not become ready')
 worker=subprocess.Popen([str(work/'keel-worker')],env=env,stdout=(work/'worker.log').open('w'),stderr=subprocess.STDOUT)
 credentials=work/'credentials.json';credentials.write_text(json.dumps(dict(base_url='http://127.0.0.1:3102',username='mediaadmin',password=pw)))
 env.update(KEEL_AUTH_BROWSER_CREDENTIALS_FILE=str(credentials),KEEL_WORKSPACE_EVIDENCE_DIR=str(work),KEEL_ISOLATED_API_PID=str(api.pid))
 checks=[(['node','scripts/check-provision-preparation-browser.mjs'],'provision-browser'),(['node','scripts/check-media-dependencies-browser.mjs'],'dependency-browser'),(['python3','scripts/check-media-integration.py'],'legacy-media'),(['python3','scripts/check-param-integration.py'],'parameter-regression'),(['node','scripts/check-media-browser.mjs'],'legacy-browser'),(['node','scripts/check-media-artifacts-browser.mjs'],'artifact-browser'),(['node','scripts/check-catalog-browser.mjs'],'catalog-browser'),(['node','scripts/check-catalog-management-browser.mjs'],'catalog-management-browser'),(['node','scripts/check-global-media-browser.mjs'],'global-media-browser'),(['node','scripts/check-media-conflict-browser.mjs'],'media-conflict-browser'),(['node','scripts/check-media-manifest-browser.mjs'],'manifest-browser'),(['node','scripts/check-media-integrity-browser.mjs'],'integrity-browser'),(['node','scripts/check-media-source-browser.mjs'],'source-browser'),(['node','scripts/check-media-matrix-browser.mjs'],'matrix-browser'),(['node','scripts/check-media-filters-browser.mjs'],'filters-browser')]
 for command,name in ([check for check in checks if check[1] == 'provision-browser'] if args.provision_only else [(['python3','scripts/check-media-large-artifact.py'],'large-artifact')] if args.large_artifact_only else [check for check in checks if check[1] == 'filters-browser'] if args.filters_only else [check for check in checks if check[1] == 'matrix-browser'] if args.matrix_only else [check for check in checks if check[1] == 'source-browser'] if args.source_only else [check for check in checks if check[1] == 'integrity-browser'] if args.integrity_only else [check for check in checks if check[1] == 'manifest-browser'] if args.manifest_only else [check for check in checks if check[1] == 'dependency-browser'] if args.dependencies_only else [check for check in checks if check[1] == 'media-conflict-browser'] if args.media_conflict_only else [check for check in checks if check[1] == 'global-media-browser'] if args.global_media_only else [check for check in checks if check[1] == 'catalog-management-browser'] if args.management_only else [check for check in checks if check[1].startswith('catalog-')] if args.catalog_only else checks):
  result=checked(command,env);(work/(name+'.log')).write_text(result);print(result,flush=True)
finally:
 if worker is not None:
  worker.terminate()
  try:worker.wait(timeout=10)
  except subprocess.TimeoutExpired:worker.kill();worker.wait()
 if api is not None:
  api.terminate()
  try:api.wait(timeout=10)
  except subprocess.TimeoutExpired:api.kill();api.wait()
 if source_fixture is not None:source_fixture.close()
 if fault_proxy is not None:fault_proxy.close()
 if (work/'media-container').exists():subprocess.run(['python3','scripts/test-media-environment.py','stop','--directory',str(work)],capture_output=True)
 for name in [database,unit_database,provision_database]:subprocess.run(['docker','exec','keel-auth-test-postgres','dropdb','--if-exists','-U',owner,name],capture_output=True)
 for name in ['password','credentials.json','manifest-private.pem','wrong-private.pem']:(work/name).unlink(missing_ok=True)
 print('Isolated API/databases/MinIO and temporary credentials removed; evidence retained.',flush=True)
