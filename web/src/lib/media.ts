import { request } from "./api";
import type { components } from "../generated/api";

export type MediaItemSummary = components["schemas"]["MediaItemSummary"];

const mediaBase = (projectId: string | null) => projectId === null ? '/admin/media' : `/projects/${encodeURIComponent(projectId)}/media`;
const pageQuery = (page: number) =>
  new URLSearchParams({ page: String(page), page_size: "20" }).toString();
const read = <T>(path: string, signal: AbortSignal) =>
  fetch(`/api/v1${path}`, {
    signal,
    credentials: "same-origin",
    cache: "no-store",
    headers: { Accept: "application/json" },
  }).then(async (response) => {
    if (!response.ok)
      throw Object.assign(new Error(`HTTP ${response.status}`), {
        status: response.status,
      });
    return (await response.json()) as T;
  });
const write = <T>(
  path: string,
  method: string,
  body: unknown,
  csrf: string,
  key?: string,
) =>
  fetch(`/api/v1${path}`, {
    method,
    credentials: "same-origin",
    cache: "no-store",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      "X-CSRF-Token": csrf,
      ...(key ? { "Idempotency-Key": key } : {}),
    },
    body: JSON.stringify(body),
  }).then(async (response) => {
    const value = await response.json().catch(() => null);
    if (!response.ok) {
      const problem = value as { code?: string } | null;
      throw Object.assign(
        new Error(problem?.code ?? `HTTP ${response.status}`),
        { status: response.status, code: problem?.code },
      );
    }
    return value as T;
  });

export const listMedia = (
  projectId: string | null,
  page: number,
  signal: AbortSignal,
) =>
  read<components["schemas"]["MediaPage"]>(
    `${mediaBase(projectId)}?${pageQuery(page)}`,
    signal,
  );
export const registerMedia = (
  projectId: string | null,
  body: components["schemas"]["RegisterMediaInput"],
  csrf: string,
  key: string,
) =>
  write<MediaItemSummary>(
    mediaBase(projectId),
    "POST",
    body,
    csrf,
    key,
  );
export type MediaTransitionTarget = components["schemas"]["MediaTransitionInput"]["state"];
export const transitionMedia = (
  projectId: string | null,
  item: MediaItemSummary,
  state: MediaTransitionTarget,
  csrf: string,
) => request<MediaItemSummary>(
  `${mediaBase(projectId)}/${encodeURIComponent(item.id)}/transition`,
  { method: "POST", headers: { "X-CSRF-Token": csrf }, body: JSON.stringify({ state, expected_updated_at: item.updated_at } satisfies components["schemas"]["MediaTransitionInput"]) },
);

export const ingestMedia = (
  projectId: string | null,
  metadata: Omit<
    components["schemas"]["RegisterMediaInput"],
    "sha256" | "size"
  >,
  file: File,
  csrf: string,
) => {
  const form = new FormData();
  form.append("metadata", JSON.stringify(metadata));
  form.append("file", file);
  return request<MediaItemSummary>(
    `${mediaBase(projectId)}/upload`,
    { method: "POST", headers: { "X-CSRF-Token": csrf }, body: form },
  );
};
export const attachMedia = (
  projectId: string | null,
  id: string,
  file: File,
  csrf: string,
) =>
  request<MediaItemSummary>(
    `${mediaBase(projectId)}/${encodeURIComponent(id)}/content`,
    {
      method: "PUT",
      headers: {
        "X-CSRF-Token": csrf,
        "Content-Type": "application/octet-stream",
      },
      body: file,
    },
  );
export const mediaDownloadURL = (projectId: string | null, id: string) =>
  `/api/v1${mediaBase(projectId)}/${encodeURIComponent(id)}/content`;

export const exportMediaManifest = (projectId: string | null, id: string) =>
  request<components['schemas']['SignedMediaManifest']>(`${mediaBase(projectId)}/${encodeURIComponent(id)}/manifest`);

export const fetchMediaSource = (projectId: string | null, id: string, csrf: string) =>
  request<MediaItemSummary>(`${mediaBase(projectId)}/${encodeURIComponent(id)}/fetch`, { method: 'POST', headers: { 'X-CSRF-Token': csrf } });
