package trajectory

import (
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryExtractor 轨迹提取器接口。
//
// 从 Agent 执行 Session 中提取完整 Trajectory。
// TracerTrajectoryExtractor 是默认实现。
//
// 对应 Python: openjiuwen/agent_evolving/trajectory/extractor.py TrajectoryExtractor
//
//	class TrajectoryExtractor:
//	    def extract(self, session: Any, case_id: Optional[str] = None) -> Trajectory: ...
type TrajectoryExtractor interface {
	// Extract 从 Session 提取 Trajectory。
	//
	// 对应 Python: TrajectoryExtractor.extract(session, case_id)
	Extract(sess *session.Session, caseID string) *Trajectory
}

// TracerTrajectoryExtractor 基于 Tracer Span 的轨迹提取器。
//
// 从 Session.tracer() 的 AgentSpanManager 中提取所有 Span，
// 逐个转换为 TrajectoryStep，通过 TrajectoryBuilder 组装完整 Trajectory。
//
// 对应 Python: openjiuwen/agent_evolving/trajectory/extractor.py TrajectoryExtractor
type TracerTrajectoryExtractor struct {
	// resourceManager 用于查询 Tool 元数据（可选）
	// 对应 Python: self._resource_manager
	resourceManager any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTracerTrajectoryExtractor 创建基于 Tracer 的轨迹提取器。
//
// 对应 Python: TrajectoryExtractor(resource_manager=resource_manager)
func NewTracerTrajectoryExtractor(resourceManager ...any) *TracerTrajectoryExtractor {
	e := &TracerTrajectoryExtractor{}
	if len(resourceManager) > 0 {
		e.resourceManager = resourceManager[0]
	}
	return e
}

// Extract 从 Session 提取 Trajectory。
//
// 对应 Python: TrajectoryExtractor.extract(session, case_id)
func (e *TracerTrajectoryExtractor) Extract(sess *session.Session, caseID string) *Trajectory {
	// 对齐 Python: tracer = self._get_tracer(session)
	agentSpans := e.getAgentSpans(sess)

	effectiveCaseID := caseID
	if effectiveCaseID == "" {
		effectiveCaseID = "unknown"
	}

	// 对齐 Python: builder = TrajectoryBuilder(session_id=effective_case_id, source="offline", case_id=effective_case_id)
	builder := NewTrajectoryBuilder(effectiveCaseID, "offline",
		WithCaseID(effectiveCaseID),
	)

	// 对齐 Python: for span in spans: step = self._build_step(span); builder.record_step(step)
	for _, span := range agentSpans {
		step := e.buildStep(span)
		builder.RecordStep(step)
	}

	return builder.Build()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getAgentSpans 从 Session 的 Tracer 中获取 Agent Span 列表。
//
// 对齐 Python: TrajectoryExtractor._get_agent_spans(tracer)
func (e *TracerTrajectoryExtractor) getAgentSpans(sess *session.Session) []*tracer.TraceAgentSpan {
	// 对齐 Python: tracer = getattr(session, "tracer", None); return tracer() if callable(tracer) else tracer
	if sess == nil {
		return nil
	}
	t := sess.Tracer()
	if t == nil {
		return nil
	}

	// 对齐 Python: agent_sm = getattr(tracer, "tracer_agent_span_manager", None)
	agentSM := t.AgentSpanManager
	if agentSM == nil {
		return nil
	}

	// 对齐 Python: result = get_spans(); return result if isinstance(result, list) else []
	return agentSM.GetAllAgentSpans()
}

// buildStep 将 TraceAgentSpan 转换为 TrajectoryStep。
//
// 对齐 Python: TrajectoryExtractor._build_step(span)
func (e *TracerTrajectoryExtractor) buildStep(span *tracer.TraceAgentSpan) *TrajectoryStep {
	kind := e.classifyKind(span)
	baseMeta := map[string]any{}
	if span.MetaData != nil {
		baseMeta = span.MetaData
	}
	detail := e.buildDetail(span, kind)
	fullMeta := e.buildMeta(span, baseMeta, kind, detail)

	// 对齐 Python: 从 LLMCallDetail.response 中提取 prompt_token_ids/completion_token_ids/logprobs
	var promptTokenIDs []int
	var completionTokenIDs []int
	var logprobs any
	if llmDetail, ok := detail.(*LLMCallDetail); ok && llmDetail != nil && llmDetail.Response != nil {
		if ptids, ok := llmDetail.Response["prompt_token_ids"]; ok {
			if ids, ok := ptids.([]int); ok {
				promptTokenIDs = ids
				delete(llmDetail.Response, "prompt_token_ids")
			}
		}
		if ctids, ok := llmDetail.Response["completion_token_ids"]; ok {
			if ids, ok := ctids.([]int); ok {
				completionTokenIDs = ids
				delete(llmDetail.Response, "completion_token_ids")
			}
		}
		if lp, ok := llmDetail.Response["logprobs"]; ok {
			logprobs = lp
			delete(llmDetail.Response, "logprobs")
		}
	}

	var errorMap map[string]any
	if span.Error != nil {
		errorMap = span.Error
	}

	return &TrajectoryStep{
		Kind:               kind,
		Error:              errorMap,
		StartTimeMs:        dtToMs(span.StartTime),
		EndTimeMs:          dtToMs(span.EndTime),
		Detail:             detail,
		PromptTokenIDs:     promptTokenIDs,
		CompletionTokenIDs: completionTokenIDs,
		Logprobs:           logprobs,
		Meta:               fullMeta,
	}
}

// buildDetail 根据 kind 构建 StepDetail。
//
// 对齐 Python: TrajectoryExtractor._build_detail(span, kind)
func (e *TracerTrajectoryExtractor) buildDetail(span *tracer.TraceAgentSpan, kind StepKind) StepDetail {
	switch kind {
	case StepKindLLM:
		return e.buildLLMDetail(&span.Span)
	case StepKindTool:
		return e.buildToolDetail(span)
	default:
		return nil
	}
}

// buildLLMDetail 从 Span 构建LLMCallDetail。
//
// 对齐 Python: TrajectoryExtractor._build_llm_detail(span)
func (e *TracerTrajectoryExtractor) buildLLMDetail(span *tracer.Span) *LLMCallDetail {
	// 对齐 Python: on_invoke = getattr(span, "on_invoke_data", None) or []
	onInvoke := span.OnInvokeData
	if len(onInvoke) == 0 {
		return nil
	}

	// 对齐 Python: for record in on_invoke: if isinstance(record, dict) and "llm_params" in record
	var llmParams map[string]any
	for _, record := range onInvoke {
		if lp, ok := record["llm_params"]; ok {
			if m, ok := lp.(map[string]any); ok {
				llmParams = m
				break
			}
		}
	}
	if llmParams == nil {
		return nil
	}

	// 对齐 Python: outputs = self._extract_outputs(span); response = self._parse_llm_response(outputs)
	outputs := extractOutputs(span)
	response := parseLLMResponse(outputs)

	// 对齐 Python: usage = response.get("usage") if response else None
	var usage map[string]any
	if response != nil {
		if u, ok := response["usage"]; ok {
			if m, ok := u.(map[string]any); ok {
				usage = m
			}
		}
	}
	// 对齐 Python: if not usage and isinstance(llm_params, dict): usage = llm_params.get("usage")
	if usage == nil {
		if u, ok := llmParams["usage"]; ok {
			if m, ok := u.(map[string]any); ok {
				usage = m
			}
		}
	}

	// 对齐 Python: messages=llm_params.get("messages", [])
	var messages []map[string]any
	if m, ok := llmParams["messages"]; ok {
		if ms, ok := m.([]map[string]any); ok {
			messages = ms
		}
	}

	// 对齐 Python: tools=llm_params.get("tools")
	var tools []map[string]any
	if t, ok := llmParams["tools"]; ok {
		if ts, ok := t.([]map[string]any); ok {
			tools = ts
		}
	}

	modelName, _ := llmParams["model"].(string)

	return &LLMCallDetail{
		Model:    modelName,
		Messages: messages,
		Tools:    tools,
		Response: response,
		Usage:    usage,
	}
}

// buildToolDetail 从 Span 构建 ToolCallDetail。
//
// 对齐 Python: TrajectoryExtractor._build_tool_detail(span)
func (e *TracerTrajectoryExtractor) buildToolDetail(span *tracer.TraceAgentSpan) *ToolCallDetail {
	// 对齐 Python: tool_name = getattr(span, "name", "") or ""
	toolName := span.Name

	var toolDescription string
	var toolSchema map[string]any

	// 对齐 Python: if self._resource_manager is not None and tool_name
	//     Python: tool_info = self._resource_manager.get_tool_infos(tool_name)
	if e.resourceManager != nil && toolName != "" {
		// Go 中 resourceManager 类型待定，当前为 any
		// 后续通过 ResourceManager 接口调用
		// TODO: 实现 resourceManager.get_tool_infos(tool_name)
		_ = toolDescription
		_ = toolSchema
	}

	return &ToolCallDetail{
		ToolName:        toolName,
		CallArgs:        extractInputsAsMap(&span.Span),
		CallResult:      extractOutputsAsMap(&span.Span),
		ToolDescription: toolDescription,
		ToolSchema:      toolSchema,
	}
}

// buildMeta 构建步骤元数据。
//
// 对齐 Python: TrajectoryExtractor._build_meta(span, base_meta, kind, detail)
func (e *TracerTrajectoryExtractor) buildMeta(span *tracer.TraceAgentSpan, baseMeta map[string]any, kind StepKind, detail StepDetail) map[string]any {
	// 对齐 Python: meta = copy.deepcopy(base_meta)
	meta := make(map[string]any, len(baseMeta)+5)
	for k, v := range baseMeta {
		meta[k] = v
	}

	// 对齐 Python: meta["operator_id"] = self._get_operator_id(span, base_meta)
	meta["operator_id"] = e.getOperatorID(span, baseMeta)

	// 对齐 Python: meta["span_name"] = getattr(span, "name", None)
	meta["span_name"] = span.Name

	// 对齐 Python: agent_id = getattr(span, "agent_id", None) or base_meta.get("agent_id")
	// Go 中 TraceAgentSpan 没有 AgentID 字段，从 baseMeta 回退
	if aid, ok := baseMeta["agent_id"]; ok && aid != nil {
		meta["agent_id"] = aid
	}

	// 对齐 Python: if kind not in ("llm", "tool"): meta["inputs"] = ...; meta["outputs"] = ...
	if kind != StepKindLLM && kind != StepKindTool {
		meta["inputs"] = extractInputs(&span.Span)
		meta["outputs"] = extractOutputs(&span.Span)
	}

	// 对齐 Python: meta["invoke_id"] = getattr(span, "invoke_id", None)
	// 对齐 Python: meta["parent_invoke_id"] = getattr(span, "parent_invoke_id", None)
	// 对齐 Python: meta["child_invokes"] = getattr(span, "child_invokes_id", None)
	meta["invoke_id"] = span.InvokeID
	meta["parent_invoke_id"] = span.ParentInvokeID
	if len(span.ChildInvokesID) > 0 {
		meta["child_invokes"] = span.ChildInvokesID
	}

	_ = detail // 避免 unused 警告
	return meta
}

// classifyKind 根据 invoke_type 判断步骤类型。
//
// 对齐 Python: TrajectoryExtractor._classify_kind(span)
func (e *TracerTrajectoryExtractor) classifyKind(span *tracer.TraceAgentSpan) StepKind {
	// 对齐 Python: invoke_type = getattr(span, "invoke_type", None); invoke_str = str(invoke_type) if invoke_type else ""
	switch span.InvokeType {
	case "plugin":
		return StepKindTool
	case "llm":
		return StepKindLLM
	case "workflow":
		return StepKindWorkflow
	case "memory":
		return StepKindMemory
	default:
		return StepKindAgent
	}
}

// getOperatorID 从 Span 属性中提取 operator ID。
//
// 对齐 Python: TrajectoryExtractor._get_operator_id(span, meta)
// 优先级：operator_id > llm_call_id > meta.operator_id > name
func (e *TracerTrajectoryExtractor) getOperatorID(span *tracer.TraceAgentSpan, meta map[string]any) string {
	// 对齐 Python: getattr(span, "operator_id", None)
	if v, ok := span.MetaData["operator_id"]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}

	// 对齐 Python: getattr(span, "llm_call_id", None)
	if v, ok := meta["llm_call_id"]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}

	// 对齐 Python: meta.get("operator_id")
	if v, ok := meta["operator_id"]; ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}

	// 对齐 Python: getattr(span, "name", None)
	if span.Name != "" {
		return span.Name
	}

	return ""
}

// dtToMs 将 time.Time 转换为毫秒时间戳。
//
// 对齐 Python: _dt_to_ms(dt) -> int(dt.timestamp() * 1000) if dt else None
func dtToMs(t *time.Time) int {
	if t == nil {
		return 0
	}
	return int(t.UnixMilli())
}

// parseLLMResponse 解析 LLM 响应。
//
// 对齐 Python: TrajectoryExtractor._parse_llm_response(outputs)
func parseLLMResponse(outputs any) map[string]any {
	if outputs == nil {
		return nil
	}
	switch v := outputs.(type) {
	case map[string]any:
		return v
	default:
		return nil
	}
}

// extractInputs 从 Span 提取输入数据。
//
// 对齐 Python: TrajectoryExtractor._extract_inputs(span)
// Python: raw = getattr(span, "inputs", None); if isinstance(raw, dict) and "inputs" in raw: return raw["inputs"]
func extractInputs(span *tracer.Span) any {
	raw := span.Inputs
	if m, ok := raw.(map[string]any); ok {
		if inner, ok := m["inputs"]; ok {
			return inner
		}
	}
	return raw
}

// extractOutputs 从 Span 提取输出数据。
//
// 对齐 Python: TrajectoryExtractor._extract_outputs(span)
func extractOutputs(span *tracer.Span) any {
	raw := span.Outputs
	if m, ok := raw.(map[string]any); ok {
		if inner, ok := m["outputs"]; ok {
			return inner
		}
	}
	return raw
}

// extractInputsAsMap 从 Span 提取输入数据为 map[string]any。
func extractInputsAsMap(span *tracer.Span) map[string]any {
	raw := extractInputs(span)
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return nil
}

// extractOutputsAsMap 从 Span 提取输出数据为 map[string]any。
func extractOutputsAsMap(span *tracer.Span) map[string]any {
	raw := extractOutputs(span)
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return nil
}
