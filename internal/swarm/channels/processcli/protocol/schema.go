package protocol

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	jsonschemakind "github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// schemaJSON 打包的完整协议 schema。
// Python: schema.py:15-18 protocol_schema() 从包内读 schema.json
//
//go:embed schema.json
var schemaJSON []byte

var (
	// schemaOnce 保证 schema.json 只解析一次
	schemaOnce sync.Once
	// schemaData 解析后的只读 schema 树
	schemaData map[string]any
	// schemaParseErr 解析错误
	schemaParseErr error
	// validators 按 kind 缓存的校验器。
	// Python: schema.py:23-26 @lru_cache(maxsize=3) _validator(kind)
	validators sync.Map
	// validatorErrs 按 kind 缓存的构造错误
	validatorErrs sync.Map
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ProtocolSchema 返回打包的完整协议 schema 的新副本。
// Python: schema.py:15-18 protocol_schema()
func ProtocolSchema() map[string]any {
	loadSchema()
	if schemaData == nil {
		return map[string]any{}
	}
	// 深拷贝，避免调用方修改污染内部状态
	if thawed, ok := ThawJson(schemaData).(map[string]any); ok {
		return thawed
	}
	return map[string]any{}
}

// InputErrorDetails 定位被拒输入，不回显值或敏感 key。
// Python: schema.py:38-63 input_error_details
//
// DTO 校验仍是权威；schema 诊断只在其失败时求值，保持既有成功输入与错误行为。
func InputErrorDetails(value any, schemaKind string) map[string]any {
	validator, err := validatorFor(schemaKind)
	if err != nil || validator == nil {
		return map[string]any{"field": "/", "reason": "invalid_value"}
	}
	verr := validator.Validate(value)
	if verr == nil {
		return map[string]any{"field": "/", "reason": "invalid_value"}
	}
	validationErr, ok := verr.(*jsonschema.ValidationError)
	if !ok {
		return map[string]any{"field": "/", "reason": "invalid_value"}
	}

	// Python: :52-58 沿 context 取 absolute_path 最长、schema 不为 {"type":"null"} 的子错误
	deepest := deepestValidationError(validationErr)
	path := append([]string{}, deepest.InstanceLocation...)

	// Python: :53-56 validator == "required" 时把缺失的第一个 key 追加到 path
	if required, isRequired := deepest.ErrorKind.(*jsonschemakind.Required); isRequired {
		if len(required.Missing) > 0 {
			path = append(path, required.Missing[0])
		}
	}

	// Python: :57-59 field = "/" + "/".join(转义后的段)
	field := "/"
	if len(path) > 0 {
		escaped := make([]string, 0, len(path))
		for _, segment := range path {
			escaped = append(escaped, escapeJSONPointer(segment))
		}
		field = "/" + strings.Join(escaped, "/")
	}

	// Python: :60-62 reason 映射
	result := map[string]any{
		"field":  field,
		"reason": reasonForKind(deepest.ErrorKind),
	}

	// Python: :63 validator == "type" 时追加 expected
	if typeKind, isType := deepest.ErrorKind.(*jsonschemakind.Type); isType {
		expected := make([]any, len(typeKind.Want))
		for i, want := range typeKind.Want {
			expected[i] = want
		}
		result["expected"] = expected
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadSchema 解析 schema.json（仅一次）。
func loadSchema() {
	schemaOnce.Do(func() {
		if err := json.Unmarshal(schemaJSON, &schemaData); err != nil {
			schemaParseErr = err
		}
	})
}

// validatorFor 取指定 $defs 项的校验器。
// Python: schema.py:23-26
//
//	Draft202012Validator({"$ref": f"#/$defs/{kind}", "$defs": schema["$defs"]})
func validatorFor(schemaKind string) (*jsonschema.Schema, error) {
	if cached, ok := validators.Load(schemaKind); ok {
		return cached.(*jsonschema.Schema), nil
	}
	if cachedErr, ok := validatorErrs.Load(schemaKind); ok {
		return nil, cachedErr.(error)
	}
	compile := func() (*jsonschema.Schema, error) {
		loadSchema()
		if schemaParseErr != nil {
			return nil, schemaParseErr
		}
		defs, _ := schemaData["$defs"].(map[string]any)
		if defs == nil {
			return nil, fmt.Errorf("schema.json missing $defs")
		}
		document := map[string]any{
			"$ref":  "#/$defs/" + schemaKind,
			"$defs": defs,
		}
		raw, err := json.Marshal(document)
		if err != nil {
			return nil, err
		}
		decoded, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		compiler := jsonschema.NewCompiler()
		url := "mem://processcli/" + schemaKind + ".json"
		if err := compiler.AddResource(url, decoded); err != nil {
			return nil, err
		}
		return compiler.Compile(url)
	}
	schema, err := compile()
	if err != nil {
		validatorErrs.Store(schemaKind, err)
		return nil, err
	}
	validators.Store(schemaKind, schema)
	return schema, nil
}

// deepestValidationError 取 absolute_path 最深、且 schema 不为 {"type":"null"} 的错误。
// Python: schema.py:52-58
//
//	max(error.context, key=lambda item: (len(item.absolute_path),
//	                                    item.schema != {"type": "null"}))
func deepestValidationError(err *jsonschema.ValidationError) *jsonschema.ValidationError {
	current := err
	for len(current.Causes) > 0 {
		best := current.Causes[0]
		for _, candidate := range current.Causes[1:] {
			if len(candidate.InstanceLocation) > len(best.InstanceLocation) {
				best = candidate
				continue
			}
			if len(candidate.InstanceLocation) == len(best.InstanceLocation) &&
				!isNullOnlyKind(candidate.ErrorKind) {
				best = candidate
			}
		}
		current = best
	}
	return current
}

// isNullOnlyKind 该错误的 schema 是否仅为 {"type": "null"}。
func isNullOnlyKind(kind jsonschema.ErrorKind) bool {
	typeKind, ok := kind.(*jsonschemakind.Type)
	if !ok {
		return false
	}
	return len(typeKind.Want) == 1 && typeKind.Want[0] == "null"
}

// reasonForKind 把 ErrorKind 映射为稳定的 reason 字符串。
// Python: schema.py:60-62 reasons 映射
func reasonForKind(kind jsonschema.ErrorKind) string {
	switch kind.(type) {
	case *jsonschemakind.Type:
		return "invalid_type"
	case *jsonschemakind.Required:
		return "required"
	case *jsonschemakind.AdditionalProperties:
		return "unknown_field"
	default:
		return "invalid_value"
	}
}

// escapeJSONPointer JSON Pointer 段转义。
// Python: schema.py:58-59 replace("~","~0").replace("/","~1")
func escapeJSONPointer(segment string) string {
	out := strings.ReplaceAll(segment, "~", "~0")
	return strings.ReplaceAll(out, "/", "~1")
}
