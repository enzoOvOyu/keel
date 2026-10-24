import { Workspace } from "../../components/Workspace";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
  const [type, setType] = useState<"driver">("driver"),
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
      confirm={t(action.busy ? "saving" : "mediaCreate")}
      onSubmit={(event) => void submit(event)}
      onClose={onClose}
    >
      <ActionError error={action.error} />
      {validation && (
        <p className="field-error" role="alert">
          {validation}
        </p>
      )}
      <div className="field">
        <label htmlFor="media-type">{t("mediaType")}</label>
        <select
          id="media-type"
          value={type}
          onChange={(event) => setType(event.target.value as typeof type)}
          disabled={action.busy}
        >
          {types.map((value) => (
            <option key={value} value={value}>
              {t("media_type_" + value)}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor="media-name">{t("mediaName")}</label>
        <input
          id="media-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          maxLength={120}
          required
          disabled={action.busy}
        />
      </div>
      <div className="field">
        <label htmlFor="media-version">{t("mediaVersion")}</label>
        <input
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
            <label htmlFor="media-sha">{t("mediaSha256")}</label>
            <input
              id="media-sha"
              value={sha256}
              onChange={(event) => setSha256(event.target.value)}
              maxLength={64}
              required
              disabled={action.busy}
              className="mono-value"
            />
          </div>
          <div className="field">
            <label htmlFor="media-size">{t("mediaSize")}</label>
            <input
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
        <label htmlFor="media-arch">{t("mediaArch")}</label>
        <select
          id="media-arch"
          value={arch}
          onChange={(event) => setArch(event.target.value as typeof arch)}
          disabled={action.busy}
        >
          <option value="amd64">amd64</option>
          <option value="arm64">arm64</option>
          <option value="noarch">noarch</option>
        </select>
        <span className="field-hint">{t("mediaArchHint")}</span>
      </div>
      <div className="field">
        <label htmlFor="media-os">{t("mediaOsScope")}</label>
        <input
          id="media-os"
          value={osScope}
          onChange={(event) => setOsScope(event.target.value)}
          maxLength={120}
          disabled={action.busy}
        />
      </div>
      <div className="field">
        <label htmlFor="media-hw">{t("mediaHwScope")}</label>
        <input
          id="media-hw"
          value={hwScope}
          onChange={(event) => setHwScope(event.target.value)}
          maxLength={120}
          disabled={action.busy}
        />
      </div>
      {!upload && (
        <div className="field">
          <label htmlFor="media-manifest">{t("mediaManifest")}</label>
          <input
            id="media-manifest"
            value={manifest}
            onChange={(event) => setManifest(event.target.value)}
            maxLength={200}
            disabled={action.busy}
            className="mono-value"
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
      {action.error?.code === "MEDIA_CONFLICT" ? <p role="alert" className="field-error">{t("mediaConflict")}</p> : <ActionError error={action.error} />}
      <dl className="action-target">
        <dt>{t("mediaName")}</dt>
        <dd>
          {item.name} · {item.version}
        </dd>
        <dt>{t("mediaState")}</dt>
        <dd>{t("media_state_" + item.state)}</dd>
      </dl>
      {target === "retired" && (
        <p className="impact-notice">{t("mediaRetireImpact")}</p>
      )}
      {target === "approved" && (
        <p className="impact-notice">{t("mediaSelfApproval")}</p>
      )}
      {target === "verified" && (
        <p className="impact-notice">{t("mediaManifestMissing")}</p>
      )}
    </ActionDialog>
  );
}
function MediaContent({ projectId, projectName, projectRole }: { projectId: string | null; projectName: string; projectRole?: string }) {
  const { t, i18n } = useTranslation(),
    session = useSession(),
    client = useQueryClient();
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
      page,
    ],
    queryFn: ({ signal }) => listMedia(projectId, page, signal),
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
          <button
            className="primary-button"
            onClick={() => {
              setSuccess("");
              setCreate(true);
            }}
          >
            {t("mediaCreate")}
          </button>
        )}
      </div>
      {success && <SuccessNotice>{success}</SuccessNotice>}
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
                <table className="data-table">
                  <thead>
                    <tr>
                      <th scope="col">{t("mediaName")}</th>
                      <th scope="col">{t("mediaType")}</th>
                      <th scope="col">{t("mediaArch")}</th>
                      <th scope="col">{t("mediaState")}</th>
                      <th scope="col">{t("createdAt")}</th>
                      <th scope="col">{t("actions")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {media.data.items.map((item) => (
                      <tr key={item.id}>
                        <td>
                          <strong>{item.name}</strong>
                          {item.scope_key === "global" && <span className="cell-subtitle">{t(projectId === null ? "mediaGlobalScope" : "mediaGlobalInherited")}</span>}
                          <span className="cell-subtitle mono-value">
                            {item.version} · {item.sha256.slice(0, 12)}…
                          </span>
                        </td>
                        <td>{t("media_type_" + item.type)}</td>
                        <td>{item.arch}</td>
                        <td>
                          <span
                            className={`status-label ${item.state === "approved" ? "enabled" : item.state === "retired" ? "disabled" : ""}`}
                          >
                            {t("media_state_" + item.state)}
                          </span>
                          <span className="cell-subtitle">
                            {t(
                              item.artifact_stored
                                ? "mediaBytesStored"
                                : "mediaBytesMissing",
                            )}
                          </span>
                        </td>
                        <td>{format(item.created_at)}</td>
                        <td>
                          <div className="table-actions">
                            {manage && item.scope_key === (projectId === null ? "global" : "project:" + projectId) &&
                              nextStates[item.state].map((target) => (
                                <button
                                  key={target}
                                  disabled={
                                    target === "verified" &&
                                    !item.artifact_stored
                                  }
                                  className={
                                    target === "retired" ? "danger-text" : ""
                                  }
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
                                </button>
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
                            {item.state === "approved" &&
                              item.artifact_stored && (
                                <a href={mediaDownloadURL(projectId, item.id)}>
                                  {t("mediaDownload")}
                                </a>
                              )}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
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
