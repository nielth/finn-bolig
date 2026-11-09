package main

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// decodeTurboStream decodes React Router's "turbo-stream" format, which finn uses for its
// search .data responses and for the loader data embedded in ad pages. The first line is a flat
// JSON array where objects reference keys and values by index; later lines ("P<id>:[...]")
// resolve deferred promises and continue the same index space.
func decodeTurboStream(text string) any {
	lines := strings.Split(text, "\n")
	var values []any
	dec := json.NewDecoder(strings.NewReader(lines[0]))
	dec.UseNumber()
	if err := dec.Decode(&values); err != nil {
		// Not turbo-stream; maybe plain JSON.
		var v any
		if json.Unmarshal([]byte(text), &v) == nil {
			return v
		}
		return nil
	}

	promises := map[string]int{}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "P") {
			continue
		}
		id, payload, ok := strings.Cut(line[1:], ":")
		if !ok {
			continue
		}
		var chunk []any
		d := json.NewDecoder(strings.NewReader(payload))
		d.UseNumber()
		if d.Decode(&chunk) != nil {
			continue
		}
		if len(chunk) == 1 {
			if prev, ok := chunk[0].([]any); ok && len(prev) == 2 && prev[0] == "Z" {
				promises[id] = toInt(prev[1])
				continue
			}
		}
		promises[id] = len(values)
		values = append(values, chunk...)
	}

	t := &turbo{values: values, promises: promises, memo: map[int]any{}}
	return t.hydrate(0)
}

type turbo struct {
	values   []any
	promises map[string]int
	memo     map[int]any
}

func toInt(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	}
	return -7
}

func (t *turbo) hydrate(index int) any {
	switch index {
	case -1, -7:
		return nil
	case -2:
		return math.NaN()
	case -3:
		return math.Inf(-1)
	case -4:
		return 0.0
	case -5:
		return nil
	case -6:
		return math.Inf(1)
	}
	if index < 0 || index >= len(t.values) {
		return nil
	}
	if v, ok := t.memo[index]; ok {
		return v
	}
	switch v := t.values[index].(type) {
	case []any:
		if len(v) > 0 {
			if tag, ok := v[0].(string); ok {
				return t.special(index, tag, v)
			}
		}
		out := make([]any, 0, len(v))
		for _, i := range v {
			out = append(out, t.hydrate(toInt(i)))
		}
		t.memo[index] = out
		return out
	case map[string]any:
		out := map[string]any{}
		t.memo[index] = out
		for k, i := range v {
			key := k
			if strings.HasPrefix(k, "_") {
				if n, err := strconv.Atoi(k[1:]); err == nil && n >= 0 && n < len(t.values) {
					key = toString(t.values[n])
				}
			}
			out[key] = t.hydrate(toInt(i))
		}
		return out
	default:
		t.memo[index] = v
		return v
	}
}

func (t *turbo) special(index int, tag string, v []any) any {
	arg := func(i int) any {
		if i < len(v) {
			return v[i]
		}
		return nil
	}
	switch tag {
	case "D":
		ms, _ := arg(1).(json.Number).Float64()
		return time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339)
	case "P":
		if i, ok := t.promises[toString(arg(1))]; ok {
			return t.hydrate(i)
		}
		return nil
	case "Z":
		return t.hydrate(toInt(arg(1)))
	case "U", "B", "Y", "R":
		return arg(1)
	case "M":
		out := map[string]any{}
		t.memo[index] = out
		for i := 1; i+1 < len(v); i += 2 {
			out[toString(t.hydrate(toInt(v[i])))] = t.hydrate(toInt(v[i+1]))
		}
		return out
	case "S":
		out := []any{}
		for _, i := range v[1:] {
			out = append(out, t.hydrate(toInt(i)))
		}
		return out
	case "N":
		return map[string]any{}
	}
	return nil
}

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return s.String()
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
