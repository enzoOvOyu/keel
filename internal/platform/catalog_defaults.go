package platform

import (
	"context"
	"database/sql"
	"fmt"
)

// Resolve the whole project definition before interpreting its value. A retired
// project definition remains a tombstone, including when a global value exists.
// No registered value is different from a deliberately retired definition:
// consumers may use their documented built-in defaults only for the former.
func catalogDefaultsTx(ctx context.Context, tx *sql.Tx, project string, keys ...string) (map[string]string, error) {
	return catalogDefaultsMatchingTx(ctx, tx, project, keys, "")
}

func catalogDefaultsMatchingTx(ctx context.Context, tx *sql.Tx, project string, keys []string, prefix string) (map[string]string, error) {
	if keys == nil {
		keys = []string{}
	}
	rows, e := tx.QueryContext(ctx, `SELECT p.key,p.default_value,p.state FROM param_keys p
 WHERE p.scope_key IN('global',$1) AND (p.key=ANY($2::text[]) OR ($3<>'' AND starts_with(p.key,$3)))
 AND NOT EXISTS(SELECT 1 FROM param_keys override WHERE override.key=p.key AND override.scope_key=$1 AND p.scope_key='global')
 ORDER BY p.key`, "project:"+project, keys, prefix)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value, state string
		if e = rows.Scan(&key, &value, &state); e != nil {
			return nil, e
		}
		if state != "active" {
			return nil, apiError(409, "PARAM_KEY_RETIRED", fmt.Sprintf("Catalog parameter %s is retired; review the project policy before continuing", key))
		}
		out[key] = value
	}
	return out, rows.Err()
}
