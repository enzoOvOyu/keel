package fabricmon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Counter values retain their decimal representation; float64 cannot preserve
// one-frame increments once a long-running hardware counter exceeds 2^53.
type MetricPoint struct {
	Provider string       `json:"provider,omitempty"`
	Key      string       `json:"key"`
	Unit     string       `json:"unit"`
	Port     string       `json:"port"`
	Priority *int         `json:"priority,omitempty"`
	Queue    string       `json:"queue,omitempty"`
	Pool     string       `json:"pool,omitempty"`
	Source   string       `json:"source"`
	At       time.Time    `json:"at"`
	Value    *json.Number `json:"value"`
}
type MetricDefinition struct {
	Key             string   `json:"key"`
	Unit            string   `json:"unit"`
	Kind            string   `json:"kind"`
	Granularity     string   `json:"granularity"`
	Sources         []string `json:"sources"`
	IntervalSeconds int      `json:"interval_seconds"`
}
type MetricUnit struct {
	Device   string `json:"device"`
	Pod      string `json:"pod"`
	Role     string `json:"role"`
	Port     string `json:"port"`
	Priority int    `json:"priority"`
	Queue    string `json:"queue,omitempty"`
	Pool     string `json:"pool"`
}

func (p MetricPoint) Validate(registry map[string]MetricDefinition, now time.Time) error {
	d, ok := registry[p.Key]
	if !ok {
		return fmt.Errorf("metric %s is not registered", p.Key)
	}
	if p.Unit != d.Unit {
		return fmt.Errorf("metric %s unit differs from registry", p.Key)
	}
	allowed := false
	for _, source := range d.Sources {
		allowed = allowed || p.Source == source
	}
	if !allowed {
		return errors.New("metric source differs from registry")
	}
	if p.At.IsZero() || p.At.After(now.Add(5*time.Second)) || p.At.Before(now.Add(-48*time.Hour)) {
		return errors.New("metric sample time is invalid")
	}
	if d.Granularity == "port_priority" && (p.Port == "" || p.Priority == nil) {
		return errors.New("metric requires port and priority")
	}
	if (d.Granularity == "port" || d.Granularity == "port_queue") && p.Port == "" || d.Granularity == "device_pool" && p.Pool == "" {
		return errors.New("metric scope is missing")
	}
	if d.Granularity == "port_queue" && p.Queue == "" {
		return errors.New("metric requires queue")
	}
	if d.Granularity != "port_priority" && p.Priority != nil {
		return errors.New("metric has an unexpected priority dimension")
	}
	if d.Granularity != "port_queue" && p.Queue != "" {
		return errors.New("metric has an unexpected queue dimension")
	}
	if d.Granularity != "device_pool" && p.Pool != "" {
		return errors.New("metric has an unexpected pool dimension")
	}
	if (d.Granularity == "device" || d.Granularity == "device_pool") && p.Port != "" {
		return errors.New("device metric cannot have a port dimension")
	}
	if p.Priority != nil && (*p.Priority < 0 || *p.Priority > 7) {
		return errors.New("invalid traffic priority")
	}
	if p.Value != nil {
		v, ok := decimal(*p.Value)
		if !ok || v.Sign() < 0 {
			return errors.New("invalid metric value")
		}
		if d.Kind == "counter" && !v.IsInt() {
			return errors.New("counter must be an unsigned integer")
		}
	}
	return nil
}
func decimal(v json.Number) (*big.Rat, bool) {
	if len(v.String()) > 80 {
		return nil, false
	}
	for _, separator := range []string{"e", "E"} {
		if parts := strings.Split(v.String(), separator); len(parts) > 1 {
			power, err := strconv.Atoi(parts[1])
			if err != nil || power < -128 || power > 128 {
				return nil, false
			}
		}
	}
	raw := []byte(v.String())
	if !json.Valid(raw) || bytes.ContainsAny(raw, "[]{}\"ntf") {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(v.String())
	return r, ok
}

// Expressions are data, restricted to bounded arithmetic, observations and
// comparisons. There is no scripting, I/O, file access or dynamic code loading.
type MetricExpression struct {
	Op               string             `json:"op"`
	Key              string             `json:"key,omitempty"`
	Value            json.Number        `json:"value,omitempty"`
	Args             []MetricExpression `json:"args,omitempty"`
	PathA            string             `json:"path_a,omitempty"`
	PathB            string             `json:"path_b,omitempty"`
	WindowSeconds    int                `json:"window_seconds,omitempty"`
	EndOffsetSeconds int                `json:"end_offset_seconds,omitempty"`
	Flags            []string           `json:"flags,omitempty"`
}
type MetricValue struct {
	Number *big.Rat
	Reason string
	Flags  []string
}

func unknownMetric(reason string) MetricValue {
	return MetricValue{Reason: reason, Flags: []string{"degraded"}}
}
func numberMetric(value int64) MetricValue { return MetricValue{Number: big.NewRat(value, 1)} }
func metricIndexKey(key, port string, priority int, pool, queue string) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s", key, port, priority, pool, queue)
}

type MetricEvaluation struct {
	Registry map[string]MetricDefinition
	Index    map[string][]MetricPoint
	Facts    map[string]any
	Now      time.Time
}

func NewMetricEvaluation(registry map[string]MetricDefinition, points []MetricPoint, facts map[string]any, now time.Time) (*MetricEvaluation, error) {
	c := &MetricEvaluation{Registry: registry, Index: map[string][]MetricPoint{}, Facts: facts, Now: now}
	for _, p := range points {
		if e := p.Validate(registry, now); e != nil {
			return nil, e
		}
		priority := -1
		if p.Priority != nil {
			priority = *p.Priority
		}
		key := metricIndexKey(p.Key, p.Port, priority, p.Pool, p.Queue)
		c.Index[key] = append(c.Index[key], p)
	}
	for key, values := range c.Index {
		sort.Slice(values, func(i, j int) bool { return values[i].At.Before(values[j].At) })
		for i := 1; i < len(values); i++ {
			if values[i].At.Equal(values[i-1].At) {
				return nil, errors.New("duplicate metric scope and sample time")
			}
		}
		c.Index[key] = values
	}
	return c, nil
}
func (c *MetricEvaluation) series(key string, u MetricUnit) []MetricPoint {
	d, ok := c.Registry[key]
	if !ok {
		return nil
	}
	port, priority, pool, queue := u.Port, u.Priority, u.Pool, u.Queue
	switch d.Granularity {
	case "port":
		priority = -1
		pool = ""
		queue = ""
	case "device":
		port = ""
		priority = -1
		pool = ""
		queue = ""
	case "device_pool":
		port = ""
		priority = -1
		queue = ""
	case "port_priority":
		pool = ""
		queue = ""
	case "port_queue":
		priority = -1
		pool = ""
	default:
		return nil
	}
	return c.Index[metricIndexKey(key, port, priority, pool, queue)]
}
func (c *MetricEvaluation) ValidateExpression(expr MetricExpression) error {
	nodes := 0
	var visit func(MetricExpression, int) error
	visit = func(e MetricExpression, depth int) error {
		nodes++
		if depth > 12 || nodes > 256 {
			return errors.New("metric expression exceeds limits")
		}
		if e.WindowSeconds != 0 && e.WindowSeconds != 60 && e.WindowSeconds != 300 && e.WindowSeconds != 3600 {
			return errors.New("invalid metric window")
		}
		if e.EndOffsetSeconds < 0 || e.EndOffsetSeconds > 3600 {
			return errors.New("invalid metric window offset")
		}
		switch e.Op {
		case "literal":
			if _, ok := decimal(e.Value); !ok {
				return errors.New("invalid expression literal")
			}
			if len(e.Args) != 0 {
				return errors.New("literal cannot have arguments")
			}
		case "latest", "delta", "max":
			if _, ok := c.Registry[e.Key]; !ok {
				return errors.New("expression metric is not registered")
			}
			if len(e.Args) != 0 {
				return errors.New("metric cannot have arguments")
			}
		case "mismatch", "set_mismatch":
			if !safeFactPath(e.PathA) || !safeFactPath(e.PathB) || len(e.Args) != 0 {
				return errors.New("invalid fact comparison")
			}
		case "sum", "product", "and", "or", "coalesce":
			if len(e.Args) < 2 || len(e.Args) > 16 {
				return errors.New("expression requires 2 through 16 arguments")
			}
		case "ratio", "gt", "gte", "lt", "lte", "eq":
			if len(e.Args) != 2 {
				return errors.New("binary expression requires two arguments")
			}
		default:
			return errors.New("unsupported metric expression operation")
		}
		for _, a := range e.Args {
			if err := visit(a, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(expr, 0)
}
func safeFactPath(path string) bool {
	return (strings.HasPrefix(path, "intended.") || strings.HasPrefix(path, "applied.")) && len(path) <= 240 && !strings.ContainsAny(path, "[]\x00\r\n")
}
func factAt(facts map[string]any, path string) (any, bool) {
	var value any = facts
	for _, key := range strings.Split(path, ".") {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = m[key]
		if !ok || value == nil {
			return nil, false
		}
	}
	return value, true
}
func normalizedFact(value any, set bool) string {
	if set {
		if a, ok := value.([]any); ok {
			parts := make([]string, 0, len(a))
			for _, v := range a {
				raw, _ := json.Marshal(v)
				parts = append(parts, string(raw))
			}
			sort.Strings(parts)
			return strings.Join(parts, "\x00")
		}
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}
func (c *MetricEvaluation) Evaluate(expr MetricExpression, u MetricUnit, window int) MetricValue {
	if e := c.ValidateExpression(expr); e != nil {
		return unknownMetric("invalid_expression")
	}
	return c.evaluate(expr, u, window)
}
func (c *MetricEvaluation) evaluate(expr MetricExpression, u MetricUnit, window int) MetricValue {
	if expr.WindowSeconds != 0 {
		window = expr.WindowSeconds
	}
	end := c.Now.Add(-time.Duration(expr.EndOffsetSeconds) * time.Second)
	switch expr.Op {
	case "literal":
		n, _ := decimal(expr.Value)
		return MetricValue{Number: n}
	case "mismatch", "set_mismatch":
		a, okA := factAt(c.Facts, expr.PathA)
		b, okB := factAt(c.Facts, expr.PathB)
		if !okA || !okB {
			return unknownMetric("configuration_evidence_missing")
		}
		if normalizedFact(a, expr.Op == "set_mismatch") != normalizedFact(b, expr.Op == "set_mismatch") {
			return numberMetric(1)
		}
		return numberMetric(0)
	case "latest", "delta", "max":
		d, ok := c.Registry[expr.Key]
		if !ok {
			return unknownMetric("metric_not_registered")
		}
		points := c.series(expr.Key, u)
		interval := time.Duration(d.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		if expr.Op == "latest" {
			for i := len(points) - 1; i >= 0; i-- {
				p := points[i]
				if p.At.After(end) {
					continue
				}
				if end.Sub(p.At) > interval*3/2 || p.Value == nil {
					return unknownMetric("metric_stale_or_unavailable")
				}
				n, _ := decimal(*p.Value)
				return MetricValue{Number: n}
			}
			return unknownMetric("metric_unavailable")
		}
		start := end.Add(-time.Duration(window) * time.Second)
		selected := []MetricPoint{}
		// Include the nearest sample at or before the start. Every subsequent gap
		// must be covered; a partial hour can never establish LL-03's full hour.
		for _, p := range points {
			if p.At.After(end) {
				break
			}
			if !p.At.After(start) {
				selected = []MetricPoint{p}
			} else {
				selected = append(selected, p)
			}
		}
		if len(selected) < 2 || selected[0].At.After(start) || start.Sub(selected[0].At) > interval*3/2 || end.Sub(selected[len(selected)-1].At) > interval*3/2 {
			return unknownMetric("window_incomplete")
		}
		var first, last, maximum *big.Rat
		source := ""
		for i, p := range selected {
			if p.Value == nil || i > 0 && p.At.Sub(selected[i-1].At) > interval*3/2 {
				return unknownMetric("window_gap")
			}
			if source != "" && source != p.Source+"/"+p.Provider {
				return unknownMetric("metric_source_changed")
			}
			source = p.Source + "/" + p.Provider
			n, valid := decimal(*p.Value)
			if !valid {
				return unknownMetric("invalid_metric")
			}
			if i > 0 && d.Kind == "counter" && n.Cmp(last) < 0 {
				return unknownMetric("counter_reset")
			}
			if first == nil {
				first = new(big.Rat).Set(n)
			}
			if maximum == nil || n.Cmp(maximum) > 0 {
				maximum = new(big.Rat).Set(n)
			}
			last = n
		}
		if expr.Op == "max" {
			return MetricValue{Number: maximum}
		}
		if d.Kind != "counter" {
			return unknownMetric("delta_requires_counter")
		}
		return MetricValue{Number: new(big.Rat).Sub(last, first)}
	}
	values := make([]MetricValue, 0, len(expr.Args))
	flags := []string{}
	child := *c
	child.Now = end
	for _, arg := range expr.Args {
		v := child.evaluate(arg, u, window)
		values = append(values, v)
		flags = append(flags, v.Flags...)
	}
	if expr.Op == "coalesce" {
		for i, v := range values {
			if v.Number != nil {
				if i > 0 {
					v.Flags = append(v.Flags, expr.Flags...)
				}
				return v
			}
		}
		return unknownMetric("all_sources_unavailable")
	}
	// Missing evidence stays unknown even when another operand is false. This
	// makes a missing hardware source visible rather than reporting normal.
	for _, v := range values {
		if v.Number == nil {
			return MetricValue{Reason: v.Reason, Flags: flags}
		}
	}
	out := new(big.Rat)
	truth := func(v bool) {
		if v {
			out.SetInt64(1)
		} else {
			out.SetInt64(0)
		}
	}
	switch expr.Op {
	case "sum":
		for _, v := range values {
			out.Add(out, v.Number)
		}
	case "product":
		out.SetInt64(1)
		for _, v := range values {
			out.Mul(out, v.Number)
		}
	case "ratio":
		if values[1].Number.Sign() == 0 {
			return unknownMetric("zero_denominator")
		}
		out.Quo(values[0].Number, values[1].Number)
	case "and":
		hit := true
		for _, v := range values {
			hit = hit && v.Number.Sign() != 0
		}
		truth(hit)
	case "or":
		hit := false
		for _, v := range values {
			hit = hit || v.Number.Sign() != 0
		}
		truth(hit)
	case "gt":
		truth(values[0].Number.Cmp(values[1].Number) > 0)
	case "gte":
		truth(values[0].Number.Cmp(values[1].Number) >= 0)
	case "lt":
		truth(values[0].Number.Cmp(values[1].Number) < 0)
	case "lte":
		truth(values[0].Number.Cmp(values[1].Number) <= 0)
	case "eq":
		truth(values[0].Number.Cmp(values[1].Number) == 0)
	default:
		return unknownMetric("invalid_expression")
	}
	return MetricValue{Number: out, Flags: flags}
}
