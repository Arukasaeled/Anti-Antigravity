// Package activity projects only observable behaviour from Antigravity 2.19.1.
// The allowlist comes from the running client's native descriptors. In particular,
// planner_response, prompts, signatures and authentication fields are excluded.
package activity

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

//go:embed schema.json
var SchemaJSON string

type field struct {
	name     string
	kind     int
	message  string
	repeated bool
}

var schema = func() map[string]map[int]field {
	var raw map[string]map[int][]json.RawMessage
	if err := json.Unmarshal([]byte(SchemaJSON), &raw); err != nil {
		panic(err)
	}
	result := make(map[string]map[int]field)
	for name, fields := range raw {
		result[name] = make(map[int]field)
		for number, values := range fields {
			if len(values) != 4 {
				panic("invalid activity schema")
			}
			var f field
			if err := json.Unmarshal(values[0], &f.name); err != nil {
				panic(err)
			}
			if err := json.Unmarshal(values[1], &f.kind); err != nil {
				panic(err)
			}
			if err := json.Unmarshal(values[2], &f.message); err != nil {
				panic(err)
			}
			if err := json.Unmarshal(values[3], &f.repeated); err != nil {
				panic(err)
			}
			result[name][number] = f
		}
	}
	return result
}()

var secrets = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/-]+`),
	regexp.MustCompile(`(?i)((?:access_token|refresh_token|id_token|client_secret|authorization|password|cookie)["'\s]*[:=]["'\s]*)[^\s,"';}]+`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`),
}

// SafeText is a bounded display projection, never a raw credential export.
func SafeText(s string) string {
	for _, re := range secrets {
		s = re.ReplaceAllString(s, "${1}[redacted]")
	}
	if len(s) > 32768 {
		s = s[:32768]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		s += "\n[truncated by 2Ag at 32 KiB]"
	}
	return s
}

type wireValue struct {
	number  int
	wire    uint64
	integer uint64
	data    []byte
}

func fields(data []byte) ([]wireValue, error) {
	values := []wireValue{}
	for len(data) > 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 || tag>>3 == 0 {
			return nil, fmt.Errorf("invalid activity protobuf tag")
		}
		data = data[n:]
		v := wireValue{number: int(tag >> 3), wire: tag & 7}
		switch v.wire {
		case 0:
			v.integer, n = binary.Uvarint(data)
			if n <= 0 {
				return nil, fmt.Errorf("invalid activity protobuf integer")
			}
			data = data[n:]
		case 2:
			length, used := binary.Uvarint(data)
			if used <= 0 || length > uint64(len(data)-used) {
				return nil, fmt.Errorf("invalid activity protobuf length")
			}
			data = data[used:]
			v.data = data[:int(length)]
			data = data[int(length):]
		case 1, 5:
			size := 8
			if v.wire == 5 {
				size = 4
			}
			if len(data) < size {
				return nil, fmt.Errorf("invalid activity protobuf fixed field")
			}
			v.data = data[:size]
			data = data[size:]
		default:
			return nil, fmt.Errorf("unsupported activity protobuf wire")
		}
		values = append(values, v)
		if len(values) > 20000 {
			return nil, fmt.Errorf("activity field limit reached")
		}
	}
	return values, nil
}

// Decode drops every field outside the shared allowlist. Any results may only
// contain a native Step; raw bytes and unknown message bodies never reach the UI.
func Decode(data []byte, message string) (map[string]any, error) {
	return decode(data, strings.TrimPrefix(message, "."), 0)
}

func decode(data []byte, message string, depth int) (map[string]any, error) {
	if depth > 14 {
		return nil, fmt.Errorf("activity nesting limit reached")
	}
	definitions, ok := schema[message]
	if !ok {
		return nil, fmt.Errorf("unsupported activity message")
	}
	values, err := fields(data)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if message == "google.protobuf.Any" {
		var url string
		var payload []byte
		for _, v := range values {
			if v.number == 1 {
				url = string(v.data)
			}
			if v.number == 2 {
				payload = v.data
			}
		}
		result["typeUrl"] = SafeText(url)
		if url == "type.googleapis.com/gemini_coder.Step" {
			step, err := decode(payload, "gemini_coder.Step", depth+1)
			if err != nil {
				return nil, err
			}
			result["step"] = step
			if step["projectionPartial"] == true {
				result["projectionPartial"] = true
			}
		}
		return result, nil
	}
	for _, v := range values {
		f, allowed := definitions[v.number]
		if !allowed {
			continue
		}
		var value any
		switch f.kind {
		case 9:
			if v.wire != 2 {
				continue
			}
			value = SafeText(string(v.data))
		case 11:
			if v.wire != 2 {
				continue
			}
			value, err = decode(v.data, f.message, depth+1)
			if err != nil {
				return nil, err
			}
			if child, ok := value.(map[string]any); ok && child["projectionPartial"] == true {
				result["projectionPartial"] = true
			}
		case 8:
			if v.wire != 0 {
				continue
			}
			value = v.integer != 0
		case 5:
			if v.wire != 0 {
				continue
			}
			value = int64(int32(v.integer))
		case 3, 4, 13, 14:
			if v.wire != 0 || v.integer > 9007199254740991 {
				continue
			}
			value = int64(v.integer)
		default:
			continue
		}
		if f.repeated {
			list, _ := result[f.name].([]any)
			if len(list) >= 256 {
				result["projectionPartial"] = true
				continue
			}
			result[f.name] = append(list, value)
		} else {
			result[f.name] = value
		}
	}
	// Generic arguments are stored as separate key/value fields. A bare secret
	// value cannot be detected by SafeText's labeled-text redaction.
	if message == "exa.cortex_pb.CortexStepGeneric.ArgsEntry" {
		key, _ := result["key"].(string)
		key = strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(key))
		switch key {
		case "accesstoken", "refreshtoken", "idtoken", "clientsecret", "authorization", "password", "cookie":
			result["value"] = "[redacted]"
		}
	}
	return result, nil
}
