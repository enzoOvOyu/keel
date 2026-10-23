package fabricmon

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// LogEntry is normalized by a NOS adapter. Credentials are rejected before storage.
type LogEntry struct {
	At        time.Time `json:"at"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Process   string    `json:"process"`
	Message   string    `json:"message"`
	Raw       string    `json:"raw"`
	ObjectRef string    `json:"object_ref"`
}
type LogRule struct {
	ID         string `json:"rule_id"`
	Version    int    `json:"version"`
	Pattern    string `json:"pattern"`
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	Advice     string `json:"advice"`
	expression *regexp.Regexp
}
type LogFinding struct {
	ID       string     `json:"id"`
	RuleRef  string     `json:"rule_ref"`
	Category string     `json:"category"`
	Severity string     `json:"severity"`
	Count    int        `json:"count"`
	FirstAt  time.Time  `json:"first_at"`
	LastAt   time.Time  `json:"last_at"`
	Objects  []string   `json:"objects"`
	Samples  []LogEntry `json:"samples"`
	Advice   string     `json:"advice"`
}

//go:embed log-defaults.json
var defaultLogRules []byte
var logSecrets = regexp.MustCompile(`(?i)(password|passwd|secret|token|authorization|private.key)\s*[:=]\s*\S+|-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`)
var logDigits = regexp.MustCompile(`[0-9]+`)

func (e LogEntry) Validate(since, collected time.Time) error {
	if e.At.IsZero() || e.At.Before(since) || e.At.After(collected.Add(5*time.Second)) {
		return errors.New("log timestamp is outside the declared observation window")
	}
	switch e.Level {
	case "debug", "info", "notice", "warning", "error", "critical", "alert", "emergency":
	default:
		return errors.New("invalid normalized log level")
	}
	if e.Source != "syslog" && e.Source != "snmp_trap" {
		return errors.New("invalid normalized log source")
	}
	if len(e.Raw) > 8192 || len(e.Message) > 8192 || len(e.Process) > 128 || len(e.ObjectRef) > 256 || strings.TrimSpace(e.Message) == "" || strings.TrimSpace(e.Raw) == "" {
		return errors.New("invalid or oversized log line")
	}
	if logSecrets.MatchString(e.Raw) || logSecrets.MatchString(e.Message) {
		return errors.New("collector must redact credentials before storing log evidence")
	}
	return nil
}
func DefaultLogRules() ([]LogRule, error) {
	var rules []LogRule
	if e := json.Unmarshal(defaultLogRules, &rules); e != nil {
		return nil, e
	}
	for i := range rules {
		rx, e := regexp.Compile(rules[i].Pattern)
		if e != nil {
			return nil, e
		}
		rules[i].expression = rx
	}
	return rules, nil
}
func AnalyzeLogs(entries []LogEntry, rules []LogRule, since time.Time, level, keyword string) []LogFinding {
	groups := map[string]*LogFinding{}
	for _, e := range entries {
		if e.At.Before(since) || level != "" && e.Level != level || keyword != "" && !strings.Contains(strings.ToLower(e.Message+" "+e.Raw+" "+e.Process), strings.ToLower(keyword)) {
			continue
		}
		matched := false
		for _, r := range rules {
			if r.expression == nil {
				rx, err := regexp.Compile(r.Pattern)
				if err != nil {
					continue
				}
				r.expression = rx
			}
			if !r.expression.MatchString(e.Process + " " + e.Message) {
				continue
			}
			matched = true
			addLogFinding(groups, r.ID, r, e)
		}
		if !matched && (e.Level == "error" || e.Level == "critical" || e.Level == "alert" || e.Level == "emergency") {
			sum := sha256.Sum256([]byte(e.Process + "\x00" + logDigits.ReplaceAllString(strings.ToLower(e.Message), "#")))
			r := LogRule{ID: "LOG-UNKNOWN", Version: 1, Category: "unrecognized_error", Severity: "warning", Advice: "Inspect raw context and correlate physical, link/protocol, then service evidence; add a reviewed parser rule."}
			addLogFinding(groups, r.ID+":"+hex.EncodeToString(sum[:8]), r, e)
		}
	}
	out := []LogFinding{}
	for _, f := range groups {
		sort.Strings(f.Objects)
		sort.Slice(f.Samples, func(i, j int) bool { return f.Samples[i].At.Before(f.Samples[j].At) })
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastAt.Equal(out[j].LastAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].LastAt.After(out[j].LastAt)
	})
	return out
}
func addLogFinding(groups map[string]*LogFinding, id string, r LogRule, e LogEntry) {
	f := groups[id]
	if f == nil {
		f = &LogFinding{ID: id, RuleRef: r.ID + "@" + jsonNumber(r.Version), Category: r.Category, Severity: r.Severity, FirstAt: e.At, LastAt: e.At, Objects: []string{}, Samples: []LogEntry{}, Advice: r.Advice}
		groups[id] = f
	}
	f.Count++
	if e.At.Before(f.FirstAt) {
		f.FirstAt = e.At
	}
	if e.At.After(f.LastAt) {
		f.LastAt = e.At
	}
	if len(f.Samples) < 3 {
		f.Samples = append(f.Samples, e)
	}
	if e.ObjectRef != "" {
		for _, v := range f.Objects {
			if v == e.ObjectRef {
				return
			}
		}
		f.Objects = append(f.Objects, e.ObjectRef)
	}
}
func jsonNumber(v int) string { b, _ := json.Marshal(v); return string(b) }
