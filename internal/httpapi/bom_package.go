package httpapi

import (
	"io"
	"net/http"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

const maxBOMRequestBytes = 64 << 20

func (a *authAPI) registerBOMPackages(r *gin.Engine) {
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
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBOMRequestBytes)
		header, err := c.FormFile("file")
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_FILE_REQUIRED", "message": "A zip or tar.gz file is required"})
			return
		}
		reader, err := header.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_FILE_INVALID"})
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, maxBOMRequestBytes+1))
		if err != nil || len(data) > maxBOMRequestBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "BOM_FILE_TOO_LARGE"})
			return
		}
		files, err := platform.ReadBOMPackageArchive(header.Filename, data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BOM_ARCHIVE_INVALID", "message": err.Error()})
			return
		}
		preview, err := a.store.PreviewBOMPackage(c.Request.Context(), actor, project, files)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, preview)
	})
}
