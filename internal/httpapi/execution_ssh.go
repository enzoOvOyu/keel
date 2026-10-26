package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerSSHExecution(r *gin.Engine, base string) {
	r.POST(base+"/host-flow-executions", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.HostFlowCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := service.CreateHostFlow(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, out)
	})

	r.GET(base+"/task-scripts", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		transport := "ssh"
		if values, exists := c.Request.URL.Query()["transport"]; exists {
			if len(values) != 1 || values[0] != "ssh" && values[0] != "agent" {
				failure(c, 422, "INVALID_TRANSPORT", "Script transport must be ssh or agent")
				return
			}
			transport = values[0]
		}
		out, enabled, err := a.store.ListTaskScriptsForTransport(c.Request.Context(), actor, &platform.ProjectSummary{ID: c.Param("id")}, page, transport)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, struct {
			platform.Page[platform.TaskScriptArtifact]
			Enabled bool `json:"enabled"`
		}{out, enabled})
	})
	r.POST(base+"/ssh-trust-requests/:requestId/execute", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, e := s.CreateSSHTrustVerification(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"), in.ExpectedVersion, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, x)
	})
	r.POST(base+"/artifact-retrievals", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.ArtifactCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := service.CreateArtifactRetrieval(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, out)
	})
	r.POST(base+"/artifact-distributions", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.ArtifactCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := service.CreateArtifactDistribution(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, out)
	})
	r.POST(base+"/ssh-script-executions", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.SSHScriptCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, err := s.CreateSSHScript(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, x)
	})
	r.POST(base+"/ssh-executions", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.SSHCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, e := s.CreateSSH(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, x)
	})
	r.POST(base+"/executions/:executionId/attempts/:attemptId/artifact-nonstart-proof", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.ScriptNonstartProofInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := service.RequestArtifactNonstartProof(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), c.Param("attemptId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(202, out)
	})
	r.POST(base+"/executions/:executionId/attempts/:attemptId/script-nonstart-proof", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.ScriptNonstartProofInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := s.RequestScriptNonstartProof(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), c.Param("attemptId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(202, out)
	})
	r.POST(base+"/executions/:executionId/attempts/:attemptId/ssh-receipt", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, e := s.FetchSSHReceipt(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), c.Param("attemptId"), in.ExpectedVersion, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, x)
	})
}
