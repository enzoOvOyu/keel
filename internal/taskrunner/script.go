package taskrunner

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math"
	"regexp"
	"strings"
)

const MaxScriptBytes int64 = 8192

var scriptDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validScriptVersion(s string) bool {
	if s == "" || len(s) > 256 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
func validateScriptBody(c TaskCommand, body []byte) error {
	if c.Operation != "script.run" || c.Validate() != nil || int64(len(body)) != c.ScriptSize {
		return ErrRejected
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != c.ScriptSHA256 {
		return ErrRejected
	}
	return nil
}

func scriptOutputInteger(value any) (int64, bool) {
	switch n := value.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n >= 0 && n <= 1<<53 && !math.IsNaN(n) && !math.IsInf(n, 0) && math.Trunc(n) == n {
			return int64(n), true
		}
	}
	return 0, false
}
func validateScriptObservation(o Observation) error {
	if o.Output["process_tree_stopped"] != true {
		return ErrRejected
	}
	started, ok := o.Output["process_started"].(bool)
	if !ok {
		return ErrRejected
	}
	if !started {
		if o.State != "failed" {
			return ErrRejected
		}
		switch o.Output["reason"] {
		case "script_interpreter_unavailable", "script_process_not_started", "script_workspace_unavailable", "script_authorization_expired_before_start", "script_cache_rejected":
			if _, exists := o.Output["structured_result_json"]; exists {
				return ErrRejected
			}
			if _, exists := o.Output["structured_stdout_base64"]; exists {
				return ErrRejected
			}
			return nil
		default:
			return ErrRejected
		}
	}
	if o.Output["script_media_id"] != o.Command.ScriptMediaID || o.Output["script_object_version"] != o.Command.ScriptObjectVersion || o.Output["script_sha256"] != o.Command.ScriptSHA256 || o.Output["script_interpreter"] != o.Command.ScriptInterpreter {
		return ErrRejected
	}
	if hit, exists := o.Output["cache_hit"]; exists {
		if _, ok := hit.(bool); !ok || o.Output["cache_directory"] != MediaCacheDirectory {
			return ErrRejected
		}
	} else if _, exists := o.Output["cache_directory"]; exists {
		return ErrRejected
	}
	code, ok := scriptOutputInteger(o.Output["exit_code"])
	if !ok || code > 255 || code < 0 || o.State == "success" && code != 0 {
		return ErrRejected
	}
	for _, stream := range []string{"stdout", "stderr"} {
		digest, ok := o.Output[stream+"_sha256"].(string)
		if !ok || !scriptDigestPattern.MatchString(digest) {
			return ErrRejected
		}
		size, ok := scriptOutputInteger(o.Output[stream+"_bytes"])
		if !ok || size < 0 || size > (1<<20)+4096 {
			return ErrRejected
		}
		prefix, ok := o.Output[stream+"_prefix_base64"].(string)
		if !ok {
			return ErrRejected
		}
		data, err := base64.StdEncoding.DecodeString(prefix)
		if err != nil || base64.StdEncoding.EncodeToString(data) != prefix || len(data) > 512 || int64(len(data)) > size {
			return ErrRejected
		}
		complete, ok := o.Output[stream+"_complete"].(bool)
		if !ok || o.State == "success" && !complete {
			return ErrRejected
		}
	}
	return validateScriptJSONObservation(o)
}
