package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

type networkVariableCatalog struct {
	types      map[string]string
	arch       map[string]string
	selections map[string]CatalogValidation
}

func newNetworkVariableCatalogTx(ctx context.Context, tx *sql.Tx, project string, definition netbaseline.Template, lock bool) (*networkVariableCatalog, error) {
	c := &networkVariableCatalog{types: map[string]string{}, arch: map[string]string{}, selections: map[string]CatalogValidation{}}
	keys := []string{}
	seen := map[string]bool{}
	for _, schema := range definition.Variables {
		if schema.CatalogKey != "" && !seen[schema.CatalogKey] {
			keys = append(keys, schema.CatalogKey)
			seen[schema.CatalogKey] = true
		}
	}
	if len(keys) == 0 {
		return c, nil
	}
	query := `SELECT p.key,p.value_type FROM param_keys p WHERE p.scope_key IN('global',$1) AND p.key=ANY($2::text[]) AND NOT EXISTS(SELECT 1 FROM param_keys o WHERE o.key=p.key AND o.scope_key=$1 AND p.scope_key='global') ORDER BY p.key`
	if lock {
		query += " FOR SHARE OF p"
	}
	rows, e := tx.QueryContext(ctx, query, "project:"+project, keys)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var key, typ string
		if e = rows.Scan(&key, &typ); e != nil {
			return nil, e
		}
		c.types[key] = typ
	}
	return c, rows.Err()
}
func (c *networkVariableCatalog) resolve(ctx context.Context, tx *sql.Tx, project string, definition netbaseline.Template, variables map[string]any, model string, lock bool) (map[string]string, map[string]any, []CatalogSelection, error) {
	requested := map[string]any{}
	approved := map[string]string{}
	resolved := map[string]any{}
	for name, value := range variables {
		resolved[name] = value
	}
	needsArch := false
	for name, schema := range definition.Variables {
		if schema.CatalogKey == "" {
			continue
		}
		value, present := variables[name]
		if !present {
			continue
		}
		ref, ok := value.(map[string]any)
		key, keyOK := ref["param_key"].(string)
		selected, valueOK := ref["value"].(string)
		if !ok || len(ref) != 2 || !keyOK || !valueOK || key != schema.CatalogKey || selected == "" {
			return nil, nil, nil, apiError(409, "NETWORK_CATALOG_REFERENCE_REQUIRED", fmt.Sprintf("Variable %s requires a catalog param_key/value reference", name))
		}
		typ, exists := c.types[key]
		if !exists {
			return nil, nil, nil, apiError(409, "PARAM_KEY_NOT_FOUND", "A referenced template catalog key is not registered")
		}
		if schema.Type == "version" && typ != "version" {
			return nil, nil, nil, apiError(409, "NETWORK_CATALOG_VERSION_REQUIRED", "Version variables require a media-backed version catalog key")
		}
		if other, exists := requested[key]; exists && other != selected {
			return nil, nil, nil, apiError(409, "NETWORK_CATALOG_REFERENCE_CONFLICT", "One template cannot select different values for the same catalog key")
		}
		requested[key] = selected
		approved[key] = selected
		resolved[name] = selected
		needsArch = needsArch || typ == "version"
	}
	if len(requested) == 0 {
		return approved, resolved, nil, nil
	}
	if needsArch {
		arch, ok := c.arch[model]
		if !ok {
			e := tx.QueryRowContext(ctx, `SELECT coalesce(hw_spec->>'cpu_arch','') FROM device_model_versions WHERE id=$1`, model).Scan(&arch)
			if errors.Is(e, sql.ErrNoRows) {
				return nil, nil, nil, apiError(409, "NETWORK_CATALOG_ARCH_REQUIRED", "Version selection requires the approved device model CPU architecture")
			}
			if e != nil {
				return nil, nil, nil, e
			}
			c.arch[model] = arch
		}
		if arch != "amd64" && arch != "arm64" && arch != "noarch" {
			return nil, nil, nil, apiError(409, "NETWORK_CATALOG_ARCH_REQUIRED", "Version selection requires an explicit registered model CPU architecture")
		}
		if selected, ok := requested["cpu.arch"]; ok && selected != arch {
			return nil, nil, nil, apiError(409, "NETWORK_CATALOG_ARCH_MISMATCH", "Catalog CPU architecture differs from the current device model")
		}
		requested["cpu.arch"] = arch
	}
	cacheKey := netbaseline.ContentHash(requested)
	validation, ok := c.selections[cacheKey]
	if !ok {
		var e error
		validation, e = validateCatalogSelectionsTx(ctx, tx, project, requested, lock)
		if e != nil {
			return nil, nil, nil, e
		}
		c.selections[cacheKey] = validation
	}
	if !validation.Valid {
		details := []string{}
		for _, issue := range validation.Issues {
			details = append(details, issue.Key+":"+issue.Code)
		}
		return nil, nil, nil, &Error{Status: 409, Code: "NETWORK_CATALOG_SELECTION_REJECTED", Message: "Template catalog selection or registered compatibility is no longer valid", Details: details}
	}
	return approved, resolved, validation.Selections, nil
}
