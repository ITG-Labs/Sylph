package noderoute

import (
	"encoding/json"
	"reflect"
)

const Masked = "***"

var secretKeys = []string{"secretKey", "privateKey", "password", "pass", "preSharedKey", "psk", "id", "uuid", "auth", "token"}

func MaskValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && s != "" && keyIn(k, secretKeys) {
				x[k] = Masked
				continue
			}
			x[k] = MaskValue(val)
		}
	case []any:
		for i := range x {
			x[i] = MaskValue(x[i])
		}
	}
	return v
}

func MaskRaw(raw json.RawMessage) json.RawMessage {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(MaskValue(v))
	if err != nil {
		return raw
	}
	return out
}

func hasMasked(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && s == Masked && keyIn(k, secretKeys) {
				return true
			}
			if hasMasked(val) {
				return true
			}
		}
	case []any:
		for _, val := range x {
			if hasMasked(val) {
				return true
			}
		}
	}
	return false
}

func RawHasMasked(raw json.RawMessage) bool {
	var v any
	return json.Unmarshal(raw, &v) == nil && hasMasked(v)
}

func SameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
