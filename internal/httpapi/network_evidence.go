package httpapi

import (
	"bytes"
	"encoding/csv"
	"github.com/gin-gonic/gin"
	"strconv"
	"strings"
	"time"
)

func (a *authAPI) registerNetworkEvidence(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric"
	r.GET(base+"/lldp", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.NetworkLLDP(c.Request.Context(), actor, p, c.Query("device_id"), c.Query("port"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/cables", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		if e != nil {
			failure(c, 400, "VALIDATION_ERROR", "Invalid page")
			return
		}
		size, e := strconv.Atoi(c.DefaultQuery("page_size", "200"))
		if e != nil {
			failure(c, 400, "VALIDATION_ERROR", "Invalid page size")
			return
		}
		out, e := a.store.NetworkCables(c.Request.Context(), actor, p, page, size, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/devices/:deviceId/logs", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		hours, e := strconv.Atoi(c.DefaultQuery("hours", "48"))
		if e != nil {
			failure(c, 400, "VALIDATION_ERROR", "Invalid log hours")
			return
		}
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		if e != nil {
			failure(c, 400, "VALIDATION_ERROR", "Invalid log page")
			return
		}
		size, e := strconv.Atoi(c.DefaultQuery("page_size", "200"))
		if e != nil {
			failure(c, 400, "VALIDATION_ERROR", "Invalid log page size")
			return
		}
		out, e := a.store.NetworkLogs(c.Request.Context(), actor, p, c.Param("deviceId"), hours, page, size, c.Query("level"), c.Query("keyword"), c.Query("format"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		if c.Query("format") == "csv" {
			var buffer bytes.Buffer
			writer := csv.NewWriter(&buffer)
			writer.Write([]string{"at", "level", "source", "process", "message", "raw", "object_ref"})
			safe := func(v string) string {
				trimmed := strings.TrimLeft(v, " \t\r\n")
				if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
					return "'" + v
				}
				return v
			}
			for _, entry := range out.Entries {
				writer.Write([]string{entry.At.Format(time.RFC3339Nano), safe(entry.Level), safe(entry.Source), safe(entry.Process), safe(entry.Message), safe(entry.Raw), safe(entry.ObjectRef)})
			}
			writer.Flush()
			if writer.Error() != nil {
				failure(c, 500, "EXPORT_FAILED", "Unable to encode log export")
				return
			}
			c.Header("Content-Disposition", "attachment; filename=network-logs.csv")
			c.Data(200, "text/csv; charset=utf-8", buffer.Bytes())
			return
		}
		if c.Query("format") == "json" {
			c.Header("Content-Disposition", "attachment; filename=network-logs.json")
		}

		c.JSON(200, out)
	})

}
