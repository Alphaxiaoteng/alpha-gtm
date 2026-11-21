package mcp

import "fmt"

func argString(args map[string]any, key, def string) string {
	if v, ok := args[key]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		s := fmt.Sprint(v)
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return def
}

func argBool(args map[string]any, key string, def bool) bool {
	if v, ok := args[key]; ok {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			return b == "true" || b == "1" || b == "yes"
		}
	}
	return def
}

func argInt64(args map[string]any, key string) (int64, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	default:
		var i int64
		_, err := fmt.Sscan(fmt.Sprint(v), &i)
		return i, err == nil
	}
}
