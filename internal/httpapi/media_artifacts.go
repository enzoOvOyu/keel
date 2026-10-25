package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
	"os"
	"time"
)

func (a *authAPI) mediaScope(c *gin.Context, write, global bool) (*platform.AuthSession, *platform.ProjectSummary, bool) {
	actor, ok := a.authenticated(c)
	if !ok {
		return nil, nil, false
	}
	if global && !actor.User.IsAdmin {
		a.respondError(c, platform.ErrForbidden)
		return nil, nil, false
	}
	if write && !a.writeAllowed(c) {
		return nil, nil, false
	}
	var project *platform.ProjectSummary
	if !global {
		var e error
		project, e = a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return nil, nil, false
		}
	}
	if write {
		if e := a.store.CheckMediaManage(c.Request.Context(), actor, project); e != nil {
			a.respondError(c, e)
			return nil, nil, false
		}
	}
	return actor, project, true
}
func mediaDeadline(c *gin.Context) context.CancelFunc {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	c.Request = c.Request.WithContext(ctx)
	control := http.NewResponseController(c.Writer)
	_ = control.SetReadDeadline(time.Now().Add(5 * time.Minute))
	_ = control.SetWriteDeadline(time.Now().Add(5 * time.Minute))
	return cancel
}
func (a *authAPI) registerMediaArtifacts(r *gin.Engine, base string, global bool) {
	a.registerMediaQuarantineEvidence(r, base, global)
	r.GET(base+"/:mediaId/manifest", func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, false, global)
		if !ok {
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		manifest, err := a.store.ExportMediaManifest(c.Request.Context(), actor, project, c.Param("mediaId"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "keel-media-" + manifest.Payload.RootID + ".manifest.json"}))
		c.JSON(200, manifest)
	})
	r.POST(base+"/:mediaId/fetch", func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, true, global)
		if !ok {
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		// This action consumes the immutable registered URL, never a request URL.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1)
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil || len(raw) != 0 {
			failure(c, 400, "VALIDATION_ERROR", "Source fetch does not accept a request body")
			return
		}
		item, err := a.store.FetchMediaSource(c.Request.Context(), actor, project, c.Param("mediaId"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, item)
	})
	r.POST(base+"/upload", func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, true, global)
		if !ok {
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, platform.MediaMaxBytes+128*1024)
		parts, e := c.Request.MultipartReader()
		if e != nil {
			failure(c, 415, "UNSUPPORTED_MEDIA_TYPE", "multipart/form-data is required")
			return
		}
		metadata, e := parts.NextPart()
		if e != nil || metadata.FormName() != "metadata" {
			failure(c, 400, "VALIDATION_ERROR", "The first multipart field must contain metadata JSON")
			return
		}
		raw, e := io.ReadAll(io.LimitReader(metadata, 64*1024+1))
		if e != nil || len(raw) > 64*1024 {
			failure(c, 400, "VALIDATION_ERROR", "Media metadata exceeds 64 KiB")
			return
		}
		var input platform.RegisterMediaInput
		// Same metadata contract as registration; actual digest/size override input.
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if e = decoder.Decode(&input); e != nil {
			failure(c, 400, "VALIDATION_ERROR", "Invalid media metadata JSON")
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			failure(c, 400, "VALIDATION_ERROR", "Only one metadata JSON object is allowed")
			return
		}
		file, e := parts.NextPart()
		if e != nil || file.FormName() != "file" || file.FileName() == "" {
			failure(c, 400, "VALIDATION_ERROR", "A media file must follow metadata")
			return
		}
		item, e := a.store.IngestMedia(c.Request.Context(), actor, project, input, file, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, item)
	})
	r.PUT(base+"/:mediaId/content", func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, true, global)
		if !ok {
			return
		}
		kind, _, e := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if e != nil || kind != "application/octet-stream" {
			failure(c, 415, "UNSUPPORTED_MEDIA_TYPE", "application/octet-stream is required")
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, platform.MediaMaxBytes+1)
		item, e := a.store.AttachMedia(c.Request.Context(), actor, project, c.Param("mediaId"), c.Request.Body, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, item)
	})
	r.GET(base+"/:mediaId/content", func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, false, global)
		if !ok {
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		file, item, e := a.store.DownloadMedia(c.Request.Context(), actor, project, c.Param("mediaId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		defer func() { file.Close(); os.Remove(file.Name()) }()
		c.Header("ETag", `"`+item.Sha256+`"`)
		c.Header("X-Content-SHA256", item.Sha256)
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.Name + "-" + item.Version}))
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		http.ServeContent(c.Writer, c.Request, item.Name, item.UpdatedAt, file)
	})
}
