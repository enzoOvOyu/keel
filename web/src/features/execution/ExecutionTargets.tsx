import { useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useTranslation } from "react-i18next";
import { ActionError, Pagination } from "../../components/Management";
import { useSession, useExpireOnUnauthorized } from "../../lib/session";
import { getMatrix, type ExecutionViewSearch } from "../../lib/execution-views";
const symbols: Record<string, string> = {
  success: "✓",
  failed: "×",
  unknown: "?",
  running: "↻",
  cancelled: "−",
  skipped: "·",
  pending: "○",
  not_scheduled: "—",
};
export default function ExecutionTargets({
  projectId,
  executionId,
  search,
  onSearch,
}: {
  projectId: string;
  executionId: string;
  search: ExecutionViewSearch;
  onSearch: (value: Partial<ExecutionViewSearch>) => void;
}) {
  const session = useSession(),
    { t } = useTranslation(),
    host = useRef<HTMLDivElement>(null),
    page = search.targetPage ?? 1,
    size = search.cellSize ?? 24;
  const matrix = useQuery({
    queryKey: [
      "private",
      session.user.id,
      "project",
      projectId,
      "execution-view",
      executionId,
      "matrix",
      page,
      search.targetState,
      search.q,
    ],
    queryFn: ({ signal }) =>
      getMatrix(
        projectId,
        executionId,
        page,
        search.targetState ?? "",
        search.q ?? "",
        signal,
      ),
    retry: false,
  });
  useExpireOnUnauthorized(matrix.error);
  const items = matrix.data?.items ?? [],
    rowHeight = Math.max(28, size + 8),
    virtual = useVirtualizer({
      count: items.length,
      getScrollElement: () => host.current,
      estimateSize: () => rowHeight,
      overscan: 8,
      getItemKey: (index) => items[index]?.target.id ?? index,
    });
  const columns = `220px repeat(${matrix.data?.tasks.length ?? 0},${size}px)`;
  return (
    <section>
      <h2>{t("executionViewTargets")}</h2>
      <div className="execution-view-filters">
        <label>
          {t("executionViewTargetFilter")}
          <input
            value={search.q ?? ""}
            maxLength={128}
            onChange={(e) => onSearch({ q: e.target.value, targetPage: 1 })}
          />
        </label>
        <label>
          {t("attemptState")}
          <select
            value={search.targetState ?? ""}
            onChange={(e) =>
              onSearch({ targetState: e.target.value, targetPage: 1 })
            }
          >
            <option value="">{t("fabricFilterAll")}</option>
            {Object.keys(symbols)
              .filter((state) => state !== "not_scheduled")
              .map((state) => (
                <option key={state} value={state}>
                  {t("attempt_state_" + state)}
                </option>
              ))}
          </select>
        </label>
        <label>
          {t("executionViewZoom")}
          <select
            value={size}
            onChange={(e) => onSearch({ cellSize: Number(e.target.value) })}
          >
            {[8, 16, 24, 32].map((n) => (
              <option key={n} value={n}>
                {n}px
              </option>
            ))}
          </select>
        </label>
      </div>
      <ActionError error={matrix.error} />
      {matrix.isPending && <p role="status">{t("loadingRecords")}</p>}
      {matrix.data && (
        <>
          <p>
            {t("executionViewMatrixCount", { count: matrix.data.total })} ·{" "}
            {matrix.data.mode}
          </p>
          <div
            className="execution-matrix"
            ref={host}
            role="region"
            aria-label={t("executionViewTargets")}
            tabIndex={0}
          >
            <div
              className="execution-matrix-header"
              style={{ gridTemplateColumns: columns }}
            >
              <strong>{t("attemptTarget")}</strong>
              {matrix.data.tasks.map((task) => (
                <span title={task} key={task}>
                  {task}
                </span>
              ))}
            </div>
            <div
              style={{
                height: virtual.getTotalSize(),
                position: "relative",
                minWidth: 220 + matrix.data.tasks.length * size,
              }}
            >
              {virtual.getVirtualItems().map((row) => {
                const item = items[row.index];
                return (
                  <div
                    className="execution-matrix-row"
                    key={row.key}
                    data-index={row.index}
                    style={{
                      height: rowHeight,
                      transform: `translateY(${row.start}px)`,
                      gridTemplateColumns: columns,
                    }}
                  >
                    <strong
                      className="execution-matrix-name"
                      title={item.target.id}
                    >
                      {item.target.name || item.target.id}
                    </strong>
                    {item.cells.map((cell) => (
                      <button
                        type="button"
                        className="execution-matrix-cell"
                        data-state={cell.state}
                        key={cell.task_id}
                        disabled={!cell.attempt_id}
                        title={`${item.target.name} · ${cell.task_id} · ${t("attempt_state_" + cell.state)} · #${cell.attempt_no}`}
                        aria-label={`${item.target.name} ${cell.task_id} ${t("attempt_state_" + cell.state)}`}
                        style={{
                          height: size,
                          width: size,
                          fontSize: Math.max(8, size - 8),
                        }}
                        onClick={() => onSearch({ attempt: cell.attempt_id })}
                      >
                        {symbols[cell.state] ?? "?"}
                      </button>
                    ))}
                  </div>
                );
              })}
            </div>
          </div>
          <div className="execution-matrix-legend">
            {Object.entries(symbols).map(([state, symbol]) => (
              <button
                className="secondary-button"
                type="button"
                key={state}
                disabled={state === "not_scheduled"}
                aria-pressed={search.targetState === state}
                onClick={() =>
                  onSearch({
                    targetState: search.targetState === state ? "" : state,
                    targetPage: 1,
                  })
                }
              >
                <span data-state={state}>{symbol}</span>
                {state === "not_scheduled"
                  ? t("executionViewNotScheduled")
                  : t("attempt_state_" + state)}
              </button>
            ))}
          </div>
          <Pagination
            offset={(page - 1) * 500}
            limit={500}
            total={matrix.data.total}
            busy={matrix.isFetching}
            onChange={(offset) => onSearch({ targetPage: offset / 500 + 1 })}
          />
        </>
      )}
    </section>
  );
}
