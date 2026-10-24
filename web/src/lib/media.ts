import { request } from "./api";
import type { components } from "../generated/api";

export type MediaItemSummary = components["schemas"]["MediaItemSummary"];

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
  projectId: string,
  page: number,
  signal: AbortSignal,
) =>
  read<components["schemas"]["MediaPage"]>(
    `/projects/${encodeURIComponent(projectId)}/media?${pageQuery(page)}`,
    signal,
  );
export const registerMedia = (
  projectId: string,
  body: components["schemas"]["RegisterMediaInput"],
  csrf: string,
  key: string,
) =>
  write<MediaItemSummary>(
    `/projects/${encodeURIComponent(projectId)}/media`,
    "POST",
    body,
    csrf,
    key,
  );
export const transitionMedia = (
  projectId: string,
  mediaId: string,
  state: string,
  csrf: string,
) =>
  write<MediaItemSummary>(
    `/projects/${encodeURIComponent(projectId)}/media/${encodeURIComponent(mediaId)}/transition`,
    "POST",
    { state },
    csrf,
  );

export const ingestMedia = (
  projectId: string,
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
    `/projects/${encodeURIComponent(projectId)}/media/upload`,
    { method: "POST", headers: { "X-CSRF-Token": csrf }, body: form },
  );
};
export const attachMedia = (
  projectId: string,
  id: string,
  file: File,
  csrf: string,
) =>
  request<MediaItemSummary>(
    `/projects/${encodeURIComponent(projectId)}/media/${encodeURIComponent(id)}/content`,
    {
      method: "PUT",
      headers: {
        "X-CSRF-Token": csrf,
        "Content-Type": "application/octet-stream",
      },
      body: file,
    },
  );
export const mediaDownloadURL = (projectId: string, id: string) =>
  `/api/v1/projects/${encodeURIComponent(projectId)}/media/${encodeURIComponent(id)}/content`;
