// Package assetview projects retained inventory. Paths are a bounded JSONPath
// subset, not executable expressions; cells never become validation evidence.
package assetview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid inventory view")
var ErrLimit = errors.New("inventory projection exceeds bounded result")
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
var paramPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,119}$`)

type ColorRule struct {
	BaselineKey string `json:"baseline_key"`
	Operator    string `json:"operator"`
	Tone        string `json:"tone"`
}
type Column struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Source string     `json:"source"`
	Path   string     `json:"path"`
	Color  *ColorRule `json:"color,omitempty"`
}
type Definition struct {
	Columns       []Column `json:"columns"`
	SortColumn    string   `json:"sort_column"`
	SortDirection string   `json:"sort_direction"`
	GroupColumn   string   `json:"group_column"`
}
type segment struct {
	key      string
	index    int
	wildcard bool
	property bool
}

// Supports $, .name, ["quoted key"], ['quoted key'], [index], [*].
// Recursive descent, filters, slices, expressions and object wildcards reject.
func parse(path string) ([]segment, error) {
	if len(path) < 1 || len(path) > 512 || path[0] != '$' {
		return nil, ErrInvalid
	}
	out := []segment{}
	for at := 1; at < len(path); {
		if len(out) >= 16 {
			return nil, ErrInvalid
		}
		if path[at] == '.' {
			at++
			start := at
			for at < len(path) && ((path[at] >= 'a' && path[at] <= 'z') || (path[at] >= 'A' && path[at] <= 'Z') || (path[at] >= '0' && path[at] <= '9') || path[at] == '_' || path[at] == '-') {
				at++
			}
			if start == at {
				return nil, ErrInvalid
			}
			out = append(out, segment{key: path[start:at], property: true})
			continue
		}
		if path[at] != '[' {
			return nil, ErrInvalid
		}
		at++
		if at >= len(path) {
			return nil, ErrInvalid
		}
		if path[at] == '*' {
			at++
			if at >= len(path) || path[at] != ']' {
				return nil, ErrInvalid
			}
			at++
			out = append(out, segment{wildcard: true})
			continue
		}
		if path[at] == '\'' || path[at] == '"' {
			quote := path[at]
			at++
			var key strings.Builder
			closed := false
			for at < len(path) {
				c := path[at]
				at++
				if c == quote {
					closed = true
					break
				}
				if c == '\\' {
					if at >= len(path) || (path[at] != quote && path[at] != '\\') {
						return nil, ErrInvalid
					}
					c = path[at]
					at++
				}
				if c < 32 {
					return nil, ErrInvalid
				}
				key.WriteByte(c)
			}
			if !closed || key.Len() == 0 || key.Len() > 255 || at >= len(path) || path[at] != ']' {
				return nil, ErrInvalid
			}
			at++
			out = append(out, segment{key: key.String(), property: true})
			continue
		}
		start := at
		for at < len(path) && path[at] >= '0' && path[at] <= '9' {
			at++
		}
		if start == at || at >= len(path) || path[at] != ']' {
			return nil, ErrInvalid
		}
		n, e := strconv.Atoi(path[start:at])
		if e != nil || n > 4095 {
			return nil, ErrInvalid
		}
		at++
		out = append(out, segment{index: n})
	}
	return out, nil
}
func (d Definition) Validate() error {
	if len(d.Columns) < 1 || len(d.Columns) > 16 || (d.SortDirection != "asc" && d.SortDirection != "desc") {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, c := range d.Columns {
		if !keyPattern.MatchString(c.ID) || seen[c.ID] || len(strings.TrimSpace(c.Title)) < 1 || utf8.RuneCountInString(c.Title) > 100 || (c.Source != "asset" && c.Source != "redfish" && c.Source != "network") {
			return ErrInvalid
		}
		seen[c.ID] = true
		if _, e := parse(c.Path); e != nil {
			return e
		}
		if c.Color != nil {
			r := c.Color
			if !paramPattern.MatchString(r.BaselineKey) || (r.Operator != "eq" && r.Operator != "ne") || (r.Tone != "danger" && r.Tone != "warning" && r.Tone != "success") {
				return ErrInvalid
			}
		}
	}
	if d.SortColumn != "" && !seen[d.SortColumn] || d.GroupColumn != "" && !seen[d.GroupColumn] {
		return ErrInvalid
	}
	return nil
}
func Decode(raw []byte) (any, error) {
	if len(raw) == 0 || len(raw) > 2<<20 {
		return nil, ErrLimit
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := d.Decode(&v); e != nil {
		return nil, e
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	return v, nil
}
func Project(root any, path string) ([]any, error) {
	segments, e := parse(path)
	if e != nil {
		return nil, e
	}
	current := []any{root}
	visits := 0
	for _, s := range segments {
		next := []any{}
		for _, v := range current {
			visits++
			if visits > 8192 {
				return nil, ErrLimit
			}
			if s.property {
				if object, ok := v.(map[string]any); ok {
					if value, ok := object[s.key]; ok {
						next = append(next, value)
					}
				}
			} else if array, ok := v.([]any); ok {
				if s.wildcard {
					if len(array) > 4096 {
						return nil, ErrLimit
					}
					next = append(next, array...)
				} else if s.index < len(array) {
					next = append(next, array[s.index])
				}
			}
			if len(next) > 4096 {
				return nil, ErrLimit
			}
		}
		current = next
	}
	if len(current) > 32 {
		return nil, ErrLimit
	}
	size := 0
	for _, v := range current {
		raw, _ := json.Marshal(v)
		size += len(raw)
	}
	if size > 4096 {
		return nil, ErrLimit
	}
	return current, nil
}

type Baseline struct {
	Key     string `json:"key"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Value   string `json:"value"`
	State   string `json:"state"`
}
type Cell struct {
	Values   []any     `json:"values"`
	State    string    `json:"state"`
	Tone     string    `json:"tone"`
	Baseline *Baseline `json:"baseline,omitempty"`
}

func Evaluate(column Column, root any, sourceState string, baseline *Baseline) Cell {
	out := Cell{Values: []any{}, State: sourceState, Tone: "neutral"}
	if root == nil {
		return out
	}
	values, e := Project(root, column.Path)
	if e != nil {
		out.State = "projection_limit"
		return out
	}
	if len(values) == 0 {
		out.State = "missing"
		return out
	}
	out.Values = values
	if column.Color == nil {
		return out
	}
	out.Baseline = baseline
	if baseline == nil || baseline.State != "active" {
		out.Tone = "unknown"
		return out
	}
	// Only scalar facts compare with catalog strings. Objects/null remain unknown.
	match := true
	for _, v := range values {
		var text string
		switch value := v.(type) {
		case string:
			text = value
		case json.Number:
			text = string(value)
		case bool:
			text = strconv.FormatBool(value)
		case float64:
			text = strconv.FormatFloat(value, 'g', -1, 64)
		default:
			out.Tone = "unknown"
			return out
		}
		if text != baseline.Value {
			match = false
		}
	}
	if sourceState != "current" {
		out.Tone = "unknown"
		return out
	}
	if column.Color.Operator == "eq" && match || column.Color.Operator == "ne" && !match {
		out.Tone = column.Color.Tone
	}
	return out
}
func Text(c Cell) string {
	if len(c.Values) == 1 {
		if s, ok := c.Values[0].(string); ok {
			return s
		}
	}
	if len(c.Values) == 0 {
		return ""
	}
	raw, _ := json.Marshal(c.Values)
	return string(raw)
}

type Row struct {
	ID       string            `json:"id"`
	DeviceID string            `json:"device_id"`
	Kind     string            `json:"kind"`
	Name     string            `json:"name"`
	Serial   string            `json:"serial"`
	Group    string            `json:"group"`
	Cells    map[string]Cell   `json:"cells"`
	Sources  map[string]Source `json:"sources"`
}
type Source struct {
	ID          string `json:"id"`
	Hash        string `json:"hash"`
	Origin      string `json:"origin"`
	State       string `json:"state"`
	CollectedAt string `json:"collected_at"`
	ValidUntil  string `json:"valid_until"`
}

func Sort(rows []Row, d Definition) {
	compare := func(a, b Cell) int {
		if len(a.Values) == 1 && len(b.Values) == 1 {
			av, aok := a.Values[0].(json.Number)
			bv, bok := b.Values[0].(json.Number)
			if aok && bok {
				af, ae := av.Float64()
				bf, be := bv.Float64()
				if ae == nil && be == nil {
					if af < bf {
						return -1
					}
					if af > bf {
						return 1
					}
					return 0
				}
			}
		}
		return strings.Compare(Text(a), Text(b))
	}
	for i := range rows {
		if d.GroupColumn != "" {
			rows[i].Group = Text(rows[i].Cells[d.GroupColumn])
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if d.SortColumn != "" {
			av, bv := a.Cells[d.SortColumn], b.Cells[d.SortColumn]
			am, bm := len(av.Values) == 0, len(bv.Values) == 0
			if am != bm {
				return !am
			}
			cmp := compare(av, bv)
			if cmp != 0 {
				if d.SortDirection == "desc" {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return fmt.Sprint(a.Kind, a.ID) < fmt.Sprint(b.Kind, b.ID)
	})
}
