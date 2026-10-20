package httpapi

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func readLegacyBOMUpload(c *gin.Context) (string, []byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBOMRequestBytes)
	header, err := c.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil || header == nil || !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_LEGACY_FILE_REQUIRED", "message": "A CSV file is required"})
		return "", nil, false
	}
	batch := c.PostForm("bom_import_batch_id")
	reader, err := header.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_FILE_INVALID"})
		return "", nil, false
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxBOMRequestBytes+1))
	if err != nil || len(data) > maxBOMRequestBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "BOM_FILE_TOO_LARGE"})
		return "", nil, false
	}
	return batch, data, true
}
func (a *authAPI) registerLegacyBOM(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/bom/legacy/preview", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		batch, data, ok := readLegacyBOMUpload(c)
		if !ok {
			return
		}
		result, err := a.store.PreviewLegacyBOM(c.Request.Context(), actor, project, batch, data)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	})
	r.POST("/api/v1/projects/:id/bom/legacy/commit", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		batch, data, ok := readLegacyBOMUpload(c)
		if !ok {
			return
		}
		result, err := a.store.CommitLegacyBOM(c.Request.Context(), actor, project, batch, data, c.PostForm("preview_token"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		if result.Idempotent {
			c.JSON(http.StatusOK, result)
		} else {
			c.JSON(http.StatusCreated, result)
		}
	})
}
