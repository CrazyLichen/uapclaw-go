package tracer

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTracer 测试 NewTracer 自动生成 traceID 和 AgentSpanManager
func TestNewTracer(t *testing.T) {
	tracer := NewTracer()

	if tracer.traceID == "" {
		t.Fatal("traceID 不应为空")
	}
	if tracer.AgentSpanManager == nil {
		t.Fatal("AgentSpanManager 不应为 nil")
	}
	if tracer.AgentSpanManager.traceID != tracer.traceID {
		t.Fatalf("AgentSpanManager.traceID 不匹配: 期望 %s, 实际 %s", tracer.traceID, tracer.AgentSpanManager.traceID)
	}
	if tracer.WorkflowSpanManagerDict == nil {
		t.Fatal("WorkflowSpanManagerDict 不应为 nil")
	}
	if len(tracer.WorkflowSpanManagerDict) != 0 {
		t.Fatalf("WorkflowSpanManagerDict 初始应为空, 实际长度 %d", len(tracer.WorkflowSpanManagerDict))
	}
}

// TestTracer_Init 测试 Init 方法创建 Handler 和分发表
func TestTracer_Init(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	// 验证 AgentSpanManager
	if tracer.AgentSpanManager == nil {
		t.Fatal("AgentSpanManager 不应为 nil")
	}

	// 验证 agentHandler
	if tracer.agentHandler == nil {
		t.Fatal("agentHandler 不应为 nil")
	}

	// 验证 agentDispatch 包含 22 个 Agent 事件
	expectedAgentEvents := []TraceEvent{
		TraceChainStart, TraceChainEnd, TraceChainError,
		TraceLLMStart, TraceLLMRequest, TraceLLMEnd, TraceLLMError,
		TracePromptStart, TracePromptEnd, TracePromptError,
		TracePluginStart, TracePluginEnd, TracePluginError,
		TraceRetrieverStart, TraceRetrieverEnd, TraceRetrieverError,
		TraceEvaluatorStart, TraceEvaluatorEnd, TraceEvaluatorError,
		TraceWorkflowStart, TraceWorkflowEnd, TraceWorkflowError,
	}
	if len(tracer.agentDispatch) != len(expectedAgentEvents) {
		t.Fatalf("agentDispatch 数量不匹配: 期望 %d, 实际 %d", len(expectedAgentEvents), len(tracer.agentDispatch))
	}
	for _, event := range expectedAgentEvents {
		if _, ok := tracer.agentDispatch[event]; !ok {
			t.Fatalf("agentDispatch 缺少事件: %s", event)
		}
	}

	// 验证默认 Workflow 事件分发表包含 8 个事件
	expectedWFEvents := []TraceEvent{
		TraceWFCallStart, TraceWFPreInvoke, TraceWFPreStream,
		TraceWFInvoke, TraceWFPostStream, TraceWFPostInvoke,
		TraceWFCallDone, TraceWFInteract,
	}
	defaultDispatch, ok := tracer.workflowDispatch[""]
	if !ok {
		t.Fatal("workflowDispatch 缺少默认 key \"\"")
	}
	if len(defaultDispatch) != len(expectedWFEvents) {
		t.Fatalf("默认 workflowDispatch 数量不匹配: 期望 %d, 实际 %d", len(expectedWFEvents), len(defaultDispatch))
	}
	for _, event := range expectedWFEvents {
		if _, ok := defaultDispatch[event]; !ok {
			t.Fatalf("默认 workflowDispatch 缺少事件: %s", event)
		}
	}
}

// TestTracer_Init_SpanManagerDict默认条目 测试 Init 后 WorkflowSpanManagerDict 包含 "" key
func TestTracer_Init_SpanManagerDict默认条目(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	defaultSM, ok := tracer.WorkflowSpanManagerDict[""]
	if !ok {
		t.Fatal("WorkflowSpanManagerDict 缺少默认 key \"\"")
	}
	if defaultSM == nil {
		t.Fatal("默认 SpanManager 不应为 nil")
	}
	if defaultSM.traceID != tracer.traceID {
		t.Fatalf("默认 SpanManager.traceID 不匹配: 期望 %s, 实际 %s", tracer.traceID, defaultSM.traceID)
	}

	// 验证默认 workflow handler 存在
	if tracer.workflowHandlers[""] == nil {
		t.Fatal("默认 workflow handler 不应为 nil")
	}
}

// TestTracer_TriggerAgent_LLMStart 测试 TriggerAgent 分发到 OnLLMStart，验证 span 字段更新
func TestTracer_TriggerAgent_LLMStart(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()
	params := &TriggerParams{
		Inputs: "test input",
		InstanceInfo: map[string]any{
			"class_name": "TestAgent",
		},
	}

	// 触发 LLMStart 事件
	tracer.TriggerAgent(ctx, TraceLLMStart, params)

	// 验证 AgentSpanManager 中已创建 span
	lastSpan := tracer.AgentSpanManager.LastSpan()
	if lastSpan == nil {
		t.Fatal("AgentSpanManager.LastSpan() 不应为 nil")
	}
	if lastSpan.StartTime == nil {
		t.Fatal("span.StartTime 不应为 nil")
	}
}

// TestTracer_TriggerAgent_LLMEnd 测试 TriggerAgent 分发到 OnLLMEnd
func TestTracer_TriggerAgent_LLMEnd(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()

	// 先触发 LLMStart 创建 span
	tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Inputs:       "test input",
		InstanceInfo: map[string]any{"class_name": "TestAgent"},
	})

	// 再触发 LLMEnd
	tracer.TriggerAgent(ctx, TraceLLMEnd, &TriggerParams{
		Outputs: "test output",
	})

	// 验证 span 的 EndTime 已设置
	lastSpan := tracer.AgentSpanManager.LastSpan()
	if lastSpan == nil {
		t.Fatal("AgentSpanManager.LastSpan() 不应为 nil")
	}
	if lastSpan.EndTime == nil {
		t.Fatal("span.EndTime 不应为 nil")
	}
}

// TestTracer_TriggerAgent_LLMError 测试 TriggerAgent 分发到 OnLLMError
func TestTracer_TriggerAgent_LLMError(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()

	// 先触发 LLMStart 创建 span
	tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Inputs:       "test input",
		InstanceInfo: map[string]any{"class_name": "TestAgent"},
	})

	// 再触发 LLMError
	tracer.TriggerAgent(ctx, TraceLLMError, &TriggerParams{
		Error: errTestLLM,
	})

	// 验证 span 的 Error 已设置
	lastSpan := tracer.AgentSpanManager.LastSpan()
	if lastSpan == nil {
		t.Fatal("AgentSpanManager.LastSpan() 不应为 nil")
	}
	if lastSpan.Error == nil {
		t.Fatal("span.Error 不应为 nil")
	}
}

// TestTracer_TriggerAgent_未注册事件 测试未注册事件不 panic 且输出 Warn 日志
func TestTracer_TriggerAgent_未注册事件(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()

	// 触发未注册的 Agent 事件，不应 panic
	tracer.TriggerAgent(ctx, TraceEvent("on_unknown_event"), &TriggerParams{})
}

// TestTracer_TriggerAgent_所有Start事件 测试所有 Start 类事件均可正常分发
func TestTracer_TriggerAgent_所有Start事件(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()
	instanceInfo := map[string]any{"class_name": "TestComponent"}

	startEvents := []TraceEvent{
		TraceChainStart, TraceLLMStart, TracePromptStart,
		TracePluginStart, TraceRetrieverStart, TraceEvaluatorStart,
		TraceWorkflowStart,
	}
	for _, event := range startEvents {
		tracer.TriggerAgent(ctx, event, &TriggerParams{
			Inputs:       "test input",
			InstanceInfo: instanceInfo,
		})
	}

	// 验证创建了 7 个 span
	if len(tracer.AgentSpanManager.order) != 7 {
		t.Fatalf("AgentSpanManager span 数量不匹配: 期望 7, 实际 %d", len(tracer.AgentSpanManager.order))
	}
}

// TestTracer_TriggerAgent_LLMRequest 测试 LLMRequest 事件分发
func TestTracer_TriggerAgent_LLMRequest(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()

	// 先触发 LLMStart
	tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Inputs:       "test input",
		InstanceInfo: map[string]any{"class_name": "TestAgent"},
	})

	// 触发 LLMRequest
	tracer.TriggerAgent(ctx, TraceLLMRequest, &TriggerParams{
		OnInvokeData: map[string]any{"token_count": 100},
	})

	lastSpan := tracer.AgentSpanManager.LastSpan()
	if lastSpan == nil {
		t.Fatal("AgentSpanManager.LastSpan() 不应为 nil")
	}
}

// TestTracer_TriggerAgent_EndError事件 测试所有 End/Error 类事件
func TestTracer_TriggerAgent_EndError事件(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()
	instanceInfo := map[string]any{"class_name": "TestComponent"}

	// 先创建一个 span
	tracer.TriggerAgent(ctx, TraceChainStart, &TriggerParams{
		Inputs:       "test",
		InstanceInfo: instanceInfo,
	})

	// 测试 ChainEnd
	tracer.TriggerAgent(ctx, TraceChainEnd, &TriggerParams{Outputs: "chain result"})

	// 再创建一个 span 测试 ChainError
	tracer.TriggerAgent(ctx, TraceChainStart, &TriggerParams{
		Inputs:       "test",
		InstanceInfo: instanceInfo,
	})
	tracer.TriggerAgent(ctx, TraceChainError, &TriggerParams{Error: errTestLLM})

	// 测试 PromptEnd/Error
	tracer.TriggerAgent(ctx, TracePromptStart, &TriggerParams{Inputs: "p", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TracePromptEnd, &TriggerParams{Outputs: "prompt result"})

	tracer.TriggerAgent(ctx, TracePromptStart, &TriggerParams{Inputs: "p", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TracePromptError, &TriggerParams{Error: errTestLLM})

	// 测试 PluginEnd/Error
	tracer.TriggerAgent(ctx, TracePluginStart, &TriggerParams{Inputs: "pl", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TracePluginEnd, &TriggerParams{Outputs: "plugin result"})

	tracer.TriggerAgent(ctx, TracePluginStart, &TriggerParams{Inputs: "pl", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TracePluginError, &TriggerParams{Error: errTestLLM})

	// 测试 RetrieverEnd/Error
	tracer.TriggerAgent(ctx, TraceRetrieverStart, &TriggerParams{Inputs: "r", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TraceRetrieverEnd, &TriggerParams{Outputs: "retriever result"})

	tracer.TriggerAgent(ctx, TraceRetrieverStart, &TriggerParams{Inputs: "r", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TraceRetrieverError, &TriggerParams{Error: errTestLLM})

	// 测试 EvaluatorEnd/Error
	tracer.TriggerAgent(ctx, TraceEvaluatorStart, &TriggerParams{Inputs: "e", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TraceEvaluatorEnd, &TriggerParams{Outputs: "evaluator result"})

	tracer.TriggerAgent(ctx, TraceEvaluatorStart, &TriggerParams{Inputs: "e", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TraceEvaluatorError, &TriggerParams{Error: errTestLLM})

	// 测试 WorkflowEnd/Error
	tracer.TriggerAgent(ctx, TraceWorkflowStart, &TriggerParams{Inputs: "w", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TraceWorkflowEnd, &TriggerParams{Outputs: "workflow result"})

	tracer.TriggerAgent(ctx, TraceWorkflowStart, &TriggerParams{Inputs: "w", InstanceInfo: instanceInfo})
	tracer.TriggerAgent(ctx, TraceWorkflowError, &TriggerParams{Error: errTestLLM})
}

// TestTracer_TriggerAgent_带Span参数 测试 TriggerParams.Span 非空时的分发路径
func TestTracer_TriggerAgent_带Span参数(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()

	// 先创建一个 span
	tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Inputs:       "test input",
		InstanceInfo: map[string]any{"class_name": "TestAgent"},
	})

	// 获取已创建的 span
	lastSpan := tracer.AgentSpanManager.LastSpan()

	// 使用带 Span 的 params 触发 End 事件
	tracer.TriggerAgent(ctx, TraceLLMEnd, &TriggerParams{
		Span:    lastSpan,
		Outputs: "test output",
	})

	// 验证 span 更新
	if lastSpan.EndTime == nil {
		t.Fatal("span.EndTime 不应为 nil")
	}
}

// TestTracer_TriggerWorkflow_CallStart 测试 TriggerWorkflow 分发到 OnCallStart
func TestTracer_TriggerWorkflow_CallStart(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()
	params := &TriggerParams{
		InvokeID: "invoke-001",
		Metadata: map[string]any{
			"ComponentID":   "comp-1",
			"ComponentName": "TestComponent",
			"ComponentType": "LLM",
		},
		Inputs:   "test input",
		NeedSend: true,
	}

	// 触发默认 parentNodeID 的 CallStart 事件
	tracer.TriggerWorkflow(ctx, TraceWFCallStart, "", params)

	// 验证默认 SpanManager 中已创建 span
	defaultSM := tracer.WorkflowSpanManagerDict[""]
	baseSpan := defaultSM.GetSpan("invoke-001")
	if baseSpan == nil {
		t.Fatal("默认 SpanManager 中应存在 invoke-001 的 span")
	}
	if baseSpan.StartTime == nil {
		t.Fatal("span.StartTime 不应为 nil")
	}
}

// TestTracer_TriggerWorkflow_带ParentNodeID 测试带 parentNodeID 的 Workflow 触发
func TestTracer_TriggerWorkflow_带ParentNodeID(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	// 注册新的 Workflow SpanManager
	parentNodeID := "node-parent-1"
	tracer.RegisterWorkflowSpanManager(parentNodeID)

	ctx := context.Background()
	params := &TriggerParams{
		InvokeID: "invoke-002",
		Metadata: map[string]any{
			"ComponentID":   "comp-2",
			"ComponentName": "ChildComponent",
			"ComponentType": "Start",
		},
		Inputs:   "child input",
		NeedSend: false,
	}

	// 触发带 parentNodeID 的 CallStart 事件
	tracer.TriggerWorkflow(ctx, TraceWFCallStart, parentNodeID, params)

	// 验证对应 SpanManager 中已创建 span
	sm := tracer.WorkflowSpanManagerDict[parentNodeID]
	baseSpan := sm.GetSpan("invoke-002")
	if baseSpan == nil {
		t.Fatal("parentNodeID 对应的 SpanManager 中应存在 invoke-002 的 span")
	}

	// 触发未注册的 parentNodeID 不应 panic
	tracer.TriggerWorkflow(ctx, TraceWFCallStart, "non-existent-node", &TriggerParams{
		InvokeID: "invoke-999",
	})
}

// TestTracer_TriggerWorkflow_未注册事件 测试 Workflow 未注册事件
func TestTracer_TriggerWorkflow_未注册事件(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()

	// 触发已注册 parentNodeID 但未注册事件，不应 panic
	tracer.TriggerWorkflow(ctx, TraceEvent("on_unknown_wf_event"), "", &TriggerParams{
		InvokeID: "invoke-unknown",
	})
}

// TestTracer_TriggerWorkflow_所有Workflow事件 测试所有 Workflow 事件均可正常分发
func TestTracer_TriggerWorkflow_所有Workflow事件(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()
	invokeID := "invoke-all-wf"

	// OnCallStart
	tracer.TriggerWorkflow(ctx, TraceWFCallStart, "", &TriggerParams{
		InvokeID: invokeID,
		Metadata: map[string]any{
			"ComponentID": "comp-1", "ComponentName": "TestComp", "ComponentType": "LLM",
		},
		Inputs:    "input",
		NeedSend:  false,
		SourceIDs: []string{"src-1"},
	})

	// OnPreInvoke
	tracer.TriggerWorkflow(ctx, TraceWFPreInvoke, "", &TriggerParams{
		InvokeID:          invokeID,
		Inputs:            "pre-invoke-input",
		ComponentMetadata: map[string]any{"ComponentType": "LLM"},
		NeedSend:          false,
	})

	// OnPreStream
	tracer.TriggerWorkflow(ctx, TraceWFPreStream, "", &TriggerParams{
		InvokeID: invokeID,
		Chunk:    map[string]any{"data": "chunk1"},
		NeedSend: false,
	})

	// OnInvoke（无错误）
	tracer.TriggerWorkflow(ctx, TraceWFInvoke, "", &TriggerParams{
		InvokeID:     invokeID,
		OnInvokeData: map[string]any{"step": "processing"},
	})

	// OnPostStream
	tracer.TriggerWorkflow(ctx, TraceWFPostStream, "", &TriggerParams{
		InvokeID: invokeID,
		Chunk:    "stream-chunk",
	})

	// OnPostInvoke
	tracer.TriggerWorkflow(ctx, TraceWFPostInvoke, "", &TriggerParams{
		InvokeID: invokeID,
		Outputs:  "post-invoke-output",
	})

	// OnCallDone
	tracer.TriggerWorkflow(ctx, TraceWFCallDone, "", &TriggerParams{
		InvokeID: invokeID,
		Outputs:  "done-output",
	})

	// OnInteract
	invokeID2 := "invoke-interact"
	tracer.TriggerWorkflow(ctx, TraceWFCallStart, "", &TriggerParams{
		InvokeID: invokeID2,
		Metadata: map[string]any{
			"ComponentID": "comp-2", "ComponentName": "InteractComp", "ComponentType": "Start",
		},
		Inputs:   "input",
		NeedSend: false,
	})
	tracer.TriggerWorkflow(ctx, TraceWFInteract, "", &TriggerParams{
		InvokeID:          invokeID2,
		Inputs:            "interact-input",
		ComponentMetadata: map[string]any{"ComponentType": "Start"},
		NeedSend:          false,
	})

	// OnInvoke（带错误）
	invokeID3 := "invoke-error"
	tracer.TriggerWorkflow(ctx, TraceWFCallStart, "", &TriggerParams{
		InvokeID: invokeID3,
		Metadata: map[string]any{
			"ComponentID": "comp-3", "ComponentName": "ErrorComp", "ComponentType": "Start",
		},
		Inputs:   "input",
		NeedSend: false,
	})
	tracer.TriggerWorkflow(ctx, TraceWFInvoke, "", &TriggerParams{
		InvokeID:     invokeID3,
		OnInvokeData: map[string]any{"step": "failed"},
		Error:        errTestLLM,
	})

	// 验证 span 存在
	defaultSM := tracer.WorkflowSpanManagerDict[""]
	if defaultSM.GetSpan(invokeID) == nil {
		t.Fatal("默认 SpanManager 中应存在 invokeID 的 span")
	}
}

// TestTracer_RegisterWorkflowSpanManager 测试注册新的 Workflow SpanManager
func TestTracer_RegisterWorkflowSpanManager(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	parentNodeID := "parent-node-1"
	tracer.RegisterWorkflowSpanManager(parentNodeID)

	// 验证 WorkflowSpanManagerDict 中新增条目
	sm, ok := tracer.WorkflowSpanManagerDict[parentNodeID]
	if !ok {
		t.Fatal("WorkflowSpanManagerDict 缺少注册的 parentNodeID")
	}
	if sm == nil {
		t.Fatal("注册的 SpanManager 不应为 nil")
	}
	if sm.traceID != tracer.traceID {
		t.Fatalf("SpanManager.traceID 不匹配: 期望 %s, 实际 %s", tracer.traceID, sm.traceID)
	}
	if sm.parentID != parentNodeID {
		t.Fatalf("SpanManager.parentID 不匹配: 期望 %s, 实际 %s", parentNodeID, sm.parentID)
	}

	// 验证 workflowHandlers 中新增条目
	handler := tracer.workflowHandlers[parentNodeID]
	if handler == nil {
		t.Fatal("注册的 workflow handler 不应为 nil")
	}

	// 验证 workflowDispatch 中新增条目
	dispatch, ok := tracer.workflowDispatch[parentNodeID]
	if !ok {
		t.Fatal("workflowDispatch 缺少注册的 parentNodeID")
	}
	if len(dispatch) != 8 {
		t.Fatalf("workflowDispatch 数量不匹配: 期望 8, 实际 %d", len(dispatch))
	}
}

// TestTracer_GetWorkflowSpan 测试获取工作流追踪跨度
func TestTracer_GetWorkflowSpan(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()
	invokeID := "invoke-get-span"

	// 触发 CallStart 以创建 workflow span
	tracer.TriggerWorkflow(ctx, TraceWFCallStart, "", &TriggerParams{
		InvokeID: invokeID,
		Metadata: map[string]any{
			"ComponentID":   "comp-1",
			"ComponentName": "TestComponent",
			"ComponentType": "Start",
		},
		Inputs:   "test",
		NeedSend: true,
	})

	// 获取已创建的 span
	span := tracer.GetWorkflowSpan(invokeID, "")
	if span == nil {
		t.Fatal("GetWorkflowSpan 应返回已创建的 span")
	}
	if span.InvokeID != invokeID {
		t.Fatalf("span.InvokeID 不匹配: 期望 %s, 实际 %s", invokeID, span.InvokeID)
	}

	// 获取不存在的 span
	span2 := tracer.GetWorkflowSpan("non-existent", "")
	if span2 != nil {
		t.Fatal("GetWorkflowSpan 对不存在的 invokeID 应返回 nil")
	}

	// 获取不存在的 parentNodeID
	span3 := tracer.GetWorkflowSpan(invokeID, "non-existent-parent")
	if span3 != nil {
		t.Fatal("GetWorkflowSpan 对不存在的 parentNodeID 应返回 nil")
	}
}

// TestTracer_PopWorkflowSpan 测试移除工作流追踪跨度
func TestTracer_PopWorkflowSpan(t *testing.T) {
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)

	tracer := NewTracer()
	tracer.Init(swm)

	ctx := context.Background()
	invokeID := "invoke-pop-span"

	// 触发 CallStart 以创建 workflow span
	tracer.TriggerWorkflow(ctx, TraceWFCallStart, "", &TriggerParams{
		InvokeID: invokeID,
		Metadata: map[string]any{
			"ComponentID":   "comp-1",
			"ComponentName": "TestComponent",
			"ComponentType": "Start",
		},
		Inputs:   "test",
		NeedSend: true,
	})

	// 验证 span 存在
	defaultSM := tracer.WorkflowSpanManagerDict[""]
	if defaultSM.GetSpan(invokeID) == nil {
		t.Fatal("Pop 前 span 应存在")
	}

	// 移除 span
	tracer.PopWorkflowSpan(invokeID, "")

	// 验证 span 已移除
	if defaultSM.GetSpan(invokeID) != nil {
		t.Fatal("Pop 后 span 不应存在")
	}

	// 移除不存在的 parentNodeID 不应 panic
	tracer.PopWorkflowSpan("any-id", "non-existent-parent")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// errTestLLM 测试用 LLM 错误
var errTestLLM = &testError{msg: "llm call failed"}

// testError 测试用错误类型
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
