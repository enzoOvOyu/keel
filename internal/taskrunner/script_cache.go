package taskrunner

// ScriptCacheProbeHit accepts only the dedicated metadata probe response. This
// transient observation is not durable task acceptance or execution proof.
func ScriptCacheProbeHit(o Observation, c TaskCommand) (bool, error) {
	if (Request{Action: "cache", Command: c}).Validate() != nil || o.Command != c || o.State != "accepted" || !o.StartedAt.IsZero() || !o.FinishedAt.IsZero() || len(o.Output) != 4 || o.Output["cache_probe"] != "keel-script-cache/v1" || o.Output["sha256"] != c.ScriptSHA256 {
		return false, ErrRejected
	}
	hit, ok := o.Output["cache_hit"].(bool)
	size, valid := scriptOutputInteger(o.Output["size"])
	if !ok || !valid || size != c.ScriptSize {
		return false, ErrRejected
	}
	return hit, nil
}
