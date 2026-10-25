import { MediaBootProfileEditor, MediaBootProfileSummary, MediaBootPlanDialog, type MediaOSBootProfile } from './MediaBootProfile';
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table"
import { MediaQuarantineDialog } from './MediaQuarantineDialog';
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select"
import type { MediaListFilter } from '../../lib/media';
import { MediaCompatibilityImport, MediaCompatibilitySummary } from './MediaCompatibility';
import type { MediaCompatibility } from '../../lib/media';
import { MediaSourceDialog } from './MediaSourceDialog';
import { MediaManifestDialog } from './MediaManifestDialog';
import { MediaDependencyPicker, MediaDependencySummary, type MediaDependencyChoice } from './MediaDependencyPicker';
import { Workspace } from "../../components/Workspace";
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  ProjectWorkspace,
  useProject,
} from "../../components/ProjectWorkspace";
import {
  ActionDialog,
  ActionError,
  Pagination,
  PermissionDenied,
  SuccessNotice,
} from "../../components/Management";
import { useAction } from "../../lib/management";
import {
  listMedia,
  registerMedia,
  transitionMedia,
  ingestMedia,
  attachMedia,
  mediaDownloadURL,
  type MediaItemSummary,
  type MediaTransitionTarget,
} from "../../lib/media";
import { useExpireOnUnauthorized, useSession } from "../../lib/session";

const types = [
  "driver",
  "firmware",
  "os",
  "nccl",
  "tool",
  "config",
  "ztp_script",
] as const;
const nextStates: Record<string, MediaTransitionTarget[]> = {
  uploaded: ["verified", "retired"],
  verified: ["approved", "retired"],
  approved: ["retired"],
  retired: [],
};

function useIdempotencyKey() {
  const ref = useRef<{ payload: string; key: string } | null>(null);
  return (payload: string) => {
    if (!ref.current || ref.current.payload !== payload)
      ref.current = { payload, key: crypto.randomUUID() };
    return ref.current.key;
  };
}
function RegisterMediaDialog({
  projectId,
  upload = false,
  onClose,
  onDone,
}: {
  upload?: boolean;
  projectId: string | null;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const { t } = useTranslation(),
    session = useSession(),
    action = useAction(),
    keyFor = useIdempotencyKey();
  const [file, setFile] = useState<File | null>(null);
  const [compatibility, setCompatibility] = useState<MediaCompatibility>();
  const [matrixBlocked, setMatrixBlocked] = useState(false);
  const [dependencies, setDependencies] = useState<MediaDependencyChoice[]>([]);
  const [bootProfile, setBootProfile] = useState<MediaOSBootProfile>();
  const [type, setType] = useState<(typeof types)[number]>("driver"),
    [name, setName] = useState(""),
    [version, setVersion] = useState("");
  const [sha256, setSha256] = useState(""),
    [size, setSize] = useState("0"),
    [arch, setArch] = useState<"amd64" | "arm64" | "noarch">("amd64");
  const [osScope, setOsScope] = useState(""),
    [hwScope, setHwScope] = useState(""),
    [manifest, setManifest] = useState(""),
    [validation, setValidation] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (matrixBlocked) return;
    const trimmedName = name.trim(),
      trimmedVersion = version.trim();
    if (
      !trimmedName ||
      trimmedName.length > 120 ||
      !trimmedVersion ||
      trimmedVersion.length > 120
    ) {
      setValidation(t("invalidFields"));
      return;
    }
    if (!upload && !/^[a-f0-9]{64}$/.test(sha256)) {
      setValidation(t("mediaSha256") + ": 64");
      return;
    }
    const bytes = Number(size);
    if (!Number.isSafeInteger(bytes) || bytes < 0) {
      setValidation(t("mediaSize"));
      return;
    }
    if (upload && (!file || file.size > 1073741824)) {
      setValidation(t("mediaFileLimit"));
      return;
    }
    if (dependencies.some(item => item.arch !== 'noarch' && item.arch !== arch)) { setValidation(t('mediaDependencyArchMismatch')); return; }
    if (type === 'os' && bootProfile) {
      const entries = [bootProfile.kernel_media_id, bootProfile.initrd_media_id, bootProfile.template_media_id];
      if (arch === 'noarch' || new Set(entries).size !== 3 || entries.some(id => !id || !dependencies.some(item => item.id === id))) { setValidation(t('mediaBootInvalid')); return; }
    }
    setValidation("");
    const body = {
      type,
      name: trimmedName,
      version: trimmedVersion,
      sha256,
      size: bytes,
      arch,
      os_scope: osScope.trim(),
      hw_scope: hwScope.trim(),
      manifest_ref: manifest.trim(),
      deps: dependencies.length || compatibility || (type === "os" && bootProfile) ? { ...(type === "os" && bootProfile ? { os_boot: bootProfile } : {}), ...(dependencies.length ? { artifacts: dependencies.map(item => ({ media_id: item.id, sha256: item.sha256 })) } : {}), ...(compatibility ? { compatibility } : {}) } : undefined,
    };
    const result = await action.run(() =>
      upload
        ? ingestMedia(projectId, body, file!, session.csrf_token)
        : registerMedia(
            projectId,
            body,
            session.csrf_token,
            keyFor(JSON.stringify(body)),
          ),
    );
    if (result.ok) await onDone();
  }
  return (
    <ActionDialog
      title={t(upload ? "mediaUpload" : "mediaCreate")}
      description={t(projectId === null ? "mediaGlobalRegistration" : upload ? "mediaUploadDetail" : "mediaCreateDetail")}
      busy={action.busy}
      submitDisabled={matrixBlocked}
      confirm={t(action.busy ? "saving" : "mediaCreate")}
      onSubmit={(event) => void submit(event)}
      onClose={onClose}
    >
      {action.error?.code?.startsWith("MEDIA_DEPENDENCY_") ? <p role="alert" className="field-error">{t("mediaDependencyBlocked")}</p> : <ActionError error={action.error} />}
      {validation && (
        <p className="field-error" role="alert">
          {validation}
        </p>
      )}
      <div className="field">
        <Label htmlFor="media-type">{t("mediaType")}</Label>
        <NativeSelect className="ui:min-h-11"
          id="media-type"
          value={type}
          onChange={(event) => setType(event.target.value as typeof type)}
          disabled={action.busy}
        >
          {types.map((value) => (
            <NativeSelectOption key={value} value={value}>
              {t("media_type_" + value)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>
      <div className="field">
        <Label htmlFor="media-name">{t("mediaName")}</Label>
        <Input className="ui:h-11"
          id="media-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          maxLength={120}
          required
          disabled={action.busy}
        />
      </div>
      <div className="field">
        <Label htmlFor="media-version">{t("mediaVersion")}</Label>
        <Input className="ui:h-11"
          id="media-version"
          value={version}
          onChange={(event) => setVersion(event.target.value)}
          maxLength={120}
          required
          disabled={action.busy}
        />
      </div>
      {upload ? (
        <div className="field">
          <Label htmlFor="media-file">{t("mediaFile")}</Label>
          <Input
            id="media-file"
            type="file"
            required
            disabled={action.busy}
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
          />
          <p>{t("mediaFileLimit")}</p>
        </div>
      ) : (
        <>
          <div className="field">
            <Label htmlFor="media-sha">{t("mediaSha256")}</Label>
            <Input
              id="media-sha"
              value={sha256}
              onChange={(event) => setSha256(event.target.value)}
              maxLength={64}
              required
              disabled={action.busy}
              className="mono-value ui:h-11"
            />
          </div>
          <div className="field">
            <Label htmlFor="media-size">{t("mediaSize")}</Label>
            <Input className="ui:h-11"
              id="media-size"
              type="number"
              min={0}
              value={size}
              onChange={(event) => setSize(event.target.value)}
              required
              disabled={action.busy}
            />
          </div>
        </>
      )}
      <div className="field">
        <Label htmlFor="media-arch">{t("mediaArch")}</Label>
        <NativeSelect className="ui:min-h-11"
          id="media-arch"
          value={arch}
          onChange={(event) => setArch(event.target.value as typeof arch)}
          disabled={action.busy}
        >
          <NativeSelectOption value="amd64">amd64</NativeSelectOption>
          <NativeSelectOption value="arm64">arm64</NativeSelectOption>
          <NativeSelectOption value="noarch">noarch</NativeSelectOption>
        </NativeSelect>
        <span className="field-hint">{t("mediaArchHint")}</span>
      </div>
      <div className="field">
        <Label htmlFor="media-os">{t("mediaOsScope")}</Label>
        <Input className="ui:h-11"
          id="media-os"
          value={osScope}
          onChange={(event) => setOsScope(event.target.value)}
          maxLength={120}
          disabled={action.busy}
        />
      </div>
      <div className="field">
        <Label htmlFor="media-hw">{t("mediaHwScope")}</Label>
        <Input className="ui:h-11"
          id="media-hw"
          value={hwScope}
          onChange={(event) => setHwScope(event.target.value)}
          maxLength={120}
          disabled={action.busy}
        />
      </div>
      <MediaCompatibilityImport projectId={projectId} disabled={action.busy} value={compatibility} onChange={setCompatibility} onBlocked={setMatrixBlocked} />
      <MediaDependencyPicker projectId={projectId} arch={arch} selected={dependencies} onChange={setDependencies} disabled={action.busy} />
      {type === "os" && <MediaBootProfileEditor value={bootProfile} onChange={setBootProfile} dependencies={dependencies} arch={arch} disabled={action.busy} />}
      {!upload && (
        <div className="field">
          <Label htmlFor="media-manifest">{t("mediaManifest")}</Label>
          <Input
            id="media-manifest"
            value={manifest}
            onChange={(event) => setManifest(event.target.value)}
            maxLength={200}
            disabled={action.busy}
            className="mono-value ui:h-11"
          />
          <span className="field-hint">{t("mediaManifestHint")}</span>
        </div>
      )}
    </ActionDialog>
  );
}
function AttachMediaDialog({
  projectId,
  item,
  onClose,
  onDone,
}: {
  projectId: string | null;
  item: MediaItemSummary;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const { t } = useTranslation(),
    session = useSession(),
    action = useAction(),
    [file, setFile] = useState<File | null>(null);
  return (
    <ActionDialog
      title={t("mediaAttach")}
      description={`${item.name} · ${item.version} · ${item.sha256}`}
      busy={action.busy}
      confirm={t("mediaAttach")}
      submitDisabled={!file || file.size > 1073741824}
      onClose={onClose}
      onSubmit={(event) => {
        event.preventDefault();
        if (file)
          void action
            .run(() =>
              attachMedia(projectId, item.id, file, session.csrf_token),
            )
            .then(async (result) => {
              if (result.ok) await onDone();
            });
      }}
    >
      <ActionError error={action.error} />
      <Label htmlFor="media-attach-file">{t("mediaFile")}</Label>
      <Input
        id="media-attach-file"
        type="file"
        required
        disabled={action.busy}
        onChange={(event) => setFile(event.target.files?.[0] ?? null)}
      />
      <p>{t("mediaAttachDetail")}</p>
    </ActionDialog>
  );
}
function TransitionDialog({
  projectId,
  item,
  target,
  onClose,
  onDone,
}: {
  projectId: string | null;
  item: MediaItemSummary;
  target: MediaTransitionTarget;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const { t } = useTranslation(),
    session = useSession(),
    action = useAction();
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = await action.run(() =>
      transitionMedia(projectId, item, target, session.csrf_token),
    );
    if (result.ok) await onDone();
  }
  return (
    <ActionDialog
      title={t("mediaNext" + target.charAt(0).toUpperCase() + target.slice(1))}
      description={t(projectId === null ? "mediaGlobalTransition" : "mediaTransitionDetail")}
      busy={action.busy}
      confirm={t("confirmChange")}
      danger={target === "retired"}
      onSubmit={(event) => void submit(event)}
      onClose={onClose}
    >
      {action.error?.code === "MEDIA_ARTIFACT_QUARANTINED" ? <p role="alert" className="field-error">{t("mediaQuarantined")}</p> : action.error?.code === "MEDIA_CONFLICT" ? <p role="alert" className="field-error">{t("mediaConflict")}</p> : action.error?.code?.startsWith("MEDIA_DEPENDENCY_") ? <p role="alert" className="field-error">{t("mediaDependencyBlocked")}</p> : <ActionError error={action.error} />}
      <dl className="action-target">
        <dt>{t("mediaName")}</dt>
        <dd>
          {item.name} · {item.version}
        </dd>
        <dt>{t("mediaState")}</dt>
        <dd>{t("media_state_" + item.state)}</dd>
      </dl>
      <MediaDependencySummary refs={item.deps.artifacts} />
      <MediaBootProfileSummary profile={item.deps.os_boot} />
      <MediaCompatibilitySummary matrix={item.deps.compatibility} />
      {target === "retired" && (
        <p className="impact-notice">{t("mediaRetireImpact")}</p>
      )}
      {target === "approved" && (
        <p className="impact-notice">{t("mediaSelfApproval")}</p>
      )}
      {target === "verified" && !item.artifact_stored && (
        <p className="impact-notice">{t("mediaManifestMissing")}</p>
      )}
    </ActionDialog>
  );
}
function MediaContent({ projectId, projectName, projectRole }: { projectId: string | null; projectName: string; projectRole?: string }) {
  const { t, i18n } = useTranslation(),
    session = useSession(),
    client = useQueryClient();
  const [filters, setFilters] = useState<MediaListFilter>({});
  const [search, setSearch] = useState("");
  function changeFilters(next: MediaListFilter) { setPage(1); setFilters(next); }
  const [quarantineView, setQuarantineView] = useState<MediaItemSummary | null>(null);
  const [matrixView, setMatrixView] = useState<MediaItemSummary | null>(null);
  const [sourceFetch, setSourceFetch] = useState<MediaItemSummary | null>(null);
  const [bootView, setBootView] = useState<MediaItemSummary | null>(null);
  const [manifestExport, setManifestExport] = useState<MediaItemSummary | null>(null);
  const [upload, setUpload] = useState(false),
    [attach, setAttach] = useState<MediaItemSummary | null>(null);
  const [page, setPage] = useState(1),
    [create, setCreate] = useState(false),
    [transition, setTransition] = useState<{
      item: MediaItemSummary;
      target: MediaTransitionTarget;
    } | null>(null),
    [success, setSuccess] = useState("");
  const allowed = projectId === null ? session.user.is_admin : session.permissions.includes("media:read");
  const manage =
    (session.user.is_admin || projectId !== null && ["Admin", "SRE"].includes(projectRole ?? "")) &&
    (projectId === null || session.permissions.includes("media:manage"));
  const media = useQuery({
    queryKey: [
      "private",
      session.user.id,
      projectId === null ? "global" : "project",
      projectId,
      "media",
      filters,
      page,
    ],
    queryFn: ({ signal }) => listMedia(projectId, page, signal, filters),
    enabled: allowed,
  });
  useExpireOnUnauthorized(media.error);
  useEffect(() => {
    document.title = `${t(projectId === null ? "mediaGlobalTitle" : "mediaTitle")} · ${projectName}`;
  }, [t, projectName, projectId]);
  async function done(message: string) {
    setCreate(false);
    setUpload(false);
    setAttach(null);
    setSourceFetch(null);
    setTransition(null);
    setSuccess(message);
    await client.invalidateQueries({
      queryKey: projectId === null ? ["private", session.user.id] : ["private", session.user.id, "project", projectId],
    });
  }
  if (!allowed) return <PermissionDenied />;
  const format = (value: string) =>
    new Intl.DateTimeFormat(i18n.language === "en" ? "en-US" : "zh-CN", {
      dateStyle: "medium",
    }).format(new Date(value));
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="section-eyebrow">{projectName}</p>
          <h1>{t(projectId === null ? "mediaGlobalTitle" : "mediaTitle")}</h1>
          <p>{t(projectId === null ? "mediaGlobalIntro" : "mediaIntro")}</p>
        </div>
        {manage && (
          <Button variant="outline" onClick={() => setUpload(true)}>
            {t("mediaUpload")}
          </Button>
        )}
        {manage && (
          <Button variant="default"
            className="primary-button ui:min-h-11"
            onClick={() => {
              setSuccess("");
              setCreate(true);
            }}
          >
            {t("mediaCreate")}
          </Button>
        )}
      </div>
      <form role="search" aria-label={t("mediaFilters")} className="ui:grid ui:gap-3 ui:mb-4 ui:sm:grid-cols-2 ui:lg:grid-cols-5" onSubmit={event => { event.preventDefault(); changeFilters({ ...filters, search: search.trim() }); }}>
        <div className="field"><Label htmlFor="media-search">{t("mediaSearch")}</Label><Input id="media-search" value={search} maxLength={120} onChange={event => setSearch(event.target.value)} /></div>
        <div className="field"><Label htmlFor="media-filter-type">{t("mediaType")}</Label><NativeSelect id="media-filter-type" value={filters.type ?? ""} onChange={event => changeFilters({ ...filters, type: event.target.value as MediaListFilter['type'] })}><NativeSelectOption value="">{t("mediaFilterAll")}</NativeSelectOption>{types.map(type => <NativeSelectOption value={type} key={type}>{t("media_type_"+type)}</NativeSelectOption>)}</NativeSelect></div>
        <div className="field"><Label htmlFor="media-filter-state">{t("mediaState")}</Label><NativeSelect id="media-filter-state" value={filters.state ?? ""} onChange={event => changeFilters({ ...filters, state: event.target.value as MediaListFilter['state'] })}><NativeSelectOption value="">{t("mediaFilterAll")}</NativeSelectOption>{Object.keys(nextStates).map(state => <NativeSelectOption value={state} key={state}>{t("media_state_"+state)}</NativeSelectOption>)}</NativeSelect></div>
        <div className="field"><Label htmlFor="media-filter-arch">{t("mediaCompatibleArch")}</Label><NativeSelect id="media-filter-arch" value={filters.compatible_arch ?? ""} onChange={event => changeFilters({ ...filters, compatible_arch: event.target.value as MediaListFilter['compatible_arch'] })}><NativeSelectOption value="">{t("mediaFilterAll")}</NativeSelectOption>{['amd64','arm64','noarch'].map(arch => <NativeSelectOption value={arch} key={arch}>{arch}</NativeSelectOption>)}</NativeSelect></div>
        <div className="ui:flex ui:items-end ui:gap-2 ui:pb-3"><Button type="submit">{t("mediaFilterApply")}</Button><Button type="button" variant="outline" onClick={() => { setSearch(""); changeFilters({}); }}>{t("mediaFilterClear")}</Button></div>
      </form>
      {success && <SuccessNotice>{success}</SuccessNotice>}
      {quarantineView && <MediaQuarantineDialog projectId={projectId} item={quarantineView} onClose={() => setQuarantineView(null)} />}
      {matrixView && <ActionDialog title={t("mediaCompatibility")} description={`${matrixView.name} · ${matrixView.version}`} busy={false} confirm={t("mediaMatrixClose")} onClose={() => setMatrixView(null)} onSubmit={event => { event.preventDefault(); setMatrixView(null); }}><MediaCompatibilitySummary matrix={matrixView.deps.compatibility} /></ActionDialog>}
      {sourceFetch && <MediaSourceDialog projectId={projectId} item={sourceFetch} onClose={() => setSourceFetch(null)} onDone={() => done(t("mediaSourceFetched"))} />}
      {bootView && <MediaBootPlanDialog projectId={projectId} item={bootView} onClose={() => setBootView(null)} />}
      {manifestExport && <MediaManifestDialog projectId={projectId} item={manifestExport} onClose={() => setManifestExport(null)} onDone={() => { setManifestExport(null); setSuccess(t("mediaManifestDownloaded")); }} />}
      <section className="management-panel" aria-busy={media.isFetching}>
        <ActionError error={media.error} />
        {media.isPending ? (
          <p className="table-loading" role="status">
            {t("loadingRecords")}
          </p>
        ) : (
          media.data && (
            <>
              <div
                className="table-scroll"
                tabIndex={0}
                role="region"
                aria-label={t(projectId === null ? "mediaGlobalTitle" : "mediaTitle")}
              >
                <Table scrollContainer={false} className="data-table">
                  <TableHeader>
                    <TableRow>
                      <TableHead className="ui:align-top ui:p-4" scope="col">{t("mediaName")}</TableHead>
                      <TableHead className="ui:align-top ui:p-4" scope="col">{t("mediaType")}</TableHead>
                      <TableHead className="ui:align-top ui:p-4" scope="col">{t("mediaArch")}</TableHead>
                      <TableHead className="ui:align-top ui:p-4" scope="col">{t("mediaState")}</TableHead>
                      <TableHead className="ui:align-top ui:p-4" scope="col">{t("createdAt")}</TableHead>
                      <TableHead className="ui:align-top ui:p-4" scope="col">{t("actions")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {media.data.items.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell className="ui:whitespace-normal ui:align-top ui:p-4">
                          <strong>{item.name}</strong>
                              {!!item.deps.artifacts?.length && <span className="cell-subtitle">{t("mediaDependencyCount", { count: item.deps.artifacts.length })}</span>}
                          {item.scope_key === "global" && <span className="cell-subtitle">{t(projectId === null ? "mediaGlobalScope" : "mediaGlobalInherited")}</span>}
                          <span className="cell-subtitle mono-value">
                            {item.version} · {item.sha256.slice(0, 12)}…
                          </span>
                        </TableCell>
                        <TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{t("media_type_" + item.type)}</TableCell>
                        <TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{item.arch}</TableCell>
                        <TableCell className="ui:whitespace-normal ui:align-top ui:p-4">
                          <span
                            className={`status-label ${item.state === "approved" ? "enabled" : item.state === "retired" ? "disabled" : ""}`}
                          >
                            {t("media_state_" + item.state)}
                          </span>
                          {item.artifact_quarantined && <span className="cell-subtitle field-error">{t("mediaQuarantined")}</span>}
                          <span className="cell-subtitle">
                            {t(
                              item.artifact_stored
                                ? "mediaBytesStored"
                                : "mediaBytesMissing",
                            )}
                          </span>
                        </TableCell>
                        <TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{format(item.created_at)}</TableCell>
                        <TableCell className="ui:whitespace-normal ui:align-top ui:p-4">
                          <div className="table-actions">
                            {item.artifact_quarantined && <Button variant="ghost" size="sm" onClick={() => setQuarantineView(item)}>{t("mediaQuarantineView")}</Button>}
                            {item.deps.os_boot && item.state === "approved" && <Button variant="outline" size="sm" onClick={() => setBootView(item)}>{t("mediaBootResolve")}</Button>}
                            {item.deps.compatibility && <Button variant="ghost" size="sm" onClick={() => setMatrixView(item)}>{t("mediaMatrixView")}</Button>}
                            {manage && item.scope_key === (projectId === null ? "global" : "project:" + projectId) &&
                              nextStates[item.state].map((target) => (
                                <Button variant="outline"
                                  key={target}
                                  disabled={
                                    (target === "verified" && !item.artifact_stored) || (target !== "retired" && item.artifact_quarantined)
                                  }
                                  className={[target === "retired" ? "danger-text" : "","ui:min-h-11"].filter(Boolean).join(" ")}
                                  onClick={() => {
                                    setSuccess("");
                                    setTransition({ item, target });
                                  }}
                                >
                                  {t(
                                    "mediaNext" +
                                      target.charAt(0).toUpperCase() +
                                      target.slice(1),
                                  )}
                                </Button>
                              ))}
                            {manage && item.scope_key === (projectId === null ? "global" : "project:" + projectId) &&
                              item.state === "uploaded" &&
                              !item.artifact_stored &&
                              item.uploaded_by === session.user.id && (
                                <Button
                                  variant="outline"
                                  onClick={() => setAttach(item)}
                                >
                                  {t("mediaAttach")}
                                </Button>
                              )}
                            {manage && item.scope_key === (projectId === null ? "global" : "project:" + projectId) && item.state === "uploaded" && !item.artifact_stored && item.uploaded_by === session.user.id && /^https?:\/\//i.test(item.manifest_ref) && <Button variant="outline" onClick={() => { setSuccess(""); setSourceFetch(item); }}>{t("mediaSourceFetch")}</Button>}
                            {item.state === "approved" &&
                              item.artifact_stored && !item.artifact_quarantined && (
                                <><a href={mediaDownloadURL(projectId, item.id)}>
                                  {t("mediaDownload")}
                                </a><Button variant="ghost" size="sm" onClick={() => setManifestExport(item)}>{t("mediaManifestExport")}</Button></>
                              )}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                {!media.data.items.length && (
                  <p className="table-loading">{t("noRecords")}</p>
                )}
              </div>
              <Pagination
                offset={(page - 1) * media.data.page_size}
                limit={media.data.page_size}
                total={media.data.total}
                busy={media.isFetching}
                onChange={(offset) =>
                  setPage(offset / media.data.page_size + 1)
                }
              />
            </>
          )
        )}
      </section>
      {upload && (
        <RegisterMediaDialog
          upload
          projectId={projectId}
          onClose={() => setUpload(false)}
          onDone={() => done(t("mediaCreated"))}
        />
      )}
      {attach && (
        <AttachMediaDialog
          projectId={projectId}
          item={attach}
          onClose={() => setAttach(null)}
          onDone={() => done(t("mediaUploaded"))}
        />
      )}
      {create && (
        <RegisterMediaDialog
          projectId={projectId}
          onClose={() => setCreate(false)}
          onDone={() => done(t("mediaCreated"))}
        />
      )}
      {transition && (
        <TransitionDialog
          projectId={projectId}
          item={transition.item}
          target={transition.target}
          onClose={() => { setTransition(null); void client.invalidateQueries({ queryKey: ["private", session.user.id] }); }}
          onDone={() => done(t("mediaTransitionDone"))}
        />
      )}
    </>
  );
}
export function MediaPage({ projectId }: { projectId: string }) {
  return (
    <ProjectWorkspace projectId={projectId}>
      <ProjectMediaContent key={projectId} />
    </ProjectWorkspace>
  );
}

function ProjectMediaContent() { const project = useProject(); return <MediaContent projectId={project.id} projectName={project.name} projectRole={project.role} />; }
export function GlobalMediaPage() { return <Workspace><MediaContent projectId={null} projectName="global" /></Workspace>; }
