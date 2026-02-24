package common

import (
	"fmt"
	"strconv"
	"strings"
)

func ToInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int16:
		return int(val)
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(val))
		return n
	}
	return 0
}

func ToInt16(v any) int16 {
	switch val := v.(type) {
	case string:
		var i int16
		_, _ = fmt.Sscanf(val, "%d", &i)
		return i
	case float64:
		return int16(val)
	case float32:
		return int16(val)
	case int:
		return int16(val)
	case int16:
		return val
	case int32:
		return int16(val)
	case int64:
		return int16(val)
	case uint:
		return int16(val)
	case uint16:
		return int16(val)
	case uint32:
		return int16(val)
	case uint64:
		return int16(val)
	}
	return 0
}

func ToInt32(v any) int32 {
	switch val := v.(type) {
	case string:
		i, _ := strconv.ParseInt(val, 10, 32)
		return int32(i)
	case float64:
		return int32(val)
	case int:
		return int32(val)
	case int32:
		return val
	}
	return 0
}

func ToInt64(v any) int64 {
	switch val := v.(type) {
	case string:
		var i int64
		_, _ = fmt.Sscanf(val, "%d", &i)
		return i
	case float64:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	}
	return 0
}

func ToBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		s := strings.TrimSpace(strings.ToLower(val))
		return s == "true" || s == "1" || s == "yes"
	case int:
		return val != 0
	case int32:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	}
	return false
}

func ToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

func TrimRunes(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if max <= 0 || len(r) <= max {
		return strings.TrimSpace(s)
	}
	return string(r[:max]) + "…"
}
