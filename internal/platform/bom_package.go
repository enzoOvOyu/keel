package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// BOMPackage is the validated, still read-only representation of one standard
// directory package. File bytes are supplied by the transport after it has
// rejected archive links, traversal and duplicate entries.
type BOMPackage struct {
	Manifest   BOMPackageManifest
	Rows       map[string][]BOMPackageRow
	FileHashes map[string]string
	Issues     []BOMPackageIssue
}

type BOMPackageManifest struct {
	SchemaVersion      string           `json:"schema_version"`
	BatchID            string           `json:"bom_import_batch_id"`
	ProjectCode        string           `json:"project_code"`
	ProjectID          string           `json:"project_id"`
	BatchDate          string           `json:"batch_date"`
	Seq                int              `json:"seq"`
	DeclaredBy         string           `json:"declared_by"`
	ProfileVersionRef  string           `json:"profile_version_ref"`
	DomainFiles        []BOMPackageFile `json:"domain_files"`
	NetworkBaselineRef string           `json:"network_baseline_ref"`
	CredentialBatchRef string           `json:"credential_batch_ref"`
	ContentHash        string           `json:"content_hash"`
	RowCount           int              `json:"row_count"`
}

type BOMPackageFile struct {
	Domain  string   `json:"domain"`
	File    string   `json:"file"`
	Entity  string   `json:"entity"`
	Columns []string `json:"columns"`
}

type BOMPackageRow struct {
	File   string
	Line   int
	Values map[string]string
}

type BOMPackageIssue struct {
	Severity  string `json:"severity"`
	File      string `json:"file"`
	BOMLineNo int    `json:"bom_line_no,omitempty"`
	Field     string `json:"field,omitempty"`
	Code      string `json:"code"`
}

var requiredBOMFiles = map[string]struct{ domain, entity string }{
	"server/servers.csv":           {"server", "server"},
	"server/server_components.csv": {"server", "component"},
	"server/server_ports.csv":      {"server", "port"},
	"fabric/switches.csv":          {"fabric", "switch"},
	"fabric/switch_ports.csv":      {"fabric", "port"},
	"fabric/optic_modules.csv":     {"fabric", "optic_module"},
	"fabric/cables.csv":            {"fabric", "cable"},
}
var packageBatchID = regexp.MustCompile(`^BOM-BATCH-[A-Za-z0-9_-]+-[0-9]{8}-[0-9]+$`)
var packageHash = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func packagePath(name string) bool {
	return name != "" && name != "." && !strings.HasPrefix(name, "/") && !strings.Contains(name, "\\") && path.Clean(name) == name && !strings.HasPrefix(name, "../") && name != ".." && !strings.Contains(name, "//")
}

// BOMContentHash follows PRD A.6 exactly: sorted relative path, NUL, decimal
// byte length, NUL, raw-file SHA256 hex, LF. manifest.json is excluded.
func BOMContentHash(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		if name != "manifest.json" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		digest := sha256.Sum256(files[name])
		fmt.Fprintf(h, "%s\x00%d\x00%s\n", name, len(files[name]), hex.EncodeToString(digest[:]))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// ValidateBOMPackage performs no writes. Database-backed uniqueness, model,
// project-template and external-item checks must run in the preview transaction
// before commit; this function checks the portable package contract.
func ValidateBOMPackage(files map[string][]byte) (*BOMPackage, error) {
	out := &BOMPackage{Rows: map[string][]BOMPackageRow{}, FileHashes: map[string]string{}, Issues: []BOMPackageIssue{}}
	add := func(sev, file, field, code string, line int) {
		out.Issues = append(out.Issues, BOMPackageIssue{Severity: sev, File: file, Field: field, Code: code, BOMLineNo: line})
	}
	for name, data := range files {
		if !packagePath(name) {
			add("BLOCK", name, "", "invalid_path", 0)
		}
		if !utf8.Valid(data) || bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
			add("BLOCK", name, "", "invalid_utf8_or_bom", 0)
		}
		if bytes.Contains(data, []byte("-----BEGIN PRIVATE KEY-----")) || bytes.Contains(data, []byte("-----BEGIN OPENSSH PRIVATE KEY-----")) {
			add("BLOCK", name, "", "plaintext_secret", 0)
		}
	}
	raw, ok := files["manifest.json"]
	if !ok {
		add("BLOCK", "manifest.json", "", "missing_manifest", 0)
		return out, nil
	}
	if e := json.Unmarshal(raw, &out.Manifest); e != nil {
		return nil, fmt.Errorf("manifest.json: %w", e)
	}
	m := out.Manifest
	if m.SchemaVersion != "bom-import/v1" {
		add("BLOCK", "manifest.json", "schema_version", "unsupported_schema", 0)
	}
	if !packageBatchID.MatchString(m.BatchID) {
		add("BLOCK", "manifest.json", "bom_import_batch_id", "invalid_batch_id", 0)
	}
	for key, value := range map[string]string{"project_id": m.ProjectID, "project_code": m.ProjectCode, "profile_version_ref": m.ProfileVersionRef, "declared_by": m.DeclaredBy} {
		if strings.TrimSpace(value) == "" {
			add("BLOCK", "manifest.json", key, "required", 0)
		}
	}
	if m.Seq < 1 || m.RowCount < 0 {
		add("BLOCK", "manifest.json", "seq", "invalid_number", 0)
	}
	if !packageHash.MatchString(m.ContentHash) {
		add("BLOCK", "manifest.json", "content_hash", "invalid_hash", 0)
	}
	declared := map[string]BOMPackageFile{}
	for _, f := range m.DomainFiles {
		if !packagePath(f.File) || f.File == "manifest.json" {
			add("BLOCK", "manifest.json", "domain_files", "invalid_file_path", 0)
			continue
		}
		if _, exists := declared[f.File]; exists {
			add("BLOCK", "manifest.json", "domain_files", "duplicate_file", 0)
		}
		declared[f.File] = f
		if want, required := requiredBOMFiles[f.File]; required && (want.domain != f.Domain || want.entity != f.Entity) {
			add("BLOCK", "manifest.json", "domain_files", "wrong_domain_or_entity", 0)
		}
		if _, exists := files[f.File]; !exists {
			add("BLOCK", f.File, "", "missing_declared_file", 0)
		}
		if len(f.Columns) == 0 {
			add("BLOCK", f.File, "columns", "missing_columns", 0)
		}
	}
	for name := range requiredBOMFiles {
		if _, ok := declared[name]; !ok {
			add("BLOCK", name, "", "required_file_not_declared", 0)
		}
	}
	for name := range files {
		if name != "manifest.json" {
			if _, ok := declared[name]; !ok {
				add("BLOCK", name, "", "undeclared_file", 0)
			}
		}
	}
	// A dishonest manifest cannot declare a subset of the package and obtain the
	// same digest: every actual file participates, and undeclared files block.
	if computed := BOMContentHash(files); m.ContentHash != computed {
		add("BLOCK", "manifest.json", "content_hash", "hash_mismatch", 0)
	}
	lineSeen := map[int]string{}
	macSeen := map[string]string{}
	totalRows := 0
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data, ok := files[name]
		if !ok || !strings.HasSuffix(name, ".csv") {
			continue
		}
		reader := csv.NewReader(bytes.NewReader(data))
		reader.FieldsPerRecord = -1
		rows, e := reader.ReadAll()
		if e != nil {
			add("BLOCK", name, "", "invalid_csv", 0)
			continue
		}
		if len(rows) == 0 {
			add("BLOCK", name, "", "missing_header", 0)
			continue
		}
		header := rows[0]
		indexes := map[string]int{}
		for i, col := range header {
			col = strings.TrimSpace(col)
			if col == "" {
				add("BLOCK", name, "", "empty_header", 0)
			}
			if _, duplicate := indexes[col]; duplicate {
				add("BLOCK", name, col, "duplicate_column", 0)
			}
			indexes[col] = i
		}
		wanted := map[string]bool{}
		for _, col := range declared[name].Columns {
			wanted[col] = true
			if _, present := indexes[col]; !present {
				add("BLOCK", name, col, "missing_declared_column", 0)
			}
		}
		for col := range indexes {
			if !wanted[col] {
				add("WARN", name, col, "unmapped_column", 0)
			}
		}
		_, entityFile := requiredBOMFiles[name]
		if _, present := indexes["bom_line_no"]; entityFile && !present {
			add("BLOCK", name, "bom_line_no", "missing_line_number", 0)
		}
		for i, record := range rows[1:] {
			totalRows++
			if len(record) != len(header) {
				add("BLOCK", name, "", "column_count_mismatch", i+2)
				continue
			}
			values := map[string]string{}
			for j, col := range header {
				value := strings.TrimSpace(record[j])
				values[strings.TrimSpace(col)] = value
				upper := strings.ToUpper(value)
				if upper == "N/A" || upper == "NULL" || value == "-" {
					add("WARN", name, col, "placeholder_value", i+2)
				}
				if strings.Contains(strings.ToLower(col), "password") || strings.Contains(strings.ToLower(col), "private_key") || strings.Contains(strings.ToLower(col), "token") {
					if value != "" {
						add("BLOCK", name, col, "plaintext_secret", i+2)
					}
				}
			}
			line, e := strconv.Atoi(values["bom_line_no"])
			if !entityFile {
				line = i + 2
			} else if e != nil || line < 1 {
				add("BLOCK", name, "bom_line_no", "invalid_line_number", i+2)
				line = 0
			} else if prior, exists := lineSeen[line]; entityFile && exists {
				add("BLOCK", name, "bom_line_no", "duplicate_line_number_with_"+prior, line)
			} else if entityFile {
				lineSeen[line] = name
			}
			if name == "server/server_ports.csv" {
				mac := values["mac"]
				if mac != "" {
					parsed, e := net.ParseMAC(mac)
					if e != nil || len(parsed) != 6 {
						add("BLOCK", name, "mac", "invalid_mac", line)
					} else {
						normalized := strings.ToLower(parsed.String())
						if _, duplicate := macSeen[normalized]; duplicate {
							add("BLOCK", name, "mac", "duplicate_mac", line)
						}
						macSeen[normalized] = name
						values["mac"] = normalized
					}
				}
			}
			out.Rows[name] = append(out.Rows[name], BOMPackageRow{File: name, Line: line, Values: values})
		}
		digest := sha256.Sum256(data)
		out.FileHashes[name] = hex.EncodeToString(digest[:])
	}
	if totalRows != m.RowCount {
		add("BLOCK", "manifest.json", "row_count", "row_count_mismatch", 0)
	}
	return out, nil
}
