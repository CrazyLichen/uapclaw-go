package protocol

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OneShotQueryInput 一份文档、一个 Runtime 生命周期、一个只读操作。
// Python: query.py:42-124 OneShotQueryInput
type OneShotQueryInput struct {
	// SchemaVersion 协议版本
	SchemaVersion string
	// Operation 查询操作
	Operation string
	// Params 操作参数
	Params JsonObject
	// RequestID 请求标识
	RequestID *string
	// Workspace 工作区
	Workspace *WorkspaceSpec
	// TimeoutSeconds 超时秒数
	TimeoutSeconds *float64
}

// OneShotQueryResult 终态查询结果，仅在 Runtime 关闭后发布。
// Python: query.py:127-166 OneShotQueryResult
type OneShotQueryResult struct {
	// RequestID 请求标识
	RequestID string
	// Operation 查询操作
	Operation *string
	// Status 终态
	Status string
	// ExitCode 退出码
	ExitCode int
	// Data 查询结果数据
	Data JsonObject
	// Error 错误详情
	Error *RuntimeErrorInfo
}

// queryFields 单个操作允许与必需的参数集合。
// Python: query.py:28-39 QUERY_FIELDS
type queryFields struct {
	// Allowed 允许的参数
	Allowed map[string]struct{}
	// Required 必需的参数
	Required map[string]struct{}
}

// ──────────────────────────── 全局变量 ────────────────────────────

// queryFieldSpecs 10 个只读操作的参数契约。
// Python: query.py:28-39 QUERY_FIELDS
var queryFieldSpecs = map[string]queryFields{
	"protocol.capabilities": {Allowed: setOf(), Required: setOf()},
	"protocol.schema":       {Allowed: setOf(), Required: setOf()},
	"session.get":           {Allowed: setOf("session_id"), Required: setOf("session_id")},
	"session.list":          {Allowed: setOf("limit", "offset", "search"), Required: setOf()},
	"model.list":            {Allowed: setOf(), Required: setOf()},
	"model.resolve":         {Allowed: setOf("requested"), Required: setOf("requested")},
	"mode.list":             {Allowed: setOf(), Required: setOf()},
	"mode.resolve":          {Allowed: setOf("requested"), Required: setOf("requested")},
	"permission.get":        {Allowed: setOf("session_id"), Required: setOf()},
	"mcp.validate":          {Allowed: setOf("references"), Required: setOf("references")},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// QueryOperations 返回全部支持的操作名（用于 capabilities 自描述）。
func QueryOperations() []string {
	out := make([]string, 0, len(queryFieldSpecs))
	for op := range queryFieldSpecs {
		out = append(out, op)
	}
	return out
}

// OneShotQueryInputFromDict 解码一个严格的 type=query 记录。
// Python: query.py:94-124 OneShotQueryInput.from_dict
func OneShotQueryInputFromDict(value any) (*OneShotQueryInput, error) {
	data, err := strictObject("query", value,
		map[string]struct{}{
			"schema_version": {}, "type": {}, "operation": {},
			"params": {}, "request_id": {}, "workspace": {}, "timeout_seconds": {},
		},
		map[string]struct{}{"schema_version": {}, "type": {}, "operation": {}})
	if err != nil {
		return nil, err
	}
	version, verr := RequireSupportedSchemaVersion(data["schema_version"])
	if verr != nil {
		return nil, fmt.Errorf("schema_version is not supported")
	}
	if asString(data["type"]) != "query" {
		return nil, fmt.Errorf("query type must be query")
	}
	in := &OneShotQueryInput{
		SchemaVersion: version,
		Operation:     asString(data["operation"]),
		RequestID:     optionalStringFromAny(data["request_id"]),
	}
	if raw, ok := data["params"]; ok && raw != nil {
		in.Params = jsonObjectFromAny(raw)
	} else {
		in.Params = JsonObject{}
	}
	if raw, ok := data["workspace"]; ok && raw != nil {
		ws, wsErr := WorkspaceSpecFromDict(raw)
		if wsErr != nil {
			return nil, wsErr
		}
		in.Workspace = ws
	}
	if raw, ok := data["timeout_seconds"]; ok && raw != nil {
		f, ferr := positiveNumber("timeout_seconds", raw)
		if ferr != nil {
			return nil, ferr
		}
		in.TimeoutSeconds = &f
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	return in, nil
}

// ToDict 返回终态查询记录。
// Python: query.py:154-166 OneShotQueryResult.to_dict
//
// sequence 固定 0、session_id 固定 nil。
func (r *OneShotQueryResult) ToDict() map[string]any {
	if r == nil {
		return nil
	}
	var data any
	if r.Data != nil {
		data = ThawJson(r.Data)
	}
	var errDict any
	if r.Error != nil {
		errDict = r.Error.ToDict()
	}
	return map[string]any{
		"schema_version": CurrentSchemaVersion,
		"type":           "query_result",
		"sequence":       0,
		"request_id":     r.RequestID,
		"session_id":     nil,
		"operation":      optionalStringValue(r.Operation),
		"status":         r.Status,
		"exit_code":      r.ExitCode,
		"data":           data,
		"error":          errDict,
	}
}

// OneShotQueryResultParams NewOneShotQueryResult 的入参。
type OneShotQueryResultParams struct {
	// RequestID 请求标识
	RequestID string
	// Operation 查询操作
	Operation *string
	// Status 终态
	Status string
	// ExitCode 退出码
	ExitCode int
	// Data 查询结果数据
	Data JsonObject
	// Error 错误详情
	Error *RuntimeErrorInfo
}

// NewOneShotQueryResult 构造并校验终态查询结果。
// Python: OneShotQueryResult.__post_init__（query.py:138-152）
func NewOneShotQueryResult(p OneShotQueryResultParams) (*OneShotQueryResult, error) {
	res := &OneShotQueryResult{
		RequestID: p.RequestID,
		Operation: p.Operation,
		Status:    p.Status,
		ExitCode:  p.ExitCode,
		Data:      p.Data,
		Error:     p.Error,
	}
	if err := res.validate(); err != nil {
		return nil, err
	}
	return res, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validate OneShotQueryInput 的构造期校验。
// Python: query.py:52-92
func (in *OneShotQueryInput) validate() error {
	// Python: :53-54 if self.operation not in QUERY_FIELDS: raise ValueError
	spec, ok := queryFieldSpecs[in.Operation]
	if !ok {
		return fmt.Errorf("query operation is not supported")
	}
	// Python: :55-61 params 严格对象
	data, err := strictObject("params", in.Params, spec.Allowed, spec.Required)
	if err != nil {
		return err
	}
	// Python: :62-64 session_id / requested 必须非空文本
	for _, key := range []string{"session_id", "requested"} {
		if raw, ok := data[key]; ok {
			if _, terr := requiredText(key, asString(raw), false); terr != nil {
				return terr
			}
		}
	}
	// Python: :65-68 search 最多 200 字符
	if raw, ok := data["search"]; ok {
		search := asString(raw)
		if _, isStr := raw.(string); !isStr || runeLen(search) > 200 {
			return fmt.Errorf("search must be text of at most 200 characters")
		}
	}
	// Python: :69-72 limit 1..200
	if raw, ok := data["limit"]; ok {
		limit, lerr := nonNegativeInt("limit", raw)
		if lerr != nil {
			return lerr
		}
		if limit < 1 || limit > 200 {
			return fmt.Errorf("limit must be between 1 and 200")
		}
	}
	// Python: :73-74 offset 非负
	if raw, ok := data["offset"]; ok {
		if _, oerr := nonNegativeInt("offset", raw); oerr != nil {
			return oerr
		}
	}
	// Python: :75-80 references 数组且每项非空
	if raw, ok := data["references"]; ok {
		list, lerr := anySlice("references", raw)
		if lerr != nil {
			return fmt.Errorf("references must be an array")
		}
		for _, item := range list {
			if _, terr := requiredText("reference", asString(item), false); terr != nil {
				return terr
			}
		}
	}
	// Python: :81 object.__setattr__(self, "params", _freeze_object("params", params))
	frozen, ferr := freezeObject("params", data)
	if ferr != nil {
		return ferr
	}
	in.Params = frozen
	// Python: :82-84 request_id = _optional_text
	if in.RequestID, err = optionalText("request_id", in.RequestID); err != nil {
		return err
	}
	// Python: :85-86 workspace 类型
	if in.Workspace != nil {
		if verr := in.Workspace.validate(); verr != nil {
			return verr
		}
	}
	// Python: :87-92 timeout_seconds
	if in.TimeoutSeconds != nil {
		f, ferr2 := positiveNumber("timeout_seconds", *in.TimeoutSeconds)
		if ferr2 != nil {
			return ferr2
		}
		in.TimeoutSeconds = &f
	}
	return nil
}

// validate OneShotQueryResult 的构造期校验。
// Python: query.py:138-152
func (r *OneShotQueryResult) validate() error {
	if _, err := requiredText("request_id", r.RequestID, false); err != nil {
		return err
	}
	if !isRunStatus(r.Status) {
		return fmt.Errorf("unsupported run status: %s", r.Status)
	}
	if r.ExitCode < 0 {
		return fmt.Errorf("exit_code must not be negative")
	}
	if r.Status == string(StatusCompleted) {
		// Python: :142-148 成功必须 exit_code 0 + 无 error + 有 operation 与 data
		if r.ExitCode != 0 || r.Error != nil {
			return fmt.Errorf("successful query must have exit_code zero and no error")
		}
		// Python: if self.operation not in QUERY_FIELDS or self.data is None
		opName := ""
		if r.Operation != nil {
			opName = *r.Operation
		}
		_, opOK := queryFieldSpecs[opName]
		if !opOK || r.Data == nil {
			return fmt.Errorf("successful query requires an operation and data")
		}
	} else {
		// Python: :149-150 失败必须 exit_code 非 0 + 有 error
		if r.ExitCode == 0 || r.Error == nil {
			return fmt.Errorf("failed query requires nonzero exit_code and error")
		}
	}
	if r.Data != nil {
		frozen, err := freezeObject("data", r.Data)
		if err != nil {
			return err
		}
		r.Data = frozen
	}
	return nil
}

// setOf 构造字符串集合。
func setOf(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

// runeLen 按 Unicode 码点计数。
// Python: len(str) 按码点
func runeLen(s string) int {
	return len([]rune(s))
}

// TrimSpaceAny 取任意值的去空白字符串（query 参数校验用）。
func TrimSpaceAny(value any) string {
	return strings.TrimSpace(asString(value))
}
