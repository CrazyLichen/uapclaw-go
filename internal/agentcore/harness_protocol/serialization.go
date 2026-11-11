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

// HarnessEventToDict 把一条事件编码成带 discriminator 的 JSON 对象。
//
// 键集合固定 12 个：schema_version / event_type / sequence / timestamp /
// host_session_id / agent_id / provider_session_id / turn_id / item_id /
// correlation_id / causation_ids / payload。
// 不含 team 私名（team_session_id / member_agent_id / session_id）。
// Python: serialization.py:362-379 harness_event_to_dict
func HarnessEventToDict(event HarnessEvent) map[string]any {
	// 步骤 1：编码载荷三元组
	// Python: serialization.py:365
	eventType, schemaVersion, payload, err := payloadToWire(event.Event)
	if err != nil {
		// 上游此处抛异常；Go 侧无法在签名上返回 error（对齐上游无 error 返回值），
		// 故降级为空 payload 并把诊断写入 _codec_error 键。合法载荷均由构造器
		// 校验过，此分支仅在调用方塞入自定义 HarnessEventPayload 实现时触发。
		payload = map[string]any{}
	}
	// 步骤 2：组装 12 键信封
	// Python: serialization.py:366-379
	wire := map[string]any{
		"schema_version":      schemaVersion,
		"event_type":          eventType,
		"sequence":            event.Sequence,
		"timestamp":           event.Timestamp,
		"host_session_id":     event.HostSessionID,
		"agent_id":            event.AgentID,
		"provider_session_id": optionalStrPtr(event.ProviderSessionID),
		"turn_id":             optionalStrPtr(event.TurnID),
		"item_id":             optionalStrPtr(event.ItemID),
		"correlation_id":      optionalStrPtr(event.CorrelationID),
		"causation_ids":       causationIDsToAny(event.CausationIDs),
		"payload":             payload,
	}
	if err != nil {
		wire["_codec_error"] = err.Error()
	}
	return wire
}

// HarnessEventFromDict 解码一条事件，未知事件类型与 schema 版本无损保留。
// Python: serialization.py:445-467 harness_event_from_dict
func HarnessEventFromDict(data map[string]any) (HarnessEvent, error) {
	// 步骤 1：event_type 与 schema_version 必填
	// Python: serialization.py:449-450
	eventType, err := asString(data, "event_type")
	if err != nil {
		return HarnessEvent{}, err
	}
	schemaVersion, err := asString(data, "schema_version")
	if err != nil {
		return HarnessEvent{}, err
	}
	// 步骤 2：payload 必须是对象
	// Python: serialization.py:451
	payloadObj, err := asMapping(data["payload"], "payload")
	if err != nil {
		return HarnessEvent{}, err
	}
	// 步骤 3：causation_ids 每项非空
	// Python: serialization.py:452-453
	causationIDs := []string{}
	if raw, present := data["causation_ids"]; present && raw != nil {
		arr, err := asList(raw, "causation_ids")
		if err != nil {
			return HarnessEvent{}, err
		}
		causationIDs = make([]string, len(arr))
		for i, item := range arr {
			s, err := asRequiredString(item, "causation_id")
			if err != nil {
				return HarnessEvent{}, err
			}
			causationIDs[i] = s
		}
	}
	// 步骤 4：解码载荷
	// Python: serialization.py:457
	payload, err := payloadFromWire(eventType, schemaVersion, payloadObj)
	if err != nil {
		return HarnessEvent{}, err
	}
	// 步骤 5：必填标量
	// Python: serialization.py:455-459
	sequence, err := asIntegerFromKey(data, "sequence")
	if err != nil {
		return HarnessEvent{}, err
	}
	timestamp, err := asNumberFromKey(data, "timestamp")
	if err != nil {
		return HarnessEvent{}, err
	}
	hostSessionID, err := asString(data, "host_session_id")
	if err != nil {
		return HarnessEvent{}, err
	}
	agentID, err := asString(data, "agent_id")
	if err != nil {
		return HarnessEvent{}, err
	}
	// 步骤 6：可空 ID
	// Python: serialization.py:460-463
	providerSessionID, err := asOptionalString(data["provider_session_id"], "provider_session_id")
	if err != nil {
		return HarnessEvent{}, err
	}
	turnID, err := asOptionalString(data["turn_id"], "turn_id")
	if err != nil {
		return HarnessEvent{}, err
	}
	itemID, err := asOptionalString(data["item_id"], "item_id")
	if err != nil {
		return HarnessEvent{}, err
	}
	correlationID, err := asOptionalString(data["correlation_id"], "correlation_id")
	if err != nil {
		return HarnessEvent{}, err
	}
	// 步骤 7：交给构造器做 9 步不变量校验
	// Python: serialization.py:454-465
	return NewHarnessEvent(HarnessEventOptions{
		Sequence:          sequence,
		Timestamp:         timestamp,
		Event:             payload,
		HostSessionID:     hostSessionID,
		AgentID:           agentID,
		ProviderSessionID: optionalStrOrEmpty(providerSessionID),
		TurnID:            optionalStrOrEmpty(turnID),
		ItemID:            optionalStrOrEmpty(itemID),
		CorrelationID:     optionalStrOrEmpty(correlationID),
		CausationIDs:      causationIDs,
	})
}

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
	if rv.Kind() == reflect.Pointer {
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

// ──────────────────────────── 非导出函数（值对象编解码）────────────────────────────

// 7 对 to/from 按上游函数出现顺序排列。前缀约定分两类，不可统一：
//   - errorFromDict 的前缀来自**实参** fieldName（同一函数产出
//     model_request.error.* / turn_result.error.* / turn_result.termination.* 三种）
//   - usageFromDict / blockFromDict / messageFromDict 的前缀**硬编码**
//
// Python: serialization.py:57-134

// usageToDict 编码归一化 token 用量。
// Python: serialization.py:57-65 _usage_to_dict
func usageToDict(usage TurnUsage) map[string]any {
	return map[string]any{
		"input_tokens":            intPtrToAny(usage.InputTokens),
		"output_tokens":           intPtrToAny(usage.OutputTokens),
		"cached_input_tokens":     intPtrToAny(usage.CachedInputTokens),
		"reasoning_output_tokens": intPtrToAny(usage.ReasoningOutputTokens),
		"total_tokens":            intPtrToAny(usage.TotalTokens),
		"provider_data":           JsonValueToBuiltin(usage.ProviderData),
	}
}

// usageFromDict 解码归一化 token 用量。
// Python: serialization.py:68-76 _usage_from_dict
func usageFromDict(data map[string]any) (TurnUsage, error) {
	// 步骤 1：5 个可空整数字段
	inputTokens, err := optInteger(data["input_tokens"], "usage.input_tokens")
	if err != nil {
		return TurnUsage{}, err
	}
	outputTokens, err := optInteger(data["output_tokens"], "usage.output_tokens")
	if err != nil {
		return TurnUsage{}, err
	}
	cachedInputTokens, err := optInteger(data["cached_input_tokens"], "usage.cached_input_tokens")
	if err != nil {
		return TurnUsage{}, err
	}
	reasoningOutputTokens, err := optInteger(data["reasoning_output_tokens"], "usage.reasoning_output_tokens")
	if err != nil {
		return TurnUsage{}, err
	}
	totalTokens, err := optInteger(data["total_tokens"], "usage.total_tokens")
	if err != nil {
		return TurnUsage{}, err
	}

	// 步骤 2：provider_data 缺省为空对象
	// Python: serialization.py:75 data.get("provider_data", {})
	providerData := map[string]any{}
	if raw, present := data["provider_data"]; present && raw != nil {
		providerData, err = asJsonObject(raw, "usage.provider_data")
		if err != nil {
			return TurnUsage{}, err
		}
	}

	return TurnUsage{
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		CachedInputTokens:     cachedInputTokens,
		ReasoningOutputTokens: reasoningOutputTokens,
		TotalTokens:           totalTokens,
		ProviderData:          providerData,
	}, nil
}

// blockToDict 编码内容块。
// Python: serialization.py:79-85 _block_to_dict
func blockToDict(block ContentBlock) map[string]any {
	return map[string]any{
		"block_id": block.BlockID,
		"kind":     block.Kind,
		"content":  JsonValueToBuiltin(block.Content),
		"data":     JsonValueToBuiltin(block.Data),
	}
}

// blockFromDict 解码内容块。
// Python: serialization.py:88-94 _block_from_dict
func blockFromDict(data map[string]any) (ContentBlock, error) {
	// 步骤 1：block_id 与 kind 必填
	blockID, err := asString(data, "block_id")
	if err != nil {
		return ContentBlock{}, err
	}
	kind, err := asString(data, "kind")
	if err != nil {
		return ContentBlock{}, err
	}
	// 步骤 2：data 缺省为空对象，前缀硬编码 content_block.
	dataObj := map[string]any{}
	if raw, present := data["data"]; present && raw != nil {
		dataObj, err = asJsonObject(raw, "content_block.data")
		if err != nil {
			return ContentBlock{}, err
		}
	}
	return ContentBlock{
		BlockID: blockID,
		Kind:    kind,
		Content: data["content"],
		Data:    dataObj,
	}, nil
}

// messageToDict 编码归一化消息。
// Python: serialization.py:97-103 _message_to_dict
func messageToDict(message TurnMessage) map[string]any {
	blocks := make([]any, len(message.Content))
	for i, block := range message.Content {
		blocks[i] = blockToDict(block)
	}
	return map[string]any{
		"message_id": message.MessageID,
		"role":       string(message.Role),
		"content":    blocks,
		"data":       JsonValueToBuiltin(message.Data),
	}
}

// messageFromDict 解码归一化消息。
// Python: serialization.py:106-113 _message_from_dict
func messageFromDict(data map[string]any) (TurnMessage, error) {
	// 步骤 1：message_id 必填
	messageID, err := asString(data, "message_id")
	if err != nil {
		return TurnMessage{}, err
	}
	// 步骤 2：role 必须是合法枚举
	role, err := parseMessageRole(data["role"], "turn_message.role")
	if err != nil {
		return TurnMessage{}, err
	}
	// 步骤 3：content 数组，前缀硬编码 turn_message.
	contentRaw := []any{}
	if raw, present := data["content"]; present && raw != nil {
		contentRaw, err = asList(raw, "turn_message.content")
		if err != nil {
			return TurnMessage{}, err
		}
	}
	blocks := make([]ContentBlock, len(contentRaw))
	for i, item := range contentRaw {
		obj, err := asMapping(item, "content_block")
		if err != nil {
			return TurnMessage{}, err
		}
		block, err := blockFromDict(obj)
		if err != nil {
			return TurnMessage{}, err
		}
		blocks[i] = block
	}
	// 步骤 4：data 缺省为空对象
	dataObj := map[string]any{}
	if raw, present := data["data"]; present && raw != nil {
		dataObj, err = asJsonObject(raw, "turn_message.data")
		if err != nil {
			return TurnMessage{}, err
		}
	}
	return TurnMessage{
		MessageID: messageID,
		Role:      role,
		Content:   blocks,
		Data:      dataObj,
	}, nil
}

// errorToDict 编码轮次失败信息。
// Python: serialization.py:116-123 _error_to_dict
func errorToDict(turnError TurnError) map[string]any {
	return map[string]any{
		"message":       turnError.Message,
		"code":          strPtrToAny(turnError.Code),
		"category":      strPtrToAny(turnError.Category),
		"retryable":     boolPtrToAny(turnError.Retryable),
		"provider_data": JsonValueToBuiltin(turnError.ProviderData),
	}
}

// errorFromDict 解码轮次失败信息。前缀来自实参 fieldName。
// Python: serialization.py:126-134 _error_from_dict
func errorFromDict(data any, fieldName string) (TurnError, error) {
	// 步骤 1：必须是对象
	obj, err := asMapping(data, fieldName)
	if err != nil {
		return TurnError{}, err
	}
	// 步骤 2：message 必填
	message, err := asString(obj, "message")
	if err != nil {
		return TurnError{}, err
	}
	// 步骤 3：code 与 category 可空，但前缀用实参
	code, err := asOptionalString(obj["code"], fieldName+".code")
	if err != nil {
		return TurnError{}, err
	}
	category, err := asOptionalString(obj["category"], fieldName+".category")
	if err != nil {
		return TurnError{}, err
	}
	// 步骤 4：retryable 可空布尔
	var retryable *bool
	if raw, present := obj["retryable"]; present && raw != nil {
		b, ok := raw.(bool)
		if !ok {
			return TurnError{}, &HarnessProtocolError{Msg: fieldName + ".retryable must be a boolean"}
		}
		retryable = &b
	}
	// 步骤 5：provider_data 缺省为空对象
	providerData := map[string]any{}
	if raw, present := obj["provider_data"]; present && raw != nil {
		providerData, err = asJsonObject(raw, fieldName+".provider_data")
		if err != nil {
			return TurnError{}, err
		}
	}
	return TurnError{
		Message:      message,
		Code:         code,
		Category:     category,
		Retryable:    retryable,
		ProviderData: providerData,
	}, nil
}

// modelRequestToDict 编码物理模型请求事件。
// 19 个字段与上游顺序一致。
// Python: serialization.py:137-158 _model_request_to_dict
func modelRequestToDict(event ModelRequestEvent) map[string]any {
	// 步骤 1：内容块与消息序列
	systemInstructions := make([]any, len(event.SystemInstructions))
	for i, block := range event.SystemInstructions {
		systemInstructions[i] = blockToDict(block)
	}
	inputMessages := make([]any, len(event.InputMessages))
	for i, message := range event.InputMessages {
		inputMessages[i] = messageToDict(message)
	}
	// 步骤 2：output_message 可空
	var outputMessage any
	if event.OutputMessage != nil {
		outputMessage = messageToDict(*event.OutputMessage)
	}
	// 步骤 3：usage 可空
	var usage any
	if event.Usage != nil {
		usage = usageToDict(*event.Usage)
	}
	// 步骤 4：cost 可空，只暴露 micros 与 currency
	var cost any
	if event.Cost != nil {
		cost = map[string]any{"micros": event.Cost.Micros, "currency": event.Cost.Currency}
	}
	// 步骤 5：error 可空
	var errPayload any
	if event.Error != nil {
		errPayload = errorToDict(*event.Error)
	}
	// 步骤 6：finish_reasons 转 []any
	finishReasons := make([]any, len(event.FinishReasons))
	for i, reason := range event.FinishReasons {
		finishReasons[i] = reason
	}
	return map[string]any{
		"request_id":          event.RequestID,
		"status":              string(event.Status),
		"started_at":          event.StartedAt,
		"ended_at":            event.EndedAt,
		"model":               optionalStrPtr(event.Model),
		"provider_name":       optionalStrPtr(event.ProviderName),
		"system_instructions": systemInstructions,
		"input_messages":      inputMessages,
		"input_observed":      event.InputObserved,
		"output_message":      outputMessage,
		"tool_definitions":    JsonValueToBuiltin(event.ToolDefinitions),
		"request_parameters":  JsonValueToBuiltin(event.RequestParameters),
		"response_id":         optionalStrPtr(event.ResponseID),
		"time_to_first_chunk": floatPtrToAny(event.TimeToFirstChunk),
		"finish_reasons":      finishReasons,
		"usage":               usage,
		"cost":                cost,
		"error":               errPayload,
		"data":                JsonValueToBuiltin(event.Data),
	}
}

// modelRequestFromDict 解码物理模型请求事件。
// Python: serialization.py:161-197 _model_request_from_dict
func modelRequestFromDict(data map[string]any) (ModelRequestEvent, error) {
	// 步骤 1：request_id 必填
	requestID, err := asString(data, "request_id")
	if err != nil {
		return ModelRequestEvent{}, err
	}
	// 步骤 2：status 枚举
	status, err := parseModelRequestStatus(data["status"], "model_request.status")
	if err != nil {
		return ModelRequestEvent{}, err
	}
	// 步骤 3：两个时间戳必填
	startedAt, err := asNumberFromKey(data, "started_at")
	if err != nil {
		return ModelRequestEvent{}, err
	}
	endedAt, err := asNumberFromKey(data, "ended_at")
	if err != nil {
		return ModelRequestEvent{}, err
	}
	// 步骤 4：可空字符串
	modelValue, err := asOptionalString(data["model"], "model_request.model")
	if err != nil {
		return ModelRequestEvent{}, err
	}
	providerName, err := asOptionalString(data["provider_name"], "model_request.provider_name")
	if err != nil {
		return ModelRequestEvent{}, err
	}
	responseID, err := asOptionalString(data["response_id"], "model_request.response_id")
	if err != nil {
		return ModelRequestEvent{}, err
	}
	// 步骤 5：system_instructions 数组
	systemRaw := []any{}
	if raw, present := data["system_instructions"]; present && raw != nil {
		systemRaw, err = asList(raw, "model_request.system_instructions")
		if err != nil {
			return ModelRequestEvent{}, err
		}
	}
	systemInstructions := make([]ContentBlock, len(systemRaw))
	for i, item := range systemRaw {
		obj, err := asMapping(item, "content_block")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		block, err := blockFromDict(obj)
		if err != nil {
			return ModelRequestEvent{}, err
		}
		systemInstructions[i] = block
	}
	// 步骤 6：input_messages 数组
	inputRaw := []any{}
	if raw, present := data["input_messages"]; present && raw != nil {
		inputRaw, err = asList(raw, "model_request.input_messages")
		if err != nil {
			return ModelRequestEvent{}, err
		}
	}
	inputMessages := make([]TurnMessage, len(inputRaw))
	for i, item := range inputRaw {
		obj, err := asMapping(item, "turn_message")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		message, err := messageFromDict(obj)
		if err != nil {
			return ModelRequestEvent{}, err
		}
		inputMessages[i] = message
	}
	// 步骤 7：input_observed 缺省 false
	inputObserved := false
	if raw, present := data["input_observed"]; present && raw != nil {
		b, ok := raw.(bool)
		if !ok {
			return ModelRequestEvent{}, &HarnessProtocolError{Msg: "model_request.input_observed must be a boolean"}
		}
		inputObserved = b
	}
	// 步骤 8：output_message 可空
	var outputMessage *TurnMessage
	if raw, present := data["output_message"]; present && raw != nil {
		obj, err := asMapping(raw, "model_request.output_message")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		message, err := messageFromDict(obj)
		if err != nil {
			return ModelRequestEvent{}, err
		}
		outputMessage = &message
	}
	// 步骤 9：request_parameters 缺省为空对象
	requestParameters := map[string]any{}
	if raw, present := data["request_parameters"]; present && raw != nil {
		requestParameters, err = asJsonObject(raw, "model_request.request_parameters")
		if err != nil {
			return ModelRequestEvent{}, err
		}
	}
	// 步骤 10：time_to_first_chunk 可空
	var timeToFirstChunk *float64
	if raw, present := data["time_to_first_chunk"]; present && raw != nil {
		f, err := asNumber(raw, "model_request.time_to_first_chunk")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		timeToFirstChunk = &f
	}
	// 步骤 11：finish_reasons 每项必须非空字符串
	finishReasons := []string{}
	if raw, present := data["finish_reasons"]; present && raw != nil {
		arr, err := asList(raw, "model_request.finish_reasons")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		finishReasons = make([]string, len(arr))
		for i, item := range arr {
			s, err := asRequiredString(item, "model_request.finish_reason")
			if err != nil {
				return ModelRequestEvent{}, err
			}
			finishReasons[i] = s
		}
	}
	// 步骤 12：usage 可空
	var usage *TurnUsage
	if raw, present := data["usage"]; present && raw != nil {
		obj, err := asMapping(raw, "model_request.usage")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		u, err := usageFromDict(obj)
		if err != nil {
			return ModelRequestEvent{}, err
		}
		usage = &u
	}
	// 步骤 13：cost 可空
	var cost *MonetaryAmount
	if raw, present := data["cost"]; present && raw != nil {
		obj, err := asMapping(raw, "model_request.cost")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		micros, err := asIntegerFromKey(obj, "micros")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		currency, err := asString(obj, "currency")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		cost = &MonetaryAmount{Micros: int64(micros), Currency: currency}
	}
	// 步骤 14：error 可空，前缀 model_request.error
	var turnErr *TurnError
	if raw, present := data["error"]; present && raw != nil {
		e, err := errorFromDict(raw, "model_request.error")
		if err != nil {
			return ModelRequestEvent{}, err
		}
		turnErr = &e
	}
	// 步骤 15：data 缺省为空对象
	dataObj := map[string]any{}
	if raw, present := data["data"]; present && raw != nil {
		dataObj, err = asJsonObject(raw, "model_request.data")
		if err != nil {
			return ModelRequestEvent{}, err
		}
	}
	// 步骤 16：交给构造器做不变量校验（时间顺序、status/error 一致性等）
	return NewModelRequestEvent(ModelRequestOptions{
		RequestID:          requestID,
		Status:             status,
		StartedAt:          startedAt,
		EndedAt:            endedAt,
		Model:              optionalStrOrEmpty(modelValue),
		ProviderName:       optionalStrOrEmpty(providerName),
		SystemInstructions: systemInstructions,
		InputMessages:      inputMessages,
		InputObserved:      inputObserved,
		OutputMessage:      outputMessage,
		ToolDefinitions:    data["tool_definitions"],
		RequestParameters:  requestParameters,
		ResponseID:         optionalStrOrEmpty(responseID),
		TimeToFirstChunk:   timeToFirstChunk,
		FinishReasons:      finishReasons,
		Usage:              usage,
		Cost:               cost,
		Error:              turnErr,
		Data:               dataObj,
	})
}

// resultToDict 编码轮次终态结果。
// Python: serialization.py:200-227 _result_to_dict
func resultToDict(result TurnResult) map[string]any {
	// 步骤 1：messages 数组
	messages := make([]any, len(result.Messages))
	for i, message := range result.Messages {
		messages[i] = messageToDict(message)
	}
	// 步骤 2：termination 可空
	var termination any
	if result.Termination != nil {
		termination = map[string]any{
			"kind":          string(result.Termination.Kind),
			"message":       strPtrToAny(result.Termination.Message),
			"code":          strPtrToAny(result.Termination.Code),
			"provider_data": JsonValueToBuiltin(result.Termination.ProviderData),
		}
	}
	// 步骤 3：cost 可空
	var cost any
	if result.Cost != nil {
		cost = map[string]any{"micros": result.Cost.Micros, "currency": result.Cost.Currency}
	}
	// 步骤 4：error 可空
	var errPayload any
	if result.Error != nil {
		errPayload = errorToDict(*result.Error)
	}
	// 步骤 5：usage 可空
	var usage any
	if result.Usage != nil {
		usage = usageToDict(*result.Usage)
	}
	return map[string]any{
		"status":            string(result.Status),
		"messages":          messages,
		"final_output":      JsonValueToBuiltin(result.FinalOutput),
		"structured_output": JsonValueToBuiltin(result.StructuredOutput),
		"stop_reason":       strPtrToAny(result.StopReason),
		"termination":       termination,
		"error":             errPayload,
		"usage":             usage,
		"cost":              cost,
		"started_at":        floatPtrToAny(result.StartedAt),
		"completed_at":      floatPtrToAny(result.CompletedAt),
		"duration_ms":       intPtrToAny(result.DurationMS),
		"provider_data":     JsonValueToBuiltin(result.ProviderData),
	}
}

// resultFromDict 解码轮次终态结果。
// Python: serialization.py:230-274 _result_from_dict
func resultFromDict(data map[string]any) (TurnResult, error) {
	// 步骤 1：status 枚举
	status, err := parseTurnStatus(data["status"], "turn_result.status")
	if err != nil {
		return TurnResult{}, err
	}
	// 步骤 2：messages 数组
	messagesRaw := []any{}
	if raw, present := data["messages"]; present && raw != nil {
		messagesRaw, err = asList(raw, "turn_result.messages")
		if err != nil {
			return TurnResult{}, err
		}
	}
	messages := make([]TurnMessage, len(messagesRaw))
	for i, item := range messagesRaw {
		obj, err := asMapping(item, "turn_message")
		if err != nil {
			return TurnResult{}, err
		}
		message, err := messageFromDict(obj)
		if err != nil {
			return TurnResult{}, err
		}
		messages[i] = message
	}
	// 步骤 3：stop_reason 可空
	stopReason, err := asOptionalString(data["stop_reason"], "turn_result.stop_reason")
	if err != nil {
		return TurnResult{}, err
	}
	// 步骤 4：termination 可空
	var termination *TurnTermination
	if raw, present := data["termination"]; present && raw != nil {
		obj, err := asMapping(raw, "turn_result.termination")
		if err != nil {
			return TurnResult{}, err
		}
		kind, err := parseTurnTerminationKind(obj["kind"], "turn_result.termination.kind")
		if err != nil {
			return TurnResult{}, err
		}
		message, err := asOptionalString(obj["message"], "turn_result.termination.message")
		if err != nil {
			return TurnResult{}, err
		}
		code, err := asOptionalString(obj["code"], "turn_result.termination.code")
		if err != nil {
			return TurnResult{}, err
		}
		providerData := map[string]any{}
		if rawPD, present := obj["provider_data"]; present && rawPD != nil {
			providerData, err = asJsonObject(rawPD, "turn_result.termination.provider_data")
			if err != nil {
				return TurnResult{}, err
			}
		}
		termination = &TurnTermination{
			Kind:         kind,
			Message:      message,
			Code:         code,
			ProviderData: providerData,
		}
	}
	// 步骤 5：error 可空
	var turnErr *TurnError
	if raw, present := data["error"]; present && raw != nil {
		e, err := errorFromDict(raw, "turn_result.error")
		if err != nil {
			return TurnResult{}, err
		}
		turnErr = &e
	}
	// 步骤 6：usage 可空
	var usage *TurnUsage
	if raw, present := data["usage"]; present && raw != nil {
		obj, err := asMapping(raw, "turn_result.usage")
		if err != nil {
			return TurnResult{}, err
		}
		u, err := usageFromDict(obj)
		if err != nil {
			return TurnResult{}, err
		}
		usage = &u
	}
	// 步骤 7：cost 可空
	var cost *MonetaryAmount
	if raw, present := data["cost"]; present && raw != nil {
		obj, err := asMapping(raw, "turn_result.cost")
		if err != nil {
			return TurnResult{}, err
		}
		micros, err := asIntegerFromKey(obj, "micros")
		if err != nil {
			return TurnResult{}, err
		}
		currency, err := asString(obj, "currency")
		if err != nil {
			return TurnResult{}, err
		}
		cost = &MonetaryAmount{Micros: int64(micros), Currency: currency}
	}
	// 步骤 8：三个可空标量
	startedAt, err := optFloat(data["started_at"], "turn_result.started_at")
	if err != nil {
		return TurnResult{}, err
	}
	completedAt, err := optFloat(data["completed_at"], "turn_result.completed_at")
	if err != nil {
		return TurnResult{}, err
	}
	durationMS, err := optInteger(data["duration_ms"], "turn_result.duration_ms")
	if err != nil {
		return TurnResult{}, err
	}
	// 步骤 9：provider_data 缺省为空对象
	providerData := map[string]any{}
	if raw, present := data["provider_data"]; present && raw != nil {
		providerData, err = asJsonObject(raw, "turn_result.provider_data")
		if err != nil {
			return TurnResult{}, err
		}
	}
	// 步骤 10：交给构造器校验终态与 termination/error 的一致性
	return NewTurnResult(status, ResultOptions{
		Messages:         messages,
		FinalOutput:      data["final_output"],
		StructuredOutput: data["structured_output"],
		StopReason:       stopReason,
		Termination:      termination,
		Error:            turnErr,
		Usage:            usage,
		Cost:             cost,
		StartedAt:        startedAt,
		CompletedAt:      completedAt,
		DurationMS:       durationMS,
		ProviderData:     providerData,
	})
}

// ──────────────────────────── 非导出函数（载荷分派）────────────────────────────

// payloadToWire 把载荷编码成 (event_type, schema_version, payload) 三元组。
//
// 双形态约定：HarnessEventPayload 是接口，10 个载荷各有值与指针两种动态类型。
// 漏掉任一形态会落兜底抛错，编解码直接失败（比 EventRetentionOf 的降级更严重）。
//
// UnknownEvent 必须在最前特判：它的线格式 event_type 取自自身 EventType，
// 而 payloadKind() 返回的是内部标记 "unknown"，不能用于 wire 映射。
// Python: serialization.py:277-359 _payload_to_wire（9 条 isinstance if + UnknownEvent）
func payloadToWire(event HarnessEventPayload) (string, string, map[string]any, error) {
	// 步骤 0：UnknownEvent 特判（值 + 指针）
	// Python: serialization.py:357-358
	// 注：Payload 的静态类型是 JsonObject（= map[string]JsonValue 别名），
	// 经 JsonValueToBuiltin 后必为 map[string]any，无需类型断言守卫。
	switch unknown := event.(type) {
	case UnknownEvent:
		builtin, _ := JsonValueToBuiltin(unknown.Payload).(map[string]any)
		return unknown.EventType, unknown.SchemaVersion, builtin, nil
	case *UnknownEvent:
		if unknown != nil {
			builtin, _ := JsonValueToBuiltin(unknown.Payload).(map[string]any)
			return unknown.EventType, unknown.SchemaVersion, builtin, nil
		}
	}

	// 步骤 1：OutputEvent
	// Python: serialization.py:278-292
	outputBody := func(e OutputEvent) map[string]any {
		return map[string]any{
			"output_id":     e.OutputID,
			"kind":          string(e.Kind),
			"content":       JsonValueToBuiltin(e.Content),
			"operation":     string(e.Operation),
			"channel":       string(e.Channel),
			"content_index": e.ContentIndex,
			"content_type":  optionalStrPtr(e.ContentType),
			"data":          JsonValueToBuiltin(e.Data),
		}
	}
	switch e := event.(type) {
	case OutputEvent:
		return "output", EventWireSchemaVersion, outputBody(e), nil
	case *OutputEvent:
		if e != nil {
			return "output", EventWireSchemaVersion, outputBody(*e), nil
		}
	}

	// 步骤 2：ItemLifecycleEvent
	// Python: serialization.py:293-302
	itemBody := func(e ItemLifecycleEvent) map[string]any {
		return map[string]any{
			"kind":      string(e.Kind),
			"item_type": e.ItemType,
			"data":      JsonValueToBuiltin(e.Data),
		}
	}
	switch e := event.(type) {
	case ItemLifecycleEvent:
		return "item_lifecycle", EventWireSchemaVersion, itemBody(e), nil
	case *ItemLifecycleEvent:
		if e != nil {
			return "item_lifecycle", EventWireSchemaVersion, itemBody(*e), nil
		}
	}

	// 步骤 3：UsageUpdatedEvent
	// Python: serialization.py:303-311
	usageBody := func(e UsageUpdatedEvent) map[string]any {
		return map[string]any{
			"usage": usageToDict(e.Usage),
			"mode":  string(e.Mode),
		}
	}
	switch e := event.(type) {
	case UsageUpdatedEvent:
		return "usage_updated", EventWireSchemaVersion, usageBody(e), nil
	case *UsageUpdatedEvent:
		if e != nil {
			return "usage_updated", EventWireSchemaVersion, usageBody(*e), nil
		}
	}

	// 步骤 4：ModelRequestEvent
	// Python: serialization.py:312-313
	switch e := event.(type) {
	case ModelRequestEvent:
		return "model_request", EventWireSchemaVersion, modelRequestToDict(e), nil
	case *ModelRequestEvent:
		if e != nil {
			return "model_request", EventWireSchemaVersion, modelRequestToDict(*e), nil
		}
	}

	// 步骤 5：StateChangedEvent
	// Python: serialization.py:314-315
	stateBody := func(e StateChangedEvent) map[string]any {
		return map[string]any{
			"old": string(e.Old),
			"new": string(e.New),
		}
	}
	switch e := event.(type) {
	case StateChangedEvent:
		return "state_changed", EventWireSchemaVersion, stateBody(e), nil
	case *StateChangedEvent:
		if e != nil {
			return "state_changed", EventWireSchemaVersion, stateBody(*e), nil
		}
	}

	// 步骤 6：TurnLifecycleEvent
	// Python: serialization.py:316-324
	turnBody := func(e TurnLifecycleEvent) map[string]any {
		var result any
		if e.Result != nil {
			result = resultToDict(*e.Result)
		}
		return map[string]any{
			"kind":   string(e.Kind),
			"result": result,
		}
	}
	switch e := event.(type) {
	case TurnLifecycleEvent:
		return "turn_lifecycle", EventWireSchemaVersion, turnBody(e), nil
	case *TurnLifecycleEvent:
		if e != nil {
			return "turn_lifecycle", EventWireSchemaVersion, turnBody(*e), nil
		}
	}

	// 步骤 7：HookObservedEvent
	// Python: serialization.py:325-335
	hookBody := func(e HookObservedEvent) map[string]any {
		return map[string]any{
			"phase":     string(e.Phase),
			"hook_name": e.HookName,
			"hook_id":   e.HookID,
			"data":      JsonValueToBuiltin(e.Data),
		}
	}
	switch e := event.(type) {
	case HookObservedEvent:
		return "hook_observed", EventWireSchemaVersion, hookBody(e), nil
	case *HookObservedEvent:
		if e != nil {
			return "hook_observed", EventWireSchemaVersion, hookBody(*e), nil
		}
	}

	// 步骤 8：DiagnosticEvent
	// Python: serialization.py:336-345
	diagBody := func(e DiagnosticEvent) map[string]any {
		return map[string]any{
			"level":   string(e.Level),
			"message": e.Message,
			"data":    JsonValueToBuiltin(e.Data),
		}
	}
	switch e := event.(type) {
	case DiagnosticEvent:
		return "diagnostic", EventWireSchemaVersion, diagBody(e), nil
	case *DiagnosticEvent:
		if e != nil {
			return "diagnostic", EventWireSchemaVersion, diagBody(*e), nil
		}
	}

	// 步骤 9：ProviderEvent
	// Python: serialization.py:346-356
	providerBody := func(e ProviderEvent) map[string]any {
		return map[string]any{
			"provider":       e.Provider,
			"event_type":     e.EventType,
			"schema_version": e.SchemaVersion,
			"payload":        JsonValueToBuiltin(e.Payload),
		}
	}
	switch e := event.(type) {
	case ProviderEvent:
		return "provider", EventWireSchemaVersion, providerBody(e), nil
	case *ProviderEvent:
		if e != nil {
			return "provider", EventWireSchemaVersion, providerBody(*e), nil
		}
	}

	// 兜底：不支持的载荷类型
	// Python: serialization.py:359
	return "", "", nil, &HarnessProtocolError{
		Msg: "unsupported event payload type: " + payloadTypeName(event),
	}
}

// payloadFromWire 把 (event_type, schema_version, payload) 解码成载荷。
//
// 11 条出口：schema_version 不匹配早退（1）+ 9 个已知 event_type + 未知兜底（1）。
// 后两条是「未知字段保留」的全部实现——新 provider 发来旧宿主不认识的
// 事件类型或提高 schema 版本时，宿主原样保留，不崩不丢。
// Python: serialization.py:382-442 _payload_from_wire
func payloadFromWire(eventType, schemaVersion string, data map[string]any) (HarnessEventPayload, error) {
	// 出口 1：schema 版本不匹配 → 原样保留
	// Python: serialization.py:383-384
	if schemaVersion != EventWireSchemaVersion {
		frozen, err := FreezeJsonObject(data)
		if err != nil {
			return nil, err
		}
		return UnknownEvent{EventType: eventType, SchemaVersion: schemaVersion, Payload: frozen}, nil
	}

	// 出口 2：output
	// Python: serialization.py:385-395
	if eventType == "output" {
		outputID, err := asString(data, "output_id")
		if err != nil {
			return nil, err
		}
		kind, err := parseOutputKind(data["kind"], "output.kind")
		if err != nil {
			return nil, err
		}
		operation, err := parseOutputOperation(data["operation"], "output.operation")
		if err != nil {
			return nil, err
		}
		channel, err := parseOutputChannel(data["channel"], "output.channel")
		if err != nil {
			return nil, err
		}
		contentIndex, err := asIntegerFromKey(data, "content_index")
		if err != nil {
			return nil, err
		}
		contentType, err := asOptionalString(data["content_type"], "output.content_type")
		if err != nil {
			return nil, err
		}
		dataObj := map[string]any{}
		if raw, present := data["data"]; present && raw != nil {
			dataObj, err = asJsonObject(raw, "output.data")
			if err != nil {
				return nil, err
			}
		}
		return NewOutputEvent(outputID, kind, data["content"], operation, channel,
			contentIndex, optionalStrOrEmpty(contentType), dataObj)
	}

	// 出口 3：item_lifecycle
	// Python: serialization.py:396-401
	if eventType == "item_lifecycle" {
		kind, err := parseItemEventKind(data["kind"], "item.kind")
		if err != nil {
			return nil, err
		}
		itemType, err := asString(data, "item_type")
		if err != nil {
			return nil, err
		}
		dataObj := map[string]any{}
		if raw, present := data["data"]; present && raw != nil {
			dataObj, err = asJsonObject(raw, "item.data")
			if err != nil {
				return nil, err
			}
		}
		return NewItemLifecycleEvent(kind, itemType, dataObj)
	}

	// 出口 4：usage_updated
	// Python: serialization.py:402-406
	if eventType == "usage_updated" {
		usageObj, err := asMapping(data["usage"], "usage_updated.usage")
		if err != nil {
			return nil, err
		}
		usage, err := usageFromDict(usageObj)
		if err != nil {
			return nil, err
		}
		mode, err := parseUsageUpdateMode(data["mode"], "usage_updated.mode")
		if err != nil {
			return nil, err
		}
		return NewUsageUpdatedEvent(usage, mode)
	}

	// 出口 5：model_request
	// Python: serialization.py:407-408
	if eventType == "model_request" {
		return modelRequestFromDict(data)
	}

	// 出口 6：state_changed
	// Python: serialization.py:409-413
	if eventType == "state_changed" {
		oldState, err := parseHarnessState(data["old"], "state_changed.old")
		if err != nil {
			return nil, err
		}
		newState, err := parseHarnessState(data["new"], "state_changed.new")
		if err != nil {
			return nil, err
		}
		return NewStateChangedEvent(oldState, newState)
	}

	// 出口 7：turn_lifecycle
	// Python: serialization.py:414-421
	if eventType == "turn_lifecycle" {
		kind, err := parseTurnEventKind(data["kind"], "turn_lifecycle.kind")
		if err != nil {
			return nil, err
		}
		var result *TurnResult
		if raw, present := data["result"]; present && raw != nil {
			obj, err := asMapping(raw, "turn_lifecycle.result")
			if err != nil {
				return nil, err
			}
			r, err := resultFromDict(obj)
			if err != nil {
				return nil, err
			}
			result = &r
		}
		return NewTurnLifecycleEvent(kind, result)
	}

	// 出口 8：hook_observed
	// Python: serialization.py:422-428
	if eventType == "hook_observed" {
		phase, err := parseHookEventPhase(data["phase"], "hook.phase")
		if err != nil {
			return nil, err
		}
		hookName, err := asString(data, "hook_name")
		if err != nil {
			return nil, err
		}
		hookID, err := asString(data, "hook_id")
		if err != nil {
			return nil, err
		}
		dataObj := map[string]any{}
		if raw, present := data["data"]; present && raw != nil {
			dataObj, err = asJsonObject(raw, "hook.data")
			if err != nil {
				return nil, err
			}
		}
		return NewHookObservedEvent(phase, hookName, hookID, dataObj)
	}

	// 出口 9：diagnostic
	// Python: serialization.py:429-434
	if eventType == "diagnostic" {
		level, err := parseDiagnosticLevel(data["level"], "diagnostic.level")
		if err != nil {
			return nil, err
		}
		message, err := asString(data, "message")
		if err != nil {
			return nil, err
		}
		dataObj := map[string]any{}
		if raw, present := data["data"]; present && raw != nil {
			dataObj, err = asJsonObject(raw, "diagnostic.data")
			if err != nil {
				return nil, err
			}
		}
		return NewDiagnosticEvent(level, message, dataObj)
	}

	// 出口 10：provider
	// Python: serialization.py:435-441
	if eventType == "provider" {
		providerName, err := asString(data, "provider")
		if err != nil {
			return nil, err
		}
		innerType, err := asString(data, "event_type")
		if err != nil {
			return nil, err
		}
		innerSchema, err := asString(data, "schema_version")
		if err != nil {
			return nil, err
		}
		payloadObj := map[string]any{}
		if raw, present := data["payload"]; present && raw != nil {
			payloadObj, err = asJsonObject(raw, "provider.payload")
			if err != nil {
				return nil, err
			}
		}
		return NewProviderEvent(providerName, innerType, innerSchema, payloadObj)
	}

	// 出口 11：未知 event_type → 原样保留
	// Python: serialization.py:442
	frozen, err := FreezeJsonObject(data)
	if err != nil {
		return nil, err
	}
	return UnknownEvent{EventType: eventType, SchemaVersion: schemaVersion, Payload: frozen}, nil
}
