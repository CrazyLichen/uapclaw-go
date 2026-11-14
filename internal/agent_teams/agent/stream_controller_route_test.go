package agent

import (
	"testing"

	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
)

// newRouteTestController 构造带 label getter / recorder 的控制器。
func newRouteTestController(label, recorded string, recorder func(string) bool) *StreamController {
	sc := NewStreamController(nil, nil, nil, nil, nil,
		WithModelGroupLabelGetter(func() string { return label }),
		WithServedModelRecorder(recorder),
	)
	sc.recordedModel = recorded
	return sc
}

// TestNameModelGroupModel_LLMRouteSelected 记 pendingRoute + 后台记账。
// Python: stream_controller.py:252-261
func TestNameModelGroupModel_LLMRouteSelected(t *testing.T) {
	calls := 0
	sc := newRouteTestController("组名", "", func(string) bool { calls++; return true })
	chunk := &streambase.OutputSchema{Type: llmRouteChunkType, Payload: map[string]any{
		"status": "selected", "model_name": "*", "routed_model": "deepseek-chat",
	}}
	got := sc.nameModelGroupModel(chunk)
	if got != chunk {
		t.Fatal("llm_route chunk 原样透传")
	}
	sc.mu.RLock()
	pending := sc.pendingRoute
	sc.mu.RUnlock()
	if pending == nil || pending["routed_model"] != "deepseek-chat" {
		t.Fatalf("pendingRoute: %v", pending)
	}
	sc.servedModelWG.Wait()
	if calls != 1 {
		t.Fatalf("记账应触发一次: %d", calls)
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.recordedModel != "deepseek-chat" {
		t.Fatalf("recordedModel: %q", sc.recordedModel)
	}
}

// TestNameModelGroupModel_PostCallRename post_call 用 pendingRoute 命名并补 deployment。
// Python: stream_controller.py:262-283
func TestNameModelGroupModel_PostCallRename(t *testing.T) {
	sc := newRouteTestController("组名", "", nil)
	sc.pendingRoute = map[string]any{"routed_model": "qwen-max", "route_id": "r1"}
	chunk := &streambase.OutputSchema{Type: contextUsageChunkType, Payload: map[string]any{
		"model": models.INTELLI_ROUTER_UNIFIED_MODEL, "phase": "post_call",
	}}
	got := sc.nameModelGroupModel(chunk)
	out := got.(*streambase.OutputSchema)
	payload := out.Payload.(map[string]any)
	if payload["model"] != "qwen-max" || payload["model_name"] != "qwen-max" {
		t.Fatalf("改名失败: %v", payload)
	}
	if payload["deployment"] != "r1" {
		t.Fatalf("route_id→deployment: %v", payload)
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.pendingRoute != nil {
		t.Fatal("post_call 后 pendingRoute 应清空")
	}
}

// TestNameModelGroupModel_NoRouteFallsToLabel 无 route 的 "*" snapshot 用组名。
// Python: stream_controller.py:281
func TestNameModelGroupModel_NoRouteFallsToLabel(t *testing.T) {
	sc := newRouteTestController("组名", "", nil)
	chunk := &streambase.OutputSchema{Type: contextUsageChunkType, Payload: map[string]any{
		"model": models.INTELLI_ROUTER_UNIFIED_MODEL, "phase": "post_call",
	}}
	out := sc.nameModelGroupModel(chunk).(*streambase.OutputSchema)
	payload := out.Payload.(map[string]any)
	if payload["model"] != "组名" {
		t.Fatalf("无 route 应用组名: %v", payload)
	}
}

// TestNameModelGroupModel_OthersPassThrough 非 llm_route/context_usage、无 label、非 "*" 透传。
func TestNameModelGroupModel_OthersPassThrough(t *testing.T) {
	sc := newRouteTestController("组名", "", nil)
	plain := &streambase.OutputSchema{Type: "llm_output", Payload: map[string]any{"content": "hi"}}
	if sc.nameModelGroupModel(plain) != plain {
		t.Fatal("普通 chunk 透传")
	}
	noLabel := NewStreamController(nil, nil, nil, nil, nil)
	usage := &streambase.OutputSchema{Type: contextUsageChunkType, Payload: map[string]any{"model": "*"}}
	if noLabel.nameModelGroupModel(usage) != usage {
		t.Fatal("无 labelGetter 透传")
	}
	nonWild := &streambase.OutputSchema{Type: contextUsageChunkType, Payload: map[string]any{"model": "gpt-4"}}
	if sc.nameModelGroupModel(nonWild) != nonWild {
		t.Fatal("非 '*' 透传")
	}
}

// TestRecordServedModelSoon_Dedup 同模型不重复记账；行不存在不更新去重标记。
// Python: stream_controller.py:285-296
func TestRecordServedModelSoon_Dedup(t *testing.T) {
	calls := 0
	sc := newRouteTestController("组名", "m1", func(string) bool { calls++; return true })
	sc.recordServedModelSoon("m1")
	sc.servedModelWG.Wait()
	if calls != 0 {
		t.Fatal("同模型应短路")
	}
	sc.recordServedModelSoon("m2")
	sc.servedModelWG.Wait()
	if calls != 1 {
		t.Fatalf("新模型应记账: %d", calls)
	}
	// recorder 返回 false → 不更新 recordedModel，下次再试
	fail := NewStreamController(nil, nil, nil, nil, nil,
		WithServedModelRecorder(func(string) bool { return false }))
	fail.recordServedModelSoon("m3")
	fail.servedModelWG.Wait()
	fail.mu.RLock()
	defer fail.mu.RUnlock()
	if fail.recordedModel == "m3" {
		t.Fatal("行不存在时不应更新 recordedModel")
	}
}

// TestWriteLLMRouteDuring 非导出 helper 在 agents 包——此处仅锚定常量一致性。
func TestChunkTypeConstants(t *testing.T) {
	if llmRouteChunkType != "llm_route" || contextUsageChunkType != "context.usage" {
		t.Fatal("chunk 类型常量与 Python 不一致")
	}
}
