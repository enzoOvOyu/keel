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
import { controlExecution } from "../../lib/execution";
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
      {data.data && (
        <>
          <p>{t("executionViewArtifactsUnavailable")}</p>
          <details open>
            <summary>{t("executionViewFrozenParameters")}</summary>
            <pre>{JSON.stringify(data.data.parameters, null, 2)}</pre>
          </details>
          <div className="table-scroll">
            <table className="data-table">
              <thead>
                <tr>
                  <th>{t("attemptTask")}</th>
                  <th>{t("attemptTarget")}</th>
                  <th>{t("attemptState")}</th>
                  <th>{t("executionViewReceipt")}</th>
                </tr>
              </thead>
              <tbody>
                {data.data.items.map((item) => (
                  <tr key={item.attempt_id}>
                    <td>{item.task_id}</td>
                    <td>
                      <code>{item.target_id}</code>
                    </td>
                    <td>{item.receipt.result}</td>
                    <td>
                      <button
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
                      </button>
                      <details>
                        <summary>{t("executionViewStructuredOutput")}</summary>
                        <pre>
                          {JSON.stringify(item.receipt.output, null, 2)}
                        </pre>
                        <code>{item.digest}</code>
                      </details>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
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
      {data.data && (
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
}: {
  projectId: string;
  executionId: string;
  attempt: string;
  onClose: () => void;
  onLogs: () => void;
}) {
  const session = useSession(),
    { t } = useTranslation(),
    ref = useRef<HTMLDialogElement>(null),
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
  useExpireOnUnauthorized(query.error);
  useEffect(() => {
    const prior =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    ref.current?.showModal();
    return () => {
      ref.current?.close();
      if (prior?.isConnected) prior.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="execution-attempt-drawer"
      aria-labelledby="execution-attempt-heading"
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
    >
      <div className="execution-view-toolbar">
        <h2 id="execution-attempt-heading">
          {t("executionViewAttemptDetails")}
        </h2>
        <button type="button" className="secondary-button" onClick={onClose}>
          {t("executionViewClose")}
        </button>
      </div>
      <ActionError error={query.error} />
      {query.isPending && <p role="status">{t("loadingRecords")}</p>}
      {query.data && (
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
          </dl>
          <button type="button" className="secondary-button" onClick={onLogs}>
            {t("executionViewFocusLogs")}
          </button>
          <h3>{t("executionViewReceipt")}</h3>
          <pre>
            {query.data.attempt.receipt
              ? JSON.stringify(query.data.attempt.receipt, null, 2)
              : t("executionViewNoReceipt")}
          </pre>
        </>
      )}
    </dialog>
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
  const current = overview.data,
    active = current ? !terminal.includes(current.execution.state) : false;
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
  const canControl = Boolean(
    current &&
      (current.execution.mode === "sim"
        ? session.user.is_admin ||
          ["Admin", "SRE", "DE", "DL"].includes(project.role)
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
        <ActionError error={overview.error} />
        <button className="secondary-button" onClick={onClose}>
          {t("backToList")}
        </button>
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
        <button className="secondary-button" onClick={onClose}>
          {t("backToList")}
        </button>
      </header>
      <p className="impact-notice">
        {t(
          current.execution.mode === "fabric"
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
            <button
              className={
                action === "cancel" ? "danger-text" : "secondary-button"
              }
              key={action}
              disabled={!active || control.busy}
              onClick={() => setConfirm(action)}
            >
              {t(action === "cancel" ? "cancelExecution" : action)}
            </button>
          ))}
        </div>
      )}
      <div
        role="tablist"
        className="execution-view-tabs"
        aria-label={t("executionViewTabs")}
      >
        {tabs.map((value) => (
          <button
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
          </button>
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
              <label>
                {t("networkLogsLevel")}
                <select
                  value={search.level ?? ""}
                  onChange={(e) => go({ level: e.target.value })}
                >
                  <option value="">{t("fabricFilterAll")}</option>
                  {["info", "warning", "error"].map((level) => (
                    <option key={level}>{level}</option>
                  ))}
                </select>
              </label>
              {search.logAttempt && (
                <button
                  className="secondary-button"
                  onClick={() => go({ logAttempt: undefined })}
                >
                  {t("executionViewClearAttempt")} · {search.logAttempt}
                </button>
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
      {confirm && (
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
            <button
              type="button"
              className="secondary-button"
              onClick={() =>
                void client.invalidateQueries({ queryKey: prefix })
              }
            >
              {t("retry")}
            </button>
          )}
        </ActionDialog>
      )}
      {search.attempt && (
        <AttemptDrawer
          key={search.attempt}
          projectId={projectId}
          executionId={executionId}
          attempt={search.attempt}
          onClose={() => go({ attempt: undefined })}
          onLogs={() =>
            go({ attempt: undefined, tab: "logs", logAttempt: search.attempt })
          }
        />
      )}
    </section>
  );
}
