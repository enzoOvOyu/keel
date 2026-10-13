package platform

import (
	"fmt"
	"sort"
	"strings"
)

// verifyRenderReadback (FR-5.2) checks a sampled read-back against the
// rendered plan. The read-back is a flat map of "dotted.key" -> string; the
// rendered plan is parsed back into the same key=value tokens the renderer
// emitted (comment headers, stanza lines and totals). Every read-back entry
// must match at least one token with the same key and value; a missing entry
// is reported instead of being treated as a pass, and an empty read-back is
// never a verification (the receipt stays pending until the target answers).
func verifyRenderReadback(rendered string, readback map[string]any) (bool, string, []string) {
	warnings := []string{}
	if len(readback) == 0 {
		return false, "readback_missing: verification pending", []string{"readback_missing"}
	}
	tokens := parseRenderedTokens(rendered)
	keys := make([]string, 0, len(readback))
	for key := range readback {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	mismatch := []string{}
	missing := []string{}
	for _, key := range keys {
		want := scalarString(readback[key])
		if want == "" {
			missing = append(missing, key)
			continue
		}
		values, ok := tokens[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		matched := false
		for _, value := range values {
			if value == want {
				matched = true
				break
			}
		}
		if !matched {
			mismatch = append(mismatch, fmt.Sprintf("%s(readback=%s rendered=%s)", key, want, strings.Join(values, "|")))
		}
	}
	if len(missing) > 0 {
		warnings = append(warnings, "readback_incomplete:"+strings.Join(missing, ","))
	}
	if len(mismatch) > 0 {
		return false, "readback_mismatch: " + strings.Join(mismatch, "; "), warnings
	}
	if len(missing) > 0 {
		return false, "readback_incomplete: " + strings.Join(missing, ", "), warnings
	}
	return true, fmt.Sprintf("readback_ok: %d sampled keys match the rendered plan", len(keys)), warnings
}

// parseRenderedTokens reads a rendered plan back into key -> values. Both the
// "key=value" token style (comment headers, totals) and the "key value"
// stanza style (address/gateway/mtu lines) are recognised, so a read-back may
// sample either surface.
func parseRenderedTokens(rendered string) map[string][]string {
	out := map[string][]string{}
	add := func(key, value string) {
		if key == "" || value == "" {
			return
		}
		out[key] = append(out[key], value)
	}
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			for _, field := range strings.Fields(strings.TrimPrefix(line, "#")) {
				if key, value, ok := strings.Cut(field, "="); ok {
					add(key, value)
				} else if key, value, ok := strings.Cut(field, ":"); ok {
					add(key, value)
				}
			}
			continue
		}
		// "auto ens1" / "iface ens1 inet static" carry the interface name.
		if strings.HasPrefix(line, "auto ") || strings.HasPrefix(line, "iface ") {
			fields := strings.Fields(line)
			add("iface", fields[1])
			continue
		}
		fields := strings.Fields(line)
		if key, value, ok := strings.Cut(fields[0], "="); ok {
			add(key, value)
			continue
		}
		if len(fields) >= 2 {
			add(fields[0], strings.Join(fields[1:], " "))
		}
	}
	return out
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.4f", typed), "0"), ".")
	case bool:
		return fmt.Sprintf("%t", typed)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}
