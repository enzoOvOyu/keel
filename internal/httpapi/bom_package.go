package httpapi

import (
	"io"
	"net/http"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

const maxBOMRequestBytes = 64 << 20

func readBOMPackageUpload(c *gin.Context) (map[string][]byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBOMRequestBytes)
	err := c.Request.ParseMultipartForm(8 << 20)
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_UPLOAD_INVALID", "message": err.Error()})
		return nil, false
	}
	form := c.Request.MultipartForm
	if archive := form.File["file"]; len(archive) == 1 && len(form.File["files"]) == 0 {
		reader, err := archive[0].Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_FILE_INVALID"})
			return nil, false
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, maxBOMRequestBytes+1))
		if err != nil || len(data) > maxBOMRequestBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "BOM_FILE_TOO_LARGE"})
			return nil, false
		}
		files, err := platform.ReadBOMPackageArchive(archive[0].Filename, data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_ARCHIVE_INVALID", "message": err.Error()})
			return nil, false
		}
		return files, true
	}
	entries, paths := form.File["files"], form.Value["paths"]
	if len(form.File["file"]) != 0 || len(entries) == 0 || len(entries) != len(paths) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_FILE_REQUIRED", "message": "Provide one archive or matching directory files and paths"})
		return nil, false
	}
	raw := make(map[string][]byte, len(entries))
	for i, header := range entries {
		name := paths[i]
		if _, exists := raw[name]; exists {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_DIRECTORY_INVALID", "message": "Duplicate path"})
			return nil, false
		}
		reader, err := header.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_FILE_INVALID"})
			return nil, false
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxBOMRequestBytes+1))
		reader.Close()
		if readErr != nil || len(data) > maxBOMRequestBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "BOM_FILE_TOO_LARGE"})
			return nil, false
		}
		raw[name] = data
	}
	files, err := platform.ReadBOMPackageDirectory(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_DIRECTORY_INVALID", "message": err.Error()})
		return nil, false
	}
	return files, true
}

func (a *authAPI) registerBOMPackages(r *gin.Engine) {
	a.registerLegacyBOM(r)
	r.GET("/api/v1/projects/:id/bom/packages/:package_id/issues", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		result, err := a.store.ListBOMPackageIssues(c.Request.Context(), actor, project.ID, c.Param("package_id"), c.Query("severity"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	})
	r.POST("/api/v1/projects/:id/bom/packages/preview", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		files, ok := readBOMPackageUpload(c)
		if !ok {
			return
		}
		preview, err := a.store.PreviewBOMPackage(c.Request.Context(), actor, project, files)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, preview)
	})
	r.POST("/api/v1/projects/:id/bom/packages/commit", func(c *gin.Context) {
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
		files, ok := readBOMPackageUpload(c)
		if !ok {
			return
		}
		result, err := a.store.CommitBOMPackage(c.Request.Context(), actor, project, files, c.PostForm("preview_token"), c.GetString("trace_id"))
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
