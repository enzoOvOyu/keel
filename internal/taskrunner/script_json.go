package taskrunner

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"unicode/utf8"
)

// Keep the complete source and normalized result within the existing receipt
// and gRPC envelopes. Larger documents belong in the artifact subsystem.
const MaxScriptJSONBytes = 2048

func normalizedScriptJSON(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > MaxScriptJSONBytes || !utf8.Valid(raw) {
		return "", ErrRejected
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var value func(int) (any, error)
	value = func(depth int) (any, error) {
		nodes++
		if depth > 8 || nodes > 128 {
			return nil, ErrRejected
		}
		token, err := d.Token()
		if err != nil {
			return nil, ErrRejected
		}
		switch token := token.(type) {
		case json.Delim:
			switch token {
			case '{':
				object := map[string]any{}
				for d.More() {
					keyToken, err := d.Token()
					key, ok := keyToken.(string)
					if err != nil || !ok || len(key) > 128 {
						return nil, ErrRejected
					}
					if _, exists := object[key]; exists {
						return nil, ErrRejected
					}
					child, err := value(depth + 1)
					if err != nil {
						return nil, err
					}
					object[key] = child
				}
				end, err := d.Token()
				if err != nil || end != json.Delim('}') {
					return nil, ErrRejected
				}
				return object, nil
			case '[':
				array := []any{}
				for d.More() {
					child, err := value(depth + 1)
					if err != nil {
						return nil, err
					}
					array = append(array, child)
				}
				end, err := d.Token()
				if err != nil || end != json.Delim(']') {
					return nil, ErrRejected
				}
				return array, nil
			default:
				return nil, ErrRejected
			}
		case json.Number:
			number, err := token.Float64()
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, ErrRejected
			}
			return token, nil
		case string, bool, nil:
			return token, nil
		default:
			return nil, ErrRejected
		}
	}
	root, err := value(0)
	if err != nil {
		return "", err
	}
	if _, ok := root.(map[string]any); !ok {
		return "", ErrRejected
	}
	if _, err = d.Token(); err != io.EOF {
		return "", ErrRejected
	}
	normalized, err := json.Marshal(root)
	if err != nil || len(normalized) > MaxScriptJSONBytes {
		return "", ErrRejected
	}
	return string(normalized), nil
}

func validateScriptJSONObservation(o Observation) error {
	result, hasResult := o.Output["structured_result_json"]
	source, hasSource := o.Output["structured_stdout_base64"]
	if o.Command.ScriptResultFormat != "json" {
		if hasResult || hasSource {
			return ErrRejected
		}
		return nil
	}
	if !hasResult && !hasSource && o.State != "success" {
		return nil
	}
	text, ok := result.(string)
	encoded, sourceOK := source.(string)
	if !hasResult || !hasSource || !ok || !sourceOK {
		return ErrRejected
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(raw) != encoded {
		return ErrRejected
	}
	normalized, err := normalizedScriptJSON(raw)
	if err != nil || normalized != text {
		return ErrRejected
	}
	sum := sha256.Sum256(raw)
	size, ok := scriptOutputInteger(o.Output["stdout_bytes"])
	prefix := raw
	if len(prefix) > 512 {
		prefix = prefix[:512]
	}
	if !ok || size != int64(len(raw)) || o.Output["stdout_complete"] != true || o.Output["stdout_sha256"] != hex.EncodeToString(sum[:]) || o.Output["stdout_prefix_base64"] != base64.StdEncoding.EncodeToString(prefix) {
		return ErrRejected
	}
	return nil
}
