import { ArtifactReceiptSummary } from './ArtifactReceiptSummary'
import {RerunDialog} from './RerunDialog'
import { CacheOutput } from './CacheOutput'
import { ScriptReceiptSummary } from './ScriptReceiptSummary'
import { ExecutionReportDownload } from './ExecutionReportDownload'
import { ScriptNonstartProof } from './ScriptNonstartProof'
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table";
import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  ActionDialog,
  ActionError,
  Pagination,
} from "../../components/Management";
import { useAction } from "../../lib/management";
import { useProject } from "../../components/ProjectWorkspace";
import { useSession, useExpireOnUnauthorized } from "../../lib/session";
import { controlExecution, reconcileAttempt } from "../../lib/execution";
import { ApiError } from "../../lib/api";
import {
  downloadOutput,
  getAttempt,
  getJournal,
  getOverview,
  getOutputs,
  getRelations,
  mergeJournal,
  useExecutionStream,
  type ExecutionOverview,
  type ExecutionViewSearch,
  type ExecutionViewTab,
  type JournalEntry,
} from "../../lib/execution-views";

import "../../styles/execution-views.css";
function accessDenied(error: unknown): error is ApiError {
  return error instanceof ApiError && [401, 403, 404].includes(error.status);
}
const Targets = lazy(() => import("./ExecutionTargets"));
const terminal = ["success", "failed", "killed", "warning"],
  tabs: ExecutionViewTab[] = [
    "overview",
    "targets",
    "logs",
    "outputs",
    "relations",
  ];
const stamp = (value: string | null | undefined) =>
  value ? new Date(value).toLocaleString() : "—";
function Timeline({ overview }: { overview: ExecutionOverview }) {
  const { t } = useTranslation(),
    first = Math.min(
      ...overview.timeline.flatMap((line) =>
        line.first_started_at ? [Date.parse(line.first_started_at)] : [],
      ),
      Date.parse(overview.execution.created_at),
    ),
    last = Math.max(first + 1, Date.parse(overview.observed_at)),
    span = last - first;
  return (
    <section>
      <h2>{t("executionViewTimeline")}</h2>
      <p>{t("executionViewTimelineHint")}</p>
      <div
        className="execution-gantt"
        role="img"
        aria-label={t("executionViewTimeline")}
      >
        {overview.timeline.map((line) => {
          const state = line.counts.failed
              ? "failed"
              : line.counts.unknown
                ? "unknown"
                : line.counts.running
                  ? "running"
                  : line.counts.pending
                    ? "pending"
                    : line.counts.success
                      ? "success"
                      : "skipped",
            start = line.first_started_at
              ? Date.parse(line.first_started_at)
              : null,
            end = line.last_observed_at
              ? Date.parse(line.last_observed_at)
              : null;
          return (
            <div className="execution-gantt-row" key={line.task_id}>
              <div>
                <strong>{line.task_id}</strong>
                <small>{line.task_type}</small>
              </div>
              <div className="execution-gantt-track">
                {start !== null ? (
                  <span
                    className="execution-gantt-bar"
                    data-state={state}
                    style={{
                      left: `${Math.max(0, ((start - first) / span) * 100)}%`,
                      width: `${Math.max(0.6, (((end ?? start) - start) / span) * 100)}%`,
                    }}
                    title={`${stamp(line.first_started_at)} → ${stamp(line.last_observed_at)}`}
                  >
                    {end === null
                      ? "?"
                      : `${((end - start) / 1000).toFixed(2)}s`}
                  </span>
                ) : (
                  <span>{t("executionViewNotScheduled")}</span>
                )}
              </div>
              <span>
                {Object.entries(line.counts)
                  .map(([key, n]) => `${t("attempt_state_" + key)} ${n}`)
                  .join(" · ")}
              </span>
            </div>
          );
        })}
      </div>
    </section>
  );
}
function ScriptResult({ output }: { output: unknown }) {
  const { t } = useTranslation()
  if (!output || typeof output !== 'object' || !('observation' in output)) return null
  const observed = output.observation
  if (!observed || typeof observed !== 'object') return null
  if ('structured_result_json' in observed && typeof observed.structured_result_json === 'string') return <details open><summary>{t('executionStructuredResult')}</summary><pre>{observed.structured_result_json}</pre></details>
  if ('reason' in observed && observed.reason === 'script_json_invalid') return <p role="status">{t('executionStructuredResultInvalid')}</p>
  return null
}

function Outputs({
  projectId,
  executionId,
  page,
  onPage,
}: {
  projectId: string;
  executionId: string;
  page: number;
  onPage: (value: number) => void;
}) {
  const session = useSession(),
    { t } = useTranslation(),
    action = useAction(),
    data = useQuery({
      queryKey: [
        "private",
        session.user.id,
        "project",
        projectId,
        "execution-view",
        executionId,
        "outputs",
        page,
      ],
      queryFn: ({ signal }) => getOutputs(projectId, executionId, page, signal),
      retry: false,
    });
  useExpireOnUnauthorized(data.error);
  return (
    <section>
      <h2>{t("executionViewOutputs")}</h2>
      <ActionError error={data.error ?? action.error} />
      {data.isPending && <p role="status">{t("loadingRecords")}</p>}
      {data.data && !accessDenied(data.error) && !accessDenied(action.error) && (
        <>
          <p>{t("executionViewArtifactsUnavailable")}</p>
          <details open>
            <summary>{t("executionViewFrozenParameters")}</summary>
            <pre>{JSON.stringify(data.data.parameters, null, 2)}</pre>
          </details>
          <div className="table-scroll">
            <Table className="data-table">
              <TableHeader>
                <TableRow>
                  <TableHead>{t("attemptTask")}</TableHead>
                  <TableHead>{t("attemptTarget")}</TableHead>
                  <TableHead>{t("attemptState")}</TableHead>
                  <TableHead>{t("executionViewReceipt")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.data.items.map((item) => (
                  <TableRow key={item.attempt_id}>
                    <TableCell>{item.task_id}</TableCell>
                    <TableCell>
                      <code>{item.target_id}</code>
                    </TableCell>
                    <TableCell>{item.receipt.result}</TableCell>
                    <TableCell>
                      <Button variant="outline"
                        className="secondary-button"
                        disabled={action.busy}
                        onClick={() =>
                          void action.run(() =>
                            downloadOutput(
                              projectId,
                              executionId,
                              item.attempt_id,
                            ),
                          )
                        }
                      >
                        {t("executionViewDownloadJSON")}
                      </Button>
                      <details>
                        <summary>{t("executionViewStructuredOutput")}</summary>
                        <ScriptReceiptSummary output={item.receipt.output} /><ArtifactReceiptSummary output={item.receipt.output} />
                        <ScriptResult output={item.receipt.output} />
                        <CacheOutput key={item.attempt_id} output={item.receipt.output} />
                      <pre>
                          {JSON.stringify(item.receipt.output, null, 2)}
                        </pre>
                        <code>{item.digest}</code>
                      </details>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <Pagination
            offset={(page - 1) * 20}
            limit={20}
            total={data.data.total}
            busy={data.isFetching}
            onChange={(offset) => onPage(offset / 20 + 1)}
          />
        </>
      )}
    </section>
  );
}
function Relations({
  projectId,
  executionId,
}: {
  projectId: string;
  executionId: string;
}) {
  const session = useSession(),
    { t } = useTranslation(),
    data = useQuery({
      queryKey: [
        "private",
        session.user.id,
        "project",
        projectId,
        "execution-view",
        executionId,
        "relations",
      ],
      queryFn: ({ signal }) => getRelations(projectId, executionId, signal),
      retry: false,
    });
  useExpireOnUnauthorized(data.error);
  return (
    <section>
      <h2>{t("executionViewRelations")}</h2>
      <ActionError error={data.error} />
      {data.isPending && <p role="status">{t("loadingRecords")}</p>}
      {data.data && !accessDenied(data.error) && (
        <>
          <dl className="execution-view-facts">
            <dt>{t("flowWorkbenchTitle")}</dt>
            <dd>
              <Link
                to="/p/$projectId/flows"
                params={{ projectId }}
                search={{
                  flow: data.data.flow.flow_id,
                  tab: "revisions",
                  right: data.data.flow.id,
                }}
              >
                {data.data.flow.name} · v{data.data.flow.version}
              </Link>
            </dd>
            <dt>{t("flowRevisionHash")}</dt>
            <dd>
              <code>{data.data.flow.content_hash}</code>
            </dd>
            <dt>{t("executionViewTargetHash")}</dt>
            <dd>
              <code>{data.data.target_set_hash}</code>
            </dd>
            <dt>{t("executionViewFrozenAt")}</dt>
            <dd>{stamp(data.data.frozen_at)}</dd>
            {data.data.upstream_execution_id && <><dt>{t('rerunUpstream')}</dt><dd><Link to="/p/$projectId/executions" params={{projectId}} search={{execution:data.data.upstream_execution_id,tab:'overview'}}>{data.data.upstream_execution_id}</Link></dd></>}
            {data.data.kea_deployment_id && (
              <>
                <dt>Kea</dt>
                <dd>
                  <Link to="/p/$projectId/ztp" params={{ projectId }}>
                    {data.data.kea_deployment_id}
                  </Link>
                </dd>
              </>
            )}
            {data.data.push_job_id && (
              <>
                <dt>PushJob</dt>
                <dd>
                  <Link to="/p/$projectId/configuration" params={{ projectId }}>
                    {data.data.push_job_id}
                  </Link>
                </dd>
              </>
            )}
          </dl>
          <p>{t("executionViewRelationsUnavailable")}</p>
          <details>
            <summary>{t("executionViewFrozenParameters")}</summary>
            <pre>{JSON.stringify(data.data.parameters, null, 2)}</pre>
          </details>
        </>
      )}
    </section>
  );
}
function AttemptDrawer({
  projectId,
  executionId,
  attempt,
  onClose,
  onLogs,
  onRerun,
}: {
  projectId: string;
  executionId: string;
  attempt: string;
  onClose: () => void;
  onLogs: () => void;
  onRerun?: (targetID:string) => void;
}) {
  const session = useSession(), project = useProject(), client = useQueryClient(),
    { t } = useTranslation(),
    action = useAction(),
    prior = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null),
    query = useQuery({
      queryKey: [
        "private",
        session.user.id,
        "project",
        projectId,
        "execution-view",
        executionId,
        "attempt",
        attempt,
      ],
      queryFn: ({ signal }) =>
        getAttempt(projectId, executionId, attempt, signal),
      retry: false,
    });
  const [evidence, setEvidence] = useState(''), [denied, setDenied] = useState<ApiError | null>(null)
  const submission = useRef({ body: '', key: '' })
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useExpireOnUnauthorized(query.error);
  useEffect(() => {
    if (!accessDenied(action.error)) return
    setDenied(action.error)
    void client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId] })
  }, [action.error])
  const canReconcile = session.user.is_admin || ['Admin', 'DL', 'SRE'].includes(project.role)
  const proof = query.data?.attempt.receipt
  const currentAttempt = query.data?.attempt
  const matchingProof = proof && currentAttempt && proof.attempt_id === currentAttempt.id && proof.command_id === currentAttempt.command_id && proof.fencing_token === currentAttempt.fencing_token
  async function reconcile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!currentAttempt || !matchingProof || !proof || currentAttempt.state !== 'unknown' || !canReconcile || denied || evidence.trim().length < 10 || evidence.length > 2000) return
    const body = { expected_version: currentAttempt.state_version, result: proof.result, evidence: evidence.trim() }
    const serialized = JSON.stringify(body)
    if (submission.current.body !== serialized) submission.current = { body: serialized, key: crypto.randomUUID() }
    const result = await action.run(() => reconcileAttempt(projectId, executionId, attempt, body, session.csrf_token, submission.current.key))
    if (result.ok && mounted.current) {
      await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'execution-view', executionId] })
      await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'executions'] })
      if (mounted.current) onClose()
    }
  }
  return (
    <Dialog open onOpenChange={open => { if (!open && !action.busy) onClose(); }}>
    <DialogContent
      className="execution-attempt-drawer ui:top-0 ui:left-auto ui:right-0 ui:translate-x-0 ui:translate-y-0 ui:w-[min(640px,100vw)] ui:h-dvh ui:max-h-dvh ui:max-w-none ui:sm:max-w-none ui:rounded-none ui:overflow-auto"
      aria-describedby={undefined}
      showCloseButton={false}
      onEscapeKeyDown={event => { if (action.busy) event.preventDefault(); }}
      onInteractOutside={event => event.preventDefault()}
      onCloseAutoFocus={event => { event.preventDefault(); if(prior.current?.isConnected) prior.current.focus(); }}
    >
      <div className="execution-view-toolbar">
        <DialogTitle id="execution-attempt-heading">
          {t("executionViewAttemptDetails")}
        </DialogTitle>
        <Button variant="outline" type="button" className="secondary-button" disabled={action.busy} onClick={onClose}>
          {t("executionViewClose")}
        </Button>
      </div>
      <ActionError error={query.error} />
      <ActionError error={denied ?? action.error} />
      {query.isPending && <p role="status">{t("loadingRecords")}</p>}
      {query.data && !accessDenied(query.error) && !denied && (
        <>
          <dl className="execution-view-facts">
            <dt>{t("attemptTarget")}</dt>
            <dd>{query.data.target.name}</dd>
            <dt>{t("attemptTask")}</dt>
            <dd>{query.data.task.id}</dd>
            <dt>{t("attemptNo")}</dt>
            <dd>{query.data.attempt.attempt_no}</dd>
            <dt>{t("attemptState")}</dt>
            <dd>{t("attempt_state_" + query.data.attempt.state)}</dd>
            <dt>{t("executionStarted")}</dt>
            <dd>{stamp(query.data.attempt.started_at)}</dd>
            <dt>{t("executionFinished")}</dt>
            <dd>{stamp(query.data.attempt.finished_at)}</dd>
            {query.data.mode === 'agent' && query.data.target.agent && <>
              <dt>{t('agentCredential')}</dt>
              <dd><code className="ui:break-all">{query.data.target.agent.host_key}</code><span className="cell-subtitle">v{query.data.target.revision}</span></dd>
              <dt>SHA256</dt>
              <dd><code className="ui:break-all">{query.data.target.agent.config_hash}</code></dd>
            </>}
          </dl>
          <Button variant="outline" type="button" className="secondary-button" disabled={action.busy} onClick={onLogs}>
            {t("executionViewFocusLogs")}
          </Button>
          {onRerun && <Button variant="outline" onClick={()=>onRerun(query.data!.target.id)}>{t('rerunTarget')}</Button>}
          <h3>{t("executionViewReceipt")}</h3>
          {query.data.attempt.receipt && <><ScriptReceiptSummary output={query.data.attempt.receipt.output} /><ArtifactReceiptSummary output={query.data.attempt.receipt.output} /></>}
          <pre>
            {query.data.attempt.receipt
              ? JSON.stringify(query.data.attempt.receipt, null, 2)
              : t("executionViewNoReceipt")}
          </pre>
          {currentAttempt?.state === 'unknown' && <section aria-label={t('executionReconcileTitle')}>
            <h3>{t('executionReconcileTitle')}</h3>
            <p className="impact-notice">{t(matchingProof ? 'executionReconcileDescription' : 'executionReconcilePending')}</p>
            <Button type="button" variant="outline" disabled={action.busy || query.isFetching} onClick={() => void query.refetch()}>{t('executionReconcileRefresh')}</Button>
            {canReconcile && !matchingProof && (query.data.task.script || query.data.task.artifact) && ['ssh', 'agent'].includes(query.data.mode) && !denied && !['archived', 'delivered'].includes(project.state) && <ScriptNonstartProof artifact={!!query.data.task.artifact} key={`${attempt}:${currentAttempt.state_version}`} projectId={projectId} executionId={executionId} attemptId={attempt} version={currentAttempt.state_version} onDenied={error => setDenied(error)} onRequested={() => { void query.refetch() }} />}
            {canReconcile && matchingProof && proof && !['archived', 'delivered'].includes(project.state) && <form onSubmit={event => void reconcile(event)} aria-busy={action.busy}>
              <p>{t('executionReconcileResult')}: <strong>{t('attempt_state_' + proof.result)}</strong></p>
              <div className="field"><Label htmlFor="execution-reconcile-evidence">{t('executionReconcileEvidence')}</Label><Textarea id="execution-reconcile-evidence" value={evidence} onChange={event => setEvidence(event.target.value)} required minLength={10} maxLength={2000} disabled={action.busy} aria-describedby="execution-reconcile-hint" /><p className="field-hint" id="execution-reconcile-hint">{t('executionReconcileEvidenceHint')}</p></div>
              <Button type="submit" disabled={action.busy || query.isFetching || evidence.trim().length < 10 || evidence.length > 2000}>{t('executionReconcileConfirm')}</Button>
            </form>}
          </section>}
        </>
      )}
    </DialogContent>
    </Dialog>
  );
}
export function ExecutionWorkspace({
  projectId,
  executionId,
  onClose,
}: {
  projectId: string;
  executionId: string;
  onClose: () => void;
}) {
  const project = useProject(),
    session = useSession(),
    { t } = useTranslation(),
    client = useQueryClient(),
    navigate = useNavigate(),
    search = useSearch({ strict: false }) as ExecutionViewSearch & {
      execution?: string;
      page?: number;
    },
    tab = search.tab ?? "overview",
    control = useAction();
  const [scopeDenied, setScopeDenied] = useState<ApiError | null>(null);
  const [rerun,setRerun]=useState<{mode:"failed"|"targets";targetIDs:string[]}|null>(null);
  const [confirm, setConfirm] = useState<"pause" | "resume" | "cancel" | null>(
      null,
    ),
    [events, setEvents] = useState<JournalEntry[]>([]),
    [resync, setResync] = useState(false);
  const prefix = [
    "private",
    session.user.id,
    "project",
    projectId,
    "execution-view",
    executionId,
  ];
  const overview = useQuery({
    queryKey: [...prefix, "overview"],
    queryFn: ({ signal }) => getOverview(projectId, executionId, signal),
    retry: false,
  });
  const journal = useQuery({
    queryKey: [...prefix, "journal"],
    queryFn: ({ signal }) => getJournal(projectId, executionId, 0, signal),
    enabled: tab === "logs",
    retry: false,
  });
  const denied = scopeDenied ?? [overview.error, journal.error, control.error].find(accessDenied);
  const current = denied ? undefined : overview.data,
    active = current ? !terminal.includes(current.execution.state) : false;
  useEffect(() => { if (denied) { setConfirm(null); setRerun(null); setEvents([]); setResync(false) } }, [denied]);
  useExpireOnUnauthorized(overview.error ?? journal.error);
  function go(value: Partial<ExecutionViewSearch>) {
    void navigate({
      to: "/p/$projectId/executions",
      params: { projectId },
      search: { ...search, ...value },
    });
  }
  useEffect(() => {
    if (journal.data) {
      setEvents((previous) => mergeJournal(journal.data.items, previous));
      setResync(false);
    }
  }, [journal.data]);
  useEffect(() => {
    if (tab === "logs" && !active && current) void journal.refetch();
  }, [tab, active, executionId]);
  const connection = useExecutionStream(
    projectId,
    executionId,
    Boolean(
      current &&
        active &&
        ["overview", "targets", "logs"].includes(tab) &&
        (tab !== "logs" || journal.data),
    ),
    tab,
    tab === "logs"
      ? (journal.data?.next_cursor ?? 0)
      : (current?.event_cursor ?? 0),
    {
      onEvents: (incoming, replace) => {
        if (tab === "logs")
          setEvents((previous) =>
            mergeJournal(replace ? [] : previous, incoming),
          );
        setResync(false);
      },
      onUpdate: () => {
        void client.invalidateQueries({ queryKey: prefix });
      },
      onResync: () => {
        setResync(true);
        void client.invalidateQueries({ queryKey: prefix });
      },
      onDenied: () => {
        setScopeDenied(new ApiError(403, "FORBIDDEN"));
        void client.invalidateQueries({
          queryKey: ["private", session.user.id, "project", projectId],
        });
      },
    },
  );
  const previousTab = useRef(tab);
  useEffect(() => {
    if (
      previousTab.current !== tab &&
      ["overview", "targets", "logs"].includes(tab)
    )
      void client.invalidateQueries({ queryKey: [...prefix, "overview"] });
    previousTab.current = tab;
  }, [tab]);
  async function runControl(e: FormEvent) {
    e.preventDefault();
    if (!current || !confirm) return;
    const result = await control.run(() =>
      controlExecution(
        projectId,
        executionId,
        { action: confirm, expected_version: current.execution.state_version },
        session.csrf_token,
      ),
    );
    if (result.ok) {
      setConfirm(null);
      await client.invalidateQueries({ queryKey: prefix });
    }
  }
  const trustVerification = Boolean(current?.execution.trust_verification);
  const canControl = Boolean(
    current &&
      ((current.execution.mode === "sim" || current.execution.mode === "ssh" || current.execution.mode === "agent" || current.execution.mode === "provision")
        ? session.user.is_admin ||
          (trustVerification ? ["Admin", "SRE", "DL"] : ["Admin", "SRE", "DE", "DL"]).includes(project.role)
        : session.user.is_admin || ["Admin", "NDE"].includes(project.role)),
  );
  const visible = events.filter(
    (event) =>
      (!search.level || event.level === search.level) &&
      (!search.logAttempt ||
        (event.payload &&
          typeof event.payload === "object" &&
          (("attempt_id" in event.payload &&
            event.payload.attempt_id === search.logAttempt) ||
            ("attempt_ids" in event.payload &&
              Array.isArray(event.payload.attempt_ids) &&
              event.payload.attempt_ids.includes(search.logAttempt))))),
  );
  if (overview.isPending) return <p role="status">{t("loadingRecords")}</p>;
  if (!current)
    return (
      <section className="panel">
        <ActionError error={denied ?? overview.error} />
        <Button variant="outline" className="secondary-button" onClick={onClose}>
          {t("backToList")}
        </Button>
      </section>
    );
  return (
    <section className="execution-workspace">
      <header className="execution-view-toolbar">
        <div>
          <h1>
            {current.flow_name} · v{current.flow_version}
          </h1>
          <p>
            <code>{executionId}</code> · {t("state_" + current.execution.state)}{" "}
            · {current.execution.mode}
          </p>
        </div>
        <Button variant="outline" className="secondary-button" onClick={onClose}>
          {t("backToList")}
        </Button>
        <ExecutionReportDownload key={`${projectId}:${executionId}`} projectId={projectId} executionId={executionId} onDenied={error => setScopeDenied(error)} />
      </header>
      <p className="impact-notice">
        {t(
          ["ssh", "agent"].includes(current.execution.mode) && current.timeline.some(task => task.task_type === "media.fetch")
            ? "artifactRetrievalNotice"
            : ["ssh", "agent"].includes(current.execution.mode) && current.timeline.some(task => task.task_type === "media.stage")
            ? "artifactExecutionNotice"
            : ["ssh", "agent"].includes(current.execution.mode) && current.timeline.some(task => task.task_type === "script.run")
            ? "scriptExecutionNotice"
            : current.execution.mode === "provision"
            ? "provisionPreparationOnly"
            : current.execution.mode === "ssh"
            ? trustVerification ? "sshTrustExecutionNotice" : "sshExecutionNotice"
            : current.execution.mode === "agent"
            ? "agentExecutionNotice"
            : current.execution.mode === "fabric"
            ? "networkExecutionDetail"
            : current.execution.mode === "kea"
              ? "keaControlDetail"
              : "noDeviceConnection",
        )}
      </p>
      {canControl && (
        <div className="execution-view-toolbar">
          <p>{t("controlHint")}</p>
          {(["pause", "resume", "cancel"] as const).map((action) => (
            <Button
              variant="outline"
              className={
                action === "cancel" ? "danger-text ui:text-destructive ui:border-destructive" : "secondary-button"
              }
              key={action}
              disabled={!active || control.busy}
              onClick={() => setConfirm(action)}
            >
              {t(action === "cancel" ? "cancelExecution" : action)}
            </Button>
          ))}
        </div>
      )}
      {canControl && current.execution.mode==='sim' && !active && <Button variant="outline" onClick={()=>setRerun({mode:'failed',targetIDs:[]})}>{t('rerunFailed')}</Button>}
      {rerun && <RerunDialog projectId={projectId} projectName={project.name} sourceExecutionId={executionId} mode={rerun.mode} targetIDs={rerun.targetIDs} onClose={()=>setRerun(null)} onDone={id=>{setRerun(null);void client.invalidateQueries({queryKey:['private',session.user.id,'project',projectId]});void navigate({to:'/p/$projectId/executions',params:{projectId},search:{execution:id,tab:'overview'}})}}/>}
      <div
        role="tablist"
        className="execution-view-tabs"
        aria-label={t("executionViewTabs")}
      >
        {tabs.map((value) => (
          <Button
            variant={null}
            size={null}
            className="ui:rounded-none"
            id={"execution-tab-" + value}
            aria-controls="execution-view-panel"
            role="tab"
            aria-selected={value === tab}
            tabIndex={value === tab ? 0 : -1}
            key={value}
            onClick={() => go({ tab: value })}
            onKeyDown={(event) => {
              if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
                event.preventDefault();
                const next =
                  tabs[
                    (tabs.indexOf(tab) + (event.key === "ArrowRight" ? 1 : 4)) %
                      5
                  ];
                go({ tab: next });
                requestAnimationFrame(() =>
                  document.getElementById("execution-tab-" + next)?.focus(),
                );
              }
            }}
          >
            {t("executionViewTab_" + value)}
          </Button>
        ))}
      </div>
      {["connecting", "reconnecting", "denied"].includes(connection) && (
        <p role="status" className="execution-stream-notice">
          {t("executionViewConnection_" + connection)}
        </p>
      )}
      {resync && <p role="status">{t("executionViewResync")}</p>}
      <ActionError error={overview.error ?? journal.error ?? control.error} />
      <div
        className="panel"
        id="execution-view-panel"
        role="tabpanel"
        aria-labelledby={"execution-tab-" + tab}
      >
        {tab === "overview" && (
          <>
            <div className="execution-view-kpis">
              {[
                ["executionViewComplete", current.progress.complete_targets],
                ["executionViewSuccess", current.progress.success_targets],
                ["executionViewFailed", current.progress.failed_targets],
                ["executionViewUnknown", current.progress.unknown_targets],
              ].map(([label, value]) => (
                <article key={label}>
                  <span>{t(String(label))}</span>
                  <strong>
                    {value} / {current.progress.total_targets}
                  </strong>
                </article>
              ))}
            </div>
            <Timeline overview={current} />
            <p>
              {t("executionViewSnapshotHint")} · {stamp(current.observed_at)}
            </p>
            <code>{current.execution.target_set_hash}</code>
          </>
        )}
        {tab === "targets" && (
          <Suspense fallback={<p role="status">{t("loadingRecords")}</p>}>
            <Targets
              projectId={projectId}
              executionId={executionId}
              search={search}
              onSearch={go}
            />
          </Suspense>
        )}
        {tab === "logs" && (
          <section>
            <h2>{t("executionViewLogs")}</h2>
            <p>{t("executionViewLifecycleNotice")}</p>
            <div className="execution-view-filters">
              <Label className="ui:grid ui:gap-1.5">
                {t("networkLogsLevel")}
                <NativeSelect
                  value={search.level ?? ""}
                  onChange={(e) => go({ level: e.target.value })}
                >
                  <NativeSelectOption value="">{t("fabricFilterAll")}</NativeSelectOption>
                  {["info", "warning", "error"].map((level) => (
                    <NativeSelectOption key={level}>{level}</NativeSelectOption>
                  ))}
                </NativeSelect>
              </Label>
              {search.logAttempt && (
                <Button variant="outline"
                  className="secondary-button"
                  onClick={() => go({ logAttempt: undefined })}
                >
                  {t("executionViewClearAttempt")} · {search.logAttempt}
                </Button>
              )}
              <span>{t("executionViewConnection_" + connection)}</span>
            </div>
            <p>{t("executionViewLogLimit")}</p>
            <div
              className="execution-journal"
              role="log"
              aria-label={t("executionViewLogs")}
              aria-live="polite"
            >
              {visible.map((event) => (
                <article
                  key={event.id}
                  data-level={event.level}
                  data-execution-id={event.execution_id}
                >
                  <time>{stamp(event.created_at)}</time>{" "}
                  <strong>
                    {event.level} · {event.type}
                  </strong>
                  <pre>{JSON.stringify(event.payload, null, 2)}</pre>
                </article>
              ))}
            </div>
          </section>
        )}
        {tab === "outputs" && (
          <Outputs
            projectId={projectId}
            executionId={executionId}
            page={search.outputPage ?? 1}
            onPage={(value) => go({ outputPage: value })}
          />
        )}
        {tab === "relations" && (
          <Relations projectId={projectId} executionId={executionId} />
        )}
      </div>
      {confirm && canControl && (
        <ActionDialog
          title={t(confirm === "cancel" ? "cancelExecution" : confirm)}
          description={`${project.name} · ${executionId} · ${current.execution.mode} · ${current.progress.total_targets} ${t("attemptTarget")}`}
          busy={control.busy}
          danger={confirm === "cancel"}
          confirm={t(confirm === "cancel" ? "cancelExecution" : confirm)}
          onClose={() => setConfirm(null)}
          onSubmit={(e) => void runControl(e)}
        >
          <ActionError error={control.error} />
          <p>{t("controlHint")}</p>
          <p>
            {t("executionViewControlVersion", {
              version: current.execution.state_version,
            })}
          </p>
          {control.error?.status === 409 && (
            <Button
              type="button"
              className="secondary-button"
              onClick={() =>
                void client.invalidateQueries({ queryKey: prefix })
              }
            >
              {t("retry")}
            </Button>
          )}
        </ActionDialog>
      )}
      {search.attempt && (
        <AttemptDrawer
          key={search.attempt}
          projectId={projectId}
          executionId={executionId}
          attempt={search.attempt}
          onRerun={canControl && current.execution.mode==='sim' && !active ? targetID=>{go({attempt:undefined});setRerun({mode:'targets',targetIDs:[targetID]})}:undefined}
          onClose={() => go({ attempt: undefined })}
          onLogs={() =>
            go({ attempt: undefined, tab: "logs", logAttempt: search.attempt })
          }
        />
      )}
    </section>
  );
}
