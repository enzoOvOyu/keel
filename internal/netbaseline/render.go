// Package netbaseline contains network intent rendering and pure comparison.
// Device I/O and approvals belong to adapters and platform use cases.
package netbaseline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/flosch/pongo2/v6"
	"io"
	"regexp"
	"sort"
	"strings"
)

const MaxRenderedBytes = 1 << 20

var tagPattern = regexp.MustCompile(`(?s)\{%\s*([a-zA-Z_]+).*?%\}`)
var filterPattern = regexp.MustCompile(`\|\s*([a-zA-Z_][a-zA-Z0-9_]*)`)
var variableName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type deniedLoader struct{}

func (deniedLoader) Abs(_, _ string) string { return "denied" }
func (deniedLoader) Get(string) (io.Reader, error) {
	return nil, errors.New("network templates cannot access files")
}
func init() {
	if e := pongo2.RegisterFilter("keeljson", func(v, p *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
		raw, e := json.Marshal(v.Interface())
		if e != nil {
			return nil, &pongo2.Error{Sender: "keeljson", OrigError: errors.New("value is not JSON")}
		}
		return pongo2.AsSafeValue(string(raw)), nil
	}); e != nil {
		panic("network JSON filter registration failed")
	}
}

type Variable struct {
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Enum     []string `json:"enum,omitempty"`
}
type Template struct {
	Source    string              `json:"source"`
	Variables map[string]Variable `json:"variables"`
}

func (t Template) Validate() error {
	if len(t.Source) == 0 || len(t.Source) > 64<<10 || len(t.Variables) > 128 {
		return errors.New("network template exceeds source/schema limits")
	}
	depth, loops, nodes := 0, 0, 0
	for _, match := range tagPattern.FindAllStringSubmatch(t.Source, -1) {
		nodes++
		switch match[1] {
		case "if":
			depth++
		case "for":
			depth++
			loops++
		case "else", "elif", "empty":
		case "endif", "endfor":
			depth--
		default:
			return fmt.Errorf("network template tag %s is not permitted", match[1])
		}
		if depth < 0 || depth > 4 || loops > 2 || nodes > 200 {
			return errors.New("template control flow exceeds sandbox limits")
		}
	}
	for _, match := range filterPattern.FindAllStringSubmatch(t.Source, -1) {
		if match[1] != "keeljson" {
			return errors.New("network templates only permit the keeljson encoding filter")
		}
	}
	for name, v := range t.Variables {
		if !variableName.MatchString(name) || name == "device" || name == "sot" || name == "forloop" {
			return errors.New("invalid or reserved template variable")
		}
		switch v.Type {
		case "string", "number", "integer", "boolean", "array", "object":
		default:
			return fmt.Errorf("unsupported variable type for %s", name)
		}
		if len(v.Enum) > 256 {
			return errors.New("template enum exceeds limit")
		}
	}
	_, e := sandbox().FromString(t.Source)
	return e
}
func sandbox() *pongo2.TemplateSet {
	set := pongo2.NewSet("keel-network", deniedLoader{})
	for _, tag := range []string{"include", "extends", "import", "ssi", "macro", "debug", "set", "now", "cycle", "with"} {
		_ = set.BanTag(tag)
	}
	for _, filter := range []string{"safe", "escapejs", "ljust", "rjust", "center", "join", "stringformat", "make_list", "slice"} {
		_ = set.BanFilter(filter)
	}
	return set
}
func finiteJSON(value any, depth int, nodes *int) error {
	*nodes++
	if depth > 8 || *nodes > 1000 {
		return errors.New("template data exceeds depth or element limits")
	}
	switch v := value.(type) {
	case nil, string, bool, json.Number, float64:
	case []any:
		for _, item := range v {
			if e := finiteJSON(item, depth+1, nodes); e != nil {
				return e
			}
		}
	case map[string]any:
		for key, item := range v {
			if strings.ContainsAny(key, "\x00\r\n") {
				return errors.New("invalid JSON key")
			}
			if e := finiteJSON(item, depth+1, nodes); e != nil {
				return e
			}
		}
	default:
		return errors.New("template context contains a non-JSON type")
	}
	return nil
}
func checkVariable(name string, schema Variable, value any) error {
	ok := false
	switch schema.Type {
	case "string":
		_, ok = value.(string)
	case "boolean":
		_, ok = value.(bool)
	case "number":
		_, ok = value.(json.Number)
	case "integer":
		v, yes := value.(json.Number)
		if yes {
			_, e := v.Int64()
			ok = e == nil
		}
	case "array":
		_, ok = value.([]any)
	case "object":
		_, ok = value.(map[string]any)
	}
	if !ok {
		return fmt.Errorf("variable %s has the wrong type", name)
	}
	if len(schema.Enum) > 0 {
		allowed := false
		for _, v := range schema.Enum {
			if fmt.Sprint(value) == v {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("variable %s is outside its approved enum", name)
		}
	}
	return nil
}

type boundedWriter struct {
	ctx context.Context
	bytes.Buffer
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	if len(p)+w.Len() > MaxRenderedBytes {
		return 0, errors.New("rendered network configuration exceeds size limit")
	}
	return w.Buffer.Write(p)
}
func (w *boundedWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (t Template) Render(ctx context.Context, variables map[string]any, device map[string]any) (json.RawMessage, error) {
	if e := t.Validate(); e != nil {
		return nil, e
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if variables == nil {
		variables = map[string]any{}
	}
	if device == nil {
		device = map[string]any{}
	}
	// Roundtrip strips functions, methods and application structs from the context.
	encoded, e := json.Marshal(map[string]any{"variables": variables, "device": device})
	if e != nil || len(encoded) > 256<<10 {
		return nil, errors.New("invalid or oversized template context")
	}
	var clean map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if e = decoder.Decode(&clean); e != nil {
		return nil, e
	}
	nodes := 0
	if e = finiteJSON(clean, 0, &nodes); e != nil {
		return nil, e
	}
	values := clean["variables"].(map[string]any)
	contextData := pongo2.Context{"device": clean["device"]}
	for name, value := range values {
		schema, ok := t.Variables[name]
		if !ok {
			return nil, fmt.Errorf("variable %s is not declared by the template", name)
		}
		if e = checkVariable(name, schema, value); e != nil {
			return nil, e
		}
		contextData[name] = value
	}
	for name, schema := range t.Variables {
		if _, ok := values[name]; !ok && schema.Required {
			return nil, fmt.Errorf("required variable %s is missing", name)
		}
	}
	tpl, e := sandbox().FromString(t.Source)
	if e != nil {
		return nil, e
	}
	writer := &boundedWriter{ctx: ctx}
	if e = tpl.ExecuteWriterUnbuffered(contextData, writer); e != nil {
		return nil, e
	}
	var object map[string]any
	decoder = json.NewDecoder(bytes.NewReader(writer.Bytes()))
	decoder.UseNumber()
	if e = decoder.Decode(&object); e != nil || object == nil {
		return nil, errors.New("network template must render a JSON configuration object")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, errors.New("network template rendered trailing data")
	}
	return json.Marshal(object)
}

type Difference struct {
	Path          string `json:"path"`
	Before        any    `json:"before"`
	After         any    `json:"after"`
	BeforePresent bool   `json:"before_present"`
	AfterPresent  bool   `json:"after_present"`
	Severity      string `json:"severity"`
}

func StructuredDiff(before, after json.RawMessage) ([]Difference, error) {
	parse := func(raw json.RawMessage) (any, error) {
		var v any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		e := d.Decode(&v)
		if e != nil {
			return nil, e
		}
		var trailing any
		if d.Decode(&trailing) != io.EOF {
			return nil, errors.New("configuration contains trailing JSON")
		}
		if _, ok := v.(map[string]any); !ok {
			return nil, errors.New("configuration must be a JSON object")
		}
		return v, nil
	}
	a, e := parse(before)
	if e != nil {
		return nil, e
	}
	b, e := parse(after)
	if e != nil {
		return nil, e
	}
	out := []Difference{}
	var walk func(string, any, any, bool, bool)
	walk = func(path string, left, right any, leftPresent, rightPresent bool) {
		lm, lok := left.(map[string]any)
		rm, rok := right.(map[string]any)
		if lok && rok {
			keys := map[string]bool{}
			for key := range lm {
				keys[key] = true
			}
			for key := range rm {
				keys[key] = true
			}
			ordered := []string{}
			for key := range keys {
				ordered = append(ordered, key)
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				escaped := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
				lv, lp := lm[key]
				rv, rp := rm[key]
				walk(path+"/"+escaped, lv, rv, lp, rp)
			}
			return
		}
		l, _ := json.Marshal(left)
		r, _ := json.Marshal(right)
		if leftPresent == rightPresent && bytes.Equal(l, r) {
			return
		}
		severity := "Critical"
		for _, prefix := range []string{"/description", "/metadata/description"} {
			if strings.HasPrefix(path, prefix) {
				severity = "Minor"
			}
		}
		for _, prefix := range []string{"/interfaces", "/bgp/neighbors"} {
			if strings.HasPrefix(path, prefix) {
				severity = "Major"
			}
		}
		out = append(out, Difference{Path: path, Before: left, After: right, BeforePresent: leftPresent, AfterPresent: rightPresent, Severity: severity})
	}
	walk("", a, b, true, true)
	return out, nil
}
