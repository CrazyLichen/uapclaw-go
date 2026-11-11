package harness_protocol

import (
	"math"
	"reflect"
)

// 本文件对应 Python openjiuwen/harness_protocol/serialization.py（525 行）：
// 外部 harness 观测事件的稳定 JSON 线格式 codec。
//
// 这是协议层唯一负责「让事件活着离开本进程」的环节：provider 跑在子进程，
// 事件必须序列化过 stdio。7 对 to/from 加 13 个枚举解析，共 26 个函数，
// 与上游一一对应（Python: serialization.py，AST 核对 2 公开 + 24 私有）。
//
// 分节顺序对应上游函数出现顺序，可逐段对照。

// ──────────────────────────── 常量 ────────────────────────────

// EventWireSchemaVersion 事件线格式的 schema 版本。
// Python: serialization.py:50 EVENT_WIRE_SCHEMA_VERSION
const EventWireSchemaVersion = "1"

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数（基础助手）────────────────────────────

// asMapping 校验并返回 JSON 对象（所有 key 必须是 string）。
// Python: serialization.py:470-473 _mapping
func asMapping(value any, fieldName string) (map[string]any, error) {
	if value == nil {
		return nil, &HarnessProtocolError{Msg: fieldName + " must be a JSON object"}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, &HarnessProtocolError{Msg: fieldName + " must be a JSON object"}
	}
	return obj, nil
}

// asJsonObject 返回 JSON 对象的 JsonObject 视图。
// Python: serialization.py:476-477 _json_object
func asJsonObject(value any, fieldName string) (JsonObject, error) {
	return asMapping(value, fieldName)
}

// asList 校验并返回 JSON 数组。
// Python: serialization.py:480-483 _list
func asList(value any, fieldName string) ([]any, error) {
	arr, ok := value.([]any)
	if !ok {
		return nil, &HarnessProtocolError{Msg: fieldName + " must be a JSON array"}
	}
	return arr, nil
}

// asString 从对象里取必填字符串字段。
//
// 键缺失时返回错误，文案与上游一致（Python 的 KeyError 被外层
// harness_event_from_dict 包成 invalid harness event wire object）。
// Python: serialization.py:486-487 _string
func asString(data map[string]any, fieldName string) (string, error) {
	value, ok := data[fieldName]
	if !ok {
		return "", &HarnessProtocolError{
			Msg: "invalid harness event wire object: " + fieldName,
		}
	}
	return asRequiredString(value, fieldName)
}

// asRequiredString 校验值是非空字符串。
// Python: serialization.py:490-493 _required_string
func asRequiredString(value any, fieldName string) (string, error) {
	s, ok := value.(string)
	if !ok || s == "" {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a non-empty string"}
	}
	return s, nil
}

// asOptionalString 校验可空字符串：nil 通过，空串非法。
// Python: serialization.py:496-499 _optional_string
func asOptionalString(value any, fieldName string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	s, err := asRequiredString(value, fieldName)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// asInteger 校验整数，四步：
//  1. 拒绝 bool（Python 的 isinstance(value, bool) 优先排除）
//  2. 整数族直接转 int
//  3. float64 必须整值、有限、且在 int64 范围内（spec D4 放宽）
//  4. 其余报错
//
// 步骤 3 的范围检查不可省：实测 float64(1e300) 满足 v == Trunc(v)，
// 直接 int(v) 会溢出。JSON 里写 1e300 完全合法，json.Unmarshal 原样解出，
// 因此这是必现路径。
// Python: serialization.py:502-506 _integer
func asInteger(value any, fieldName string) (int, error) {
	// 步骤 1：拒绝 bool
	if _, isBool := value.(bool); isBool {
		return 0, &HarnessProtocolError{Msg: fieldName + " must be an integer"}
	}
	// 步骤 2：整数族
	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	}
	// 步骤 3：整值 float64
	if f, ok := value.(float64); ok {
		if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
			return 0, &HarnessProtocolError{Msg: fieldName + " must be an integer"}
		}
		if f < math.MinInt64 || f > math.MaxInt64 {
			return 0, &HarnessProtocolError{Msg: fieldName + " must be an integer"}
		}
		return int(f), nil
	}
	// 步骤 4：其余
	return 0, &HarnessProtocolError{Msg: fieldName + " must be an integer"}
}

// asNumber 校验浮点数，三步：拒 bool、整数族转 float64、float64 原值。
// NaN/Inf 在此不拒（交由 FreezeJsonValue 拒绝，对应上游 freeze_json_value）。
// Python: serialization.py:509-513 _number
func asNumber(value any, fieldName string) (float64, error) {
	// 步骤 1：拒绝 bool
	if _, isBool := value.(bool); isBool {
		return 0, &HarnessProtocolError{Msg: fieldName + " must be a number"}
	}
	// 步骤 2：整数族与 float64
	switch v := value.(type) {
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	}
	// 步骤 3：其余
	return 0, &HarnessProtocolError{Msg: fieldName + " must be a number"}
}

// pyRepr 复用 models.go:778-780 的同名助手（15.1 交付，形态等价：
// 单引号包裹，对应 Python 的 {value!r}）。此处不再重复定义。
// Python: serialization.py:522（f"unknown {field_name}: {value!r}"）

// payloadTypeName 返回载荷的 Go 类型名，用于兜底错误文案。
// Python 侧是 type(event).__name__。
func payloadTypeName(event HarnessEventPayload) string {
	if event == nil {
		return "None"
	}
	rv := reflect.ValueOf(event)
	if rv.Kind() == reflect.Ptr {
		return "*" + rv.Type().Elem().Name()
	}
	return rv.Type().Name()
}

// ──────────────────────────── 非导出函数（枚举解析）────────────────────────────

// 13 个 parseXxx 对应 Python 的一个 _enum（serialization.py:516-522），
// 共 14 处调用（HarnessState 调用 2 次：state_changed.old 与 .new）。
// Go 侧拆成独立函数是为了用 switch 常量获得编译期检查——
// case OutputKindText 拼错编译不过，而 map/字符串比较不会报错。

// parseMessageRole 解析 MessageRole（4 成员）。
// Python: serialization.py:110 _enum(MessageRole, ...)
func parseMessageRole(value any, fieldName string) (MessageRole, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch MessageRole(s) {
	case MessageRoleUser, MessageRoleAssistant, MessageRoleSystem, MessageRoleTool:
		return MessageRole(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseModelRequestStatus 解析 ModelRequestStatus（3 成员）。
// Python: serialization.py:174 _enum(ModelRequestStatus, ...)
func parseModelRequestStatus(value any, fieldName string) (ModelRequestStatus, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch ModelRequestStatus(s) {
	case ModelRequestStatusCompleted, ModelRequestStatusFailed, ModelRequestStatusCancelled:
		return ModelRequestStatus(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseTurnStatus 解析 TurnStatus（3 成员）。
// Python: serialization.py:261 _enum(TurnStatus, ...)
func parseTurnStatus(value any, fieldName string) (TurnStatus, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch TurnStatus(s) {
	case TurnStatusCompleted, TurnStatusInterrupted, TurnStatusFailed:
		return TurnStatus(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseTurnTerminationKind 解析 TurnTerminationKind（5 成员）。
// Python: serialization.py:238 _enum(TurnTerminationKind, ...)
func parseTurnTerminationKind(value any, fieldName string) (TurnTerminationKind, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch TurnTerminationKind(s) {
	case TurnTerminationKindUserAbort, TurnTerminationKindTimeout,
		TurnTerminationKindPolicy, TurnTerminationKindProvider,
		TurnTerminationKindHarnessStop:
		return TurnTerminationKind(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseOutputKind 解析 OutputKind（2 成员）。
// Python: serialization.py:388 _enum(OutputKind, ...)
func parseOutputKind(value any, fieldName string) (OutputKind, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch OutputKind(s) {
	case OutputKindText, OutputKindStructured:
		return OutputKind(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseOutputOperation 解析 OutputOperation（3 成员）。
// Python: serialization.py:390 _enum(OutputOperation, ...)
func parseOutputOperation(value any, fieldName string) (OutputOperation, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch OutputOperation(s) {
	case OutputOperationDelta, OutputOperationSnapshot, OutputOperationFinal:
		return OutputOperation(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseOutputChannel 解析 OutputChannel（3 成员）。
// Python: serialization.py:391 _enum(OutputChannel, ...)
func parseOutputChannel(value any, fieldName string) (OutputChannel, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch OutputChannel(s) {
	case OutputChannelAnswer, OutputChannelReasoning, OutputChannelSystem:
		return OutputChannel(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseItemEventKind 解析 ItemEventKind（3 成员）。
// Python: serialization.py:398 _enum(ItemEventKind, ...)
func parseItemEventKind(value any, fieldName string) (ItemEventKind, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch ItemEventKind(s) {
	case ItemEventKindStarted, ItemEventKindUpdated, ItemEventKindCompleted:
		return ItemEventKind(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseUsageUpdateMode 解析 UsageUpdateMode（2 成员）。
// Python: serialization.py:405 _enum(UsageUpdateMode, ...)
func parseUsageUpdateMode(value any, fieldName string) (UsageUpdateMode, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch UsageUpdateMode(s) {
	case UsageUpdateModeDelta, UsageUpdateModeCumulative:
		return UsageUpdateMode(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseHarnessState 解析 HarnessState（5 成员）。
// Python: serialization.py:411-412 _enum(HarnessState, ...) 调用两次
func parseHarnessState(value any, fieldName string) (HarnessState, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch HarnessState(s) {
	case HarnessStateIdle, HarnessStateRunning, HarnessStatePausing,
		HarnessStatePaused, HarnessStateTerminated:
		return HarnessState(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseTurnEventKind 解析 TurnEventKind（6 成员）。
// Python: serialization.py:417 _enum(TurnEventKind, ...)
func parseTurnEventKind(value any, fieldName string) (TurnEventKind, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch TurnEventKind(s) {
	case TurnEventKindStarted, TurnEventKindPaused, TurnEventKindResumed,
		TurnEventKindFinished, TurnEventKindAborted, TurnEventKindFailed:
		return TurnEventKind(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseHookEventPhase 解析 HookEventPhase（2 成员）。
// Python: serialization.py:424 _enum(HookEventPhase, ...)
func parseHookEventPhase(value any, fieldName string) (HookEventPhase, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch HookEventPhase(s) {
	case HookEventPhaseStarted, HookEventPhaseFinished:
		return HookEventPhase(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// parseDiagnosticLevel 解析 DiagnosticLevel（4 成员）。
// Python: serialization.py:431 _enum(DiagnosticLevel, ...)
func parseDiagnosticLevel(value any, fieldName string) (DiagnosticLevel, error) {
	s, ok := value.(string)
	if !ok {
		return "", &HarnessProtocolError{Msg: fieldName + " must be a string enum value"}
	}
	switch DiagnosticLevel(s) {
	case DiagnosticLevelDebug, DiagnosticLevelInfo, DiagnosticLevelWarning, DiagnosticLevelError:
		return DiagnosticLevel(s), nil
	}
	return "", &HarnessProtocolError{Msg: "unknown " + fieldName + ": " + pyRepr(s)}
}

// causationIDsToAny 把 string 切片转成 []any（JSON 数组）。
// 空切片必须转成空数组而非 nil，否则序列化出 null 而非 []。
func causationIDsToAny(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// optInteger 校验可空整数：nil 通过，其余走 asInteger。
func optInteger(value any, fieldName string) (*int, error) {
	if value == nil {
		return nil, nil
	}
	n, err := asInteger(value, fieldName)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// optFloat 校验可空浮点。
func optFloat(value any, fieldName string) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	f, err := asNumber(value, fieldName)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// asNumberFromKey 从对象里取必填浮点字段。
func asNumberFromKey(data map[string]any, fieldName string) (float64, error) {
	value, ok := data[fieldName]
	if !ok {
		return 0, &HarnessProtocolError{
			Msg: "invalid harness event wire object: " + fieldName,
		}
	}
	return asNumber(value, fieldName)
}

// asIntegerFromKey 从对象里取必填整数字段。
func asIntegerFromKey(data map[string]any, fieldName string) (int, error) {
	value, ok := data[fieldName]
	if !ok {
		return 0, &HarnessProtocolError{
			Msg: "invalid harness event wire object: " + fieldName,
		}
	}
	return asInteger(value, fieldName)
}

// intPtrToAny 把可空整数转成线格式值（nil 保持 nil）。
func intPtrToAny(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// strPtrToAny 把可空字符串转成线格式值。
func strPtrToAny(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// boolPtrToAny 把可空布尔转成线格式值。
func boolPtrToAny(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}

// floatPtrToAny 把可空浮点转成线格式值。
func floatPtrToAny(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// optionalStrPtr 把「空串表示未提供」的字符串转成线格式值。
// 15.2 决策：Go 侧 optional ID 统一用 string，空串即未提供；
// 序列化时空串写 nil（对应 Python 的 None）。
func optionalStrPtr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// optionalStrOrEmpty 把可空字符串还原成「空串表示未提供」。
func optionalStrOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
