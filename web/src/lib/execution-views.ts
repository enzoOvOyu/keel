import { useEffect, useRef, useState } from "react";
import type { components } from "../generated/execution";
import { ApiError, request } from "./api";
export type ExecutionOverview = components["schemas"]["ExecutionOverview"];
export type ExecutionMatrix = components["schemas"]["ExecutionMatrix"];
export type JournalEntry = components["schemas"]["JournalEntry"];
export type JournalPage = components["schemas"]["JournalPage"];
export type ExecutionOutputs = components["schemas"]["ExecutionOutputs"];
export type ExecutionRelations = components["schemas"]["ExecutionRelations"];
export type ExecutionAttemptDetail =
  components["schemas"]["ExecutionAttemptDetail"];
export type ExecutionViewTab =
  | "overview"
  | "targets"
  | "logs"
  | "outputs"
  | "relations";
export type ExecutionViewSearch = {
  tab?: ExecutionViewTab;
  targetPage?: number;
  outputPage?: number;
  targetState?: string;
  q?: string;
  level?: string;
  attempt?: string;
  logAttempt?: string;
  cellSize?: number;
};
const base = (project: string, execution: string) =>
  `/projects/${encodeURIComponent(project)}/executions/${encodeURIComponent(execution)}`;
export const getOverview = (
  project: string,
  execution: string,
  signal: AbortSignal,
) =>
  request<ExecutionOverview>(base(project, execution) + "/overview", {
    signal,
  });
export const getMatrix = (
  project: string,
  execution: string,
  page: number,
  state: string,
  q: string,
  signal: AbortSignal,
) =>
  request<ExecutionMatrix>(
    base(project, execution) +
      `/matrix?${new URLSearchParams({ page: String(page), page_size: "500", state, q })}`,
    { signal },
  );
export const getJournal = (
  project: string,
  execution: string,
  after: number,
  signal: AbortSignal,
) =>
  request<JournalPage>(base(project, execution) + `/journal?after=${after}`, {
    signal,
  });
export const getOutputs = (
  project: string,
  execution: string,
  page: number,
  signal: AbortSignal,
) =>
  request<ExecutionOutputs>(
    base(project, execution) + `/outputs?page=${page}&page_size=20`,
    { signal },
  );
export const getRelations = (
  project: string,
  execution: string,
  signal: AbortSignal,
) =>
  request<ExecutionRelations>(base(project, execution) + "/relations", {
    signal,
  });
export const getAttempt = (
  project: string,
  execution: string,
  attempt: string,
  signal: AbortSignal,
) =>
  request<ExecutionAttemptDetail>(
    base(project, execution) + `/attempts/${encodeURIComponent(attempt)}`,
    { signal },
  );
export async function downloadOutput(
  project: string,
  execution: string,
  attempt: string,
) {
  const r = await fetch(
    "/api/v1" +
      base(project, execution) +
      `/outputs/${encodeURIComponent(attempt)}/download`,
    { credentials: "same-origin" },
  );
  if (!r.ok) throw new ApiError(r.status, "OUTPUT_DOWNLOAD_FAILED");
  const url = URL.createObjectURL(await r.blob()),
    link = document.createElement("a");
  link.href = url;
  link.download = "execution-output.json";
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function executionViewSearch(
  v: Record<string, unknown>,
): ExecutionViewSearch {
  const opaque = (id: unknown) =>
    typeof id === "string" && /^[a-f0-9]{32}$/.test(id) ? id : undefined;
  const page = (n: unknown) =>
    Number.isSafeInteger(Number(n)) && Number(n) > 0 && Number(n) <= 1000000
      ? Number(n)
      : undefined;
  return {
    tab: ["overview", "targets", "logs", "outputs", "relations"].includes(
      String(v.tab),
    )
      ? (v.tab as ExecutionViewTab)
      : "overview",
    targetPage: page(v.targetPage),
    outputPage: page(v.outputPage),
    targetState: [
      "pending",
      "running",
      "success",
      "failed",
      "cancelled",
      "unknown",
      "skipped",
    ].includes(String(v.targetState))
      ? String(v.targetState)
      : "",
    q: typeof v.q === "string" ? v.q.slice(0, 128) : "",
    level: ["info", "warning", "error"].includes(String(v.level))
      ? String(v.level)
      : "",
    attempt: opaque(v.attempt),
    logAttempt: opaque(v.logAttempt),
    cellSize: [8, 16, 24, 32].includes(Number(v.cellSize))
      ? Number(v.cellSize)
      : 24,
  };
}
export function mergeJournal(
  previous: JournalEntry[],
  incoming: JournalEntry[],
  limit = 500,
) {
  const byID = new Map(previous.map((e) => [e.id, e]));
  for (const entry of incoming) byID.set(entry.id, entry);
  return Array.from(byID.values())
    .sort((a, b) => a.sequence - b.sequence)
    .slice(-limit);
}
// Only the active Overview/Targets/Logs tab subscribes; all timers and EventSources
// are closed when the tab, execution, project or authorization scope changes.
export function useExecutionStream(
  project: string,
  execution: string,
  enabled: boolean,
  scope: string,
  initialCursor: number,
  callbacks: {
    onEvents: (events: JournalEntry[], replace: boolean) => void;
    onUpdate: () => void;
    onResync: () => void;
    onDenied: () => void;
  },
) {
  const [state, setState] = useState<
      "idle" | "connecting" | "live" | "reconnecting" | "denied"
    >("idle"),
    latest = useRef(callbacks);
  latest.current = callbacks;
  useEffect(() => {
    if (!enabled) {
      setState("idle");
      return;
    }
    let source: EventSource | null = null,
      closed = false,
      timer: ReturnType<typeof setTimeout> | undefined,
      flushTimer: ReturnType<typeof setTimeout> | undefined,
      cursor = initialCursor,
      delay = 1000,
      replaceNext = false,
      pending: JournalEntry[] = [];
    const flush = () => {
      flushTimer = undefined;
      if (closed) return;
      if (pending.length) {
        latest.current.onEvents(pending, replaceNext);
        pending = [];
        replaceNext = false;
      }
      latest.current.onUpdate();
    };
    const queue = (events: JournalEntry[]) => {
      pending = mergeJournal(pending, events);
      if (!flushTimer) flushTimer = setTimeout(flush, 150);
    };
    const reconnect = (wait: number) => {
      source?.close();
      source = null;
      clearTimeout(timer);
      if (!closed) timer = setTimeout(connect, wait);
    };
    function connect() {
      if (closed) return;
      const stream = new EventSource(
        "/api/v1" + base(project, execution) + `/stream?after=${cursor}`,
        { withCredentials: true },
      );
      source = stream;
      const current = () => !closed && source === stream;
      stream.onopen = () => {
        delay = 1000;
        setState("live");
      };
      stream.addEventListener("snapshot", (event) => {
        if (!current()) return;
        try {
          const page = JSON.parse((event as MessageEvent).data) as JournalPage;
          cursor = page.next_cursor;
          queue(page.items);
        } catch {
          source?.close();
          setState("reconnecting");
          reconnect(delay);
        }
      });
      stream.addEventListener("execution", (event) => {
        if (!current()) return;
        try {
          const entry = JSON.parse(
            (event as MessageEvent).data,
          ) as JournalEntry;
          cursor = entry.sequence;
          queue([entry]);
        } catch {
          source?.close();
          setState("reconnecting");
          reconnect(delay);
        }
      });
      stream.addEventListener("resync", () => {
        if (!current()) return;
        cursor = 0;
        pending = [];
        clearTimeout(flushTimer);
        flushTimer = undefined;
        replaceNext = true;
        latest.current.onResync();
        reconnect(150);
      });
      stream.addEventListener("rotate", () => {
        if (current()) reconnect(150);
      });
      stream.addEventListener("access_revoked", () => {
        if (!current()) return;
        source?.close();
        closed = true;
        setState("denied");
        latest.current.onDenied();
      });
      stream.addEventListener("unavailable", () => {
        if (!current()) return;
        setState("reconnecting");
        reconnect(delay);
        delay = Math.min(10000, delay * 2);
      });
      stream.onerror = () => {
        if (!current()) return;
        setState("reconnecting");
        reconnect(delay);
        delay = Math.min(10000, delay * 2);
      };
    }
    setState("connecting");
    connect();
    return () => {
      closed = true;
      source?.close();
      clearTimeout(timer);
      clearTimeout(flushTimer);
    };
    // A refreshed snapshot must not tear down a live stream or reset its cursor.
  }, [project, execution, enabled, scope]);
  return state;
}

export type ExecutionReport = components['schemas']['ExecutionReport'];
export const getExecutionReport = (project:string,execution:string,signal:AbortSignal) => request<ExecutionReport>(base(project,execution)+'/report',{signal});
