package trajectory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"

	obs "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/observability"
)

// extractor.go 会话 tracer 记录到规范 OTLP 的直接抽取器。
// 对齐 Python: agent_evolving/trajectory/offline/extractor.py（全文，2026-12-04 快照）。
// Python 侧 span 为鸭子类型（getattr）；Go 侧输入是 tracer.TraceAgentSpan，
// 属性访问映射到结构体字段（_get(span, name) 等价面）。

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryExtractor 轨迹提取器接口。
// Python: offline/extractor.py TrajectoryExtractor。
type TrajectoryExtractor interface {
	// Extract 从 Session 提取轨迹。
	// Python: TrajectoryExtractor.extract(session, case_id)。
	Extract(sess *session.Session, caseID string) (*Trajectory, error)
}

// TracerTrajectoryExtractor 基于 Tracer Span 的轨迹提取器。
// Python: offline/extractor.py TrajectoryExtractor(resource_manager)。
type TracerTrajectoryExtractor struct {
	// resourceManager 用于查询 Tool 元数据（可选）。
	// Python: self._resource_manager。
	resourceManager *resources_manager.ResourceMgr
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTracerTrajectoryExtractor 创建基于 Tracer 的轨迹提取器。
// Python: TrajectoryExtractor(resource_manager=resource_manager)。
func NewTracerTrajectoryExtractor(resourceManager ...*resources_manager.ResourceMgr) *TracerTrajectoryExtractor {
	e := &TracerTrajectoryExtractor{}
	if len(resourceManager) > 0 {
		e.resourceManager = resourceManager[0]
	}
	return e
}

// Extract 从 Session 提取轨迹。
// Python: offline/extractor.py TrajectoryExtractor.extract。
func (e *TracerTrajectoryExtractor) Extract(sess *session.Session, caseID string) (*Trajectory, error) {
	spans := e.getAgentSpans(sess)
	effectiveCaseID := caseID
	if effectiveCaseID == "" {
		effectiveCaseID = "unknown"
	}
	builder, err := NewTrajectoryBuilder(effectiveCaseID, "offline", &effectiveCaseID, nil, nil, nil, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(effectiveCaseID))
	traceID := hex.EncodeToString(digest[:])[:32]
	for index, span := range spans {
		built, buildErr := e.buildSpan(span, traceID, index)
		if buildErr != nil {
			// Python: logger.exception(...) 后继续。
			logger.Error(logComponent).Int("index", index).Err(buildErr).
				Msg("[TrajectoryExtractor] failed to normalize offline span")
			continue
		}
		if recordErr := builder.RecordSpan(built); recordErr != nil {
			logger.Error(logComponent).Int("index", index).Err(recordErr).
				Msg("[TrajectoryExtractor] failed to record offline span")
		}
	}
	return builder.Build()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// spanGet 鸭子类型 _get(span, name) 等价面：按名字访问 tracer span 的
// Python 可见属性（MetaData 键优先于结构体字段，与 Python getattr 的
// 实例属性优先一致；两者皆缺返回 nil）。
func spanGet(span *tracer.TraceAgentSpan, name string) any {
	if span == nil {
		return nil
	}
	if v, ok := span.MetaData[name]; ok && v != nil {
		return v
	}
	switch name {
	case "name":
		if span.Name != "" {
			return span.Name
		}
	case "invoke_type":
		if span.InvokeType != "" {
			return span.InvokeType
		}
	case "inputs":
		return span.Inputs
	case "outputs":
		return span.Outputs
	case "error":
		return span.Error
	case "invoke_id":
		if span.InvokeID != "" {
			return span.InvokeID
		}
	case "parent_span_id", "parentInvokeId":
		if span.ParentInvokeID != "" {
			return span.ParentInvokeID
		}
	case "start_time":
		return span.StartTime
	case "end_time":
		return span.EndTime
	case "on_invoke_data":
		return span.OnInvokeData
	}
	return nil
}

// unwrap 单键映射展开。
// Python: offline/extractor.py _unwrap。
func unwrap(value any, key string) any {
	if m, isMap := value.(map[string]any); isMap && len(m) == 1 {
		if v, exists := m[key]; exists {
			return v
		}
	}
	return value
}

// dtToNanos 归一化 Session tracer 时间戳（datetime/毫秒/纳秒）。
// 数值小于 10^14 视为 Unix 毫秒，更大视为 Unix 纳秒；微秒时间戳不在
// 本适配器的输入契约内。
// Python: offline/extractor.py _dt_to_nanos。
func dtToNanos(value any) int64 {
	switch v := value.(type) {
	case *time.Time:
		if v == nil {
			return 0
		}
		return v.UnixNano()
	case time.Time:
		return v.UnixNano()
	case int:
		if abs64(int64(v)) >= 1e14 {
			return int64(v)
		}
		return int64(v) * 1_000_000
	case int64:
		if abs64(v) >= 1e14 {
			return v
		}
		return v * 1_000_000
	case float64:
		n := int64(v)
		if abs64(n) >= 1e14 {
			return n
		}
		return n * 1_000_000
	case string:
		var parsed int64
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
			if abs64(parsed) >= 1e14 {
				return parsed
			}
			return parsed * 1_000_000
		}
	}
	return 0
}

// abs64 绝对值。
func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// nativeID 读取原生 trace/span ID（int 补零/字符串修剪）。
// Python: offline/extractor.py _native_id。
func nativeID(span *tracer.TraceAgentSpan, names ...string) string {
	for _, name := range names {
		value := spanGet(span, name)
		if value == nil {
			continue
		}
		switch v := value.(type) {
		case int:
			width := 16
			if strings.Contains(strings.ToLower(name), "trace") {
				width = 32
			}
			return fmt.Sprintf("%0*x", width, v)
		case int64:
			width := 16
			if strings.Contains(strings.ToLower(name), "trace") {
				width = 32
			}
			return fmt.Sprintf("%0*x", width, v)
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		default:
			text := strings.TrimSpace(fmt.Sprint(v))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

// derivedID 派生 (trace, span) 身份（原生 ID 不足长时哈希补齐）。
// Python: offline/extractor.py _derived_id。
func derivedID(traceID string, span *tracer.TraceAgentSpan, index int) (string, string) {
	nativeTrace := nativeID(span, "trace_id", "traceId", "traceid")
	if nativeTrace == "" {
		nativeTrace = traceID
	}
	if len(nativeTrace) != 32 {
		digest := sha256.Sum256([]byte(nativeTrace))
		nativeTrace = hex.EncodeToString(digest[:])[:32]
	}
	nativeSpan := nativeID(span, "span_id", "spanId", "spanid")
	if nativeSpan == "" {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", nativeTrace, fmt.Sprint(spanGet(span, "invoke_id")), index)))
		nativeSpan = hex.EncodeToString(digest[:])[:16]
	} else if len(nativeSpan) != 16 {
		digest := sha256.Sum256([]byte(nativeSpan))
		nativeSpan = hex.EncodeToString(digest[:])[:16]
	}
	return nativeTrace, nativeSpan
}

// extractInputs 读 span 输入（unwrap 单键包装）。
// Python: offline/extractor.py _extract_inputs。
func extractInputs(span *tracer.TraceAgentSpan) any {
	return unwrap(toJsonCompatible(spanGet(span, "inputs")), "inputs")
}

// extractOutputs 读 span 输出。
// Python: offline/extractor.py _extract_outputs。
func extractOutputs(span *tracer.TraceAgentSpan) any {
	return unwrap(toJsonCompatible(spanGet(span, "outputs")), "outputs")
}

// llmParams 读 span 的 llm_params（on_invoke_data 首个携带者）。
// Python: offline/extractor.py _llm_params。
func llmParams(span *tracer.TraceAgentSpan) map[string]any {
	raw := spanGet(span, "on_invoke_data")
	var records []any
	if list, isList := raw.([]any); isList {
		records = list
	} else if m, isMap := raw.(map[string]any); isMap {
		records = []any{m}
	}
	for _, item := range records {
		record, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		if params, ok := record["llm_params"].(map[string]any); ok {
			return params
		}
	}
	return map[string]any{}
}

// extractorMessage 归一化消息（message 包装/choices/response 回退）。
// Python: offline/extractor.py _message。
func extractorMessage(value any, defaultRole string) map[string]any {
	compatible := toJsonCompatible(value)
	m, isMap := compatible.(map[string]any)
	if isMap {
		if inner, hasMessage := m["message"].(map[string]any); hasMessage {
			return extractorMessage(inner, defaultRole)
		}
		role := m["role"]
		if role == nil {
			role = defaultRole
		}
		content := m["content"]
		if role != nil || content != nil {
			item := map[string]any{"role": fmt.Sprint(role), "content": content}
			if content == nil {
				item["content"] = ""
			}
			for _, key := range []string{"name", "tool_calls"} {
				if v, exists := m[key]; exists {
					item[key] = copyJSON(v)
				}
			}
			return item
		}
		choices, choicesIsList := m["choices"].([]any)
		if choicesIsList && len(choices) > 0 {
			return extractorMessage(choices[0], defaultRole)
		}
		if _, hasResponse := m["response"]; hasResponse {
			return extractorMessage(m["response"], defaultRole)
		}
	}
	if compatible != nil {
		role := defaultRole
		if role == "" {
			role = "assistant"
		}
		return map[string]any{"role": role, "content": compatible}
	}
	return nil
}

// usageOf 读响应/参数里的 usage 映射。
// Python: offline/extractor.py _usage。
func usageOf(response any, params map[string]any) map[string]any {
	responseMap, _ := response.(map[string]any)
	if responseMap == nil {
		responseMap = map[string]any{}
	}
	usage, ok := responseMap["usage"].(map[string]any)
	if !ok {
		usage, ok = params["usage"].(map[string]any)
	}
	if !ok {
		return map[string]any{}
	}
	return usage
}

// statusAndEvents 由错误构造 status 与 exception 事件。
// Python: offline/extractor.py _status_and_events。
func statusAndEvents(errValue any) (map[string]any, []any) {
	if errValue == nil {
		return map[string]any{"code": "STATUS_CODE_OK"}, []any{}
	}
	message := fmt.Sprint(errValue)
	eventAttrs := map[string]any{
		"exception.type":    "error",
		"exception.message": message,
	}
	return map[string]any{"code": "STATUS_CODE_ERROR", "message": message}, []any{
		map[string]any{"name": "exception", "attributes": AttributesFromMap(eventAttrs)},
	}
}

// classifyKind 按调用类型归类 span。
// Python: offline/extractor.py TrajectoryExtractor._classify_kind。
func classifyKind(span *tracer.TraceAgentSpan) string {
	invokeType := strings.ToLower(fmt.Sprint(spanGet(span, "invoke_type")))
	if invokeType == "plugin" || invokeType == "tool" {
		return "tool"
	}
	if invokeType == "llm" {
		return "llm"
	}
	if invokeType != "" && invokeType != "<nil>" {
		return invokeType
	}
	return "agent"
}

// buildSpan 把一个 tracer span 转为规范 OTLP span。
// Python: offline/extractor.py TrajectoryExtractor._build_span。
func (e *TracerTrajectoryExtractor) buildSpan(span *tracer.TraceAgentSpan, traceID string, index int) (map[string]any, error) {
	kind := classifyKind(span)
	nativeTrace, nativeSpan := derivedID(traceID, span, index)
	name := fmt.Sprint(spanGet(span, "name"))
	if name == "<nil>" {
		name = ""
	}
	attrs := map[string]any{}
	var spanName string
	if kind == "llm" {
		params := llmParams(span)
		model := fmt.Sprint(firstValue(params, "model"))
		if model == "" || model == "<nil>" {
			model = fmt.Sprint(spanGet(span, "model"))
		}
		model = strings.TrimSpace(strings.TrimSuffix(model, "<nil>"))
		if model != "" {
			spanName = fmt.Sprintf("chat %s", model)
		} else {
			spanName = "chat"
		}
		attrs[obs.GenAIOperationName] = "chat"
		if model != "" {
			attrs[obs.GenAIRequestModel] = model
		}
		messages, _ := params["messages"].([]any)
		if messages == nil {
			if m, isMap := params["messages"].(map[string]any); isMap {
				messages = []any{m}
			}
		}
		var prompts []map[string]any
		for _, value := range messages {
			if message := extractorMessage(value, "user"); message != nil {
				prompts = append(prompts, message)
			}
		}
		response := extractOutputs(span)
		responseMessage := extractorMessage(response, "")
		var completions []map[string]any
		if responseMessage != nil {
			completions = append(completions, responseMessage)
		}
		for key, value := range WriteLLMExchange(prompts, completions) {
			attrs[key] = value
		}
		if tools, exists := params["tools"]; exists && tools != nil {
			attrs[obs.GenAIToolDefinitions] = toJsonCompatible(tools)
		}
		usage := usageOf(response, params)
		for _, pair := range []struct {
			keys   []string
			target string
		}{
			{[]string{"prompt_tokens", "input_tokens"}, obs.GenAIUsageInputTokens},
			{[]string{"completion_tokens", "output_tokens"}, obs.GenAIUsageOutputTokens},
		} {
			for _, key := range pair.keys {
				if value, exists := usage[key]; exists {
					attrs[pair.target] = value
					break
				}
			}
		}
		responseMap, _ := response.(map[string]any)
		if responseMap != nil {
			for _, pair := range []struct{ source, target string }{
				{"prompt_token_ids", RLPromptTokenIDs},
				{"completion_token_ids", RLCompletionTokenIDs},
				{"logprobs", RLLogprobs},
			} {
				if value, exists := responseMap[pair.source]; exists {
					attrs[pair.target] = toJsonCompatible(value)
				}
			}
		}
	} else if kind == "tool" {
		toolName := strings.TrimPrefix(name, "tool.")
		if toolName == "" || toolName == name {
			toolName = fmt.Sprint(firstValue(span.MetaData, "tool_name"))
		}
		if toolName == "" || toolName == "<nil>" {
			toolName = "unknown"
		}
		spanName = fmt.Sprintf("execute_tool %s", toolName)
		attrs[obs.GenAIOperationName] = "execute_tool"
		attrs[obs.GenAIToolName] = toolName
		attrs[obs.GenAIToolCallArguments] = extractInputs(span)
		attrs[obs.GenAIToolCallResult] = extractOutputs(span)
		toolID := firstValue(span.MetaData, "tool_call_id", "call_id")
		if toolID != nil {
			attrs[obs.GenAIToolCallID] = fmt.Sprint(toolID)
		}
		if toolInfo := e.toolInfo(toolName); toolInfo != nil {
			attrs[obs.GenAIToolDefinitions] = toolInfo
		}
	} else {
		if name != "" {
			spanName = name
		} else {
			spanName = kind
		}
		attrs[obs.OJSpanInput] = extractInputs(span)
		attrs[obs.OJSpanOutput] = extractOutputs(span)
		if agentID := firstValue(span.MetaData, "agent_id"); agentID != nil {
			attrs[obs.ATAgentID] = fmt.Sprint(agentID)
		}
	}
	status, events := statusAndEvents(span.Error)
	parentSpan := nativeID(span, "parent_span_id", "parentSpanId")
	result := map[string]any{
		"traceId":    nativeTrace,
		"spanId":     nativeSpan,
		"name":       spanName,
		"attributes": AttributesFromMap(attrs),
		"status":     status,
		"events":     events,
	}
	if parentSpan != "" {
		result["parentSpanId"] = parentSpan
	}
	startNanos := dtToNanos(spanGet(span, "start_time"))
	endNanos := dtToNanos(spanGet(span, "end_time"))
	if startNanos != 0 {
		result["startTimeUnixNano"] = fmt.Sprint(startNanos)
	}
	if endNanos != 0 {
		result["endTimeUnixNano"] = fmt.Sprint(endNanos)
	}
	return NormalizeSpan(result)
}

// toolInfo 查询工具元数据（resourceManager 可用时）。
// Python: offline/extractor.py TrajectoryExtractor._tool_info。
func (e *TracerTrajectoryExtractor) toolInfo(toolName string) any {
	if e.resourceManager == nil || toolName == "" {
		return nil
	}
	toolInfos, err := e.resourceManager.GetToolInfos([]string{toolName}, nil)
	if err != nil || len(toolInfos) == 0 {
		return nil
	}
	info := toolInfos[0]
	return map[string]any{
		"name":        toolName,
		"description": info.GetDescription(),
		"parameters":  toJsonCompatible(info.GetParameters()),
	}
}

// getAgentSpans 从 Session 的 Tracer 获取 Agent Span 列表。
// Python: offline/extractor.py TrajectoryExtractor._get_agent_spans。
func (e *TracerTrajectoryExtractor) getAgentSpans(sess *session.Session) []*tracer.TraceAgentSpan {
	if sess == nil {
		return nil
	}
	t := sess.Tracer()
	if t == nil {
		return nil
	}
	return t.AgentSpanManager.GetAllAgentSpans()
}
