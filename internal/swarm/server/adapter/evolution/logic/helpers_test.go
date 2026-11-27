package logic

import (
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── EventPayloadDict 测试 ────────────────────────────

// TestEventPayloadDict 测试从 map 事件提取 payload。
func TestEventPayloadDict(t *testing.T) {
	evt := map[string]any{"key": "value", "stage": "generating"}
	result := EventPayloadDict(evt)
	if result["key"] != "value" || result["stage"] != "generating" {
		t.Errorf("EventPayloadDict(map) = %v, 期望包含 key=value, stage=generating", result)
	}
}

// TestEventPayloadDict_nil 测试 nil 事件。
func TestEventPayloadDict_nil(t *testing.T) {
	result := EventPayloadDict(nil)
	if len(result) != 0 {
		t.Errorf("EventPayloadDict(nil) = %v, 期望空 map", result)
	}
}

// ──────────────────────────── EventType 测试 ────────────────────────────

// TestEventType_map 测试从 map 中提取 event_type。
func TestEventType_map(t *testing.T) {
	evt := map[string]any{"event_type": "chat.ask_user_question"}
	result := EventType(evt)
	if result != "chat.ask_user_question" {
		t.Errorf("EventType() = %q, 期望 %q", result, "chat.ask_user_question")
	}
}

// TestEventType_empty 测试空事件类型。
func TestEventType_empty(t *testing.T) {
	evt := map[string]any{"other_key": "value"}
	result := EventType(evt)
	if result != "" {
		t.Errorf("EventType(empty) = %q, 期望空字符串", result)
	}
}

// TestEventType_nil 测试 nil 事件返回空字符串。
func TestEventType_nil(t *testing.T) {
	result := EventType(nil)
	if result != "" {
		t.Errorf("EventType(nil) = %q, 期望空字符串", result)
	}
}

// ──────────────────────────── ResolveEvolutionEventTimeoutSec 测试 ────────────────────────────

// TestResolveEvolutionEventTimeoutSec_nilRail 测试 nil rail 使用默认值。
func TestResolveEvolutionEventTimeoutSec_nilRail(t *testing.T) {
	result := ResolveEvolutionEventTimeoutSec(nil)
	expected := TeamEvolutionEventTimeoutSec // nil rail 直接返回 fallback
	if result != expected {
		t.Errorf("ResolveEvolutionEventTimeoutSec(nil) = %f, 期望 %f", result, expected)
	}
}

// TestResolveEvolutionEventTimeoutSec_mapRail 测试从 map 读取超时。
func TestResolveEvolutionEventTimeoutSec_mapRail(t *testing.T) {
	rail := map[string]any{"evolution_total_timeout_secs": float64(100)}
	result := ResolveEvolutionEventTimeoutSec(rail)
	expected := 100.0 + TeamEvolutionEventTimeoutGraceSec
	if result != expected {
		t.Errorf("ResolveEvolutionEventTimeoutSec(map) = %f, 期望 %f", result, expected)
	}
}

// TestResolveEvolutionEventTimeoutSec_customFallback 测试自定义 fallback。
func TestResolveEvolutionEventTimeoutSec_customFallback(t *testing.T) {
	result := ResolveEvolutionEventTimeoutSec(nil, 200.0, 10.0)
	if result != 200.0 {
		t.Errorf("ResolveEvolutionEventTimeoutSec(custom) = %f, 期望 200.0", result)
	}
}

// TestResolveEvolutionEventTimeoutSec_invalidTimeout 测试无效超时值回退。
func TestResolveEvolutionEventTimeoutSec_invalidTimeout(t *testing.T) {
	rail := map[string]any{"evolution_total_timeout_secs": "invalid"}
	result := ResolveEvolutionEventTimeoutSec(rail)
	expected := TeamEvolutionEventTimeoutSec // 无效值返回 fallback
	if result != expected {
		t.Errorf("ResolveEvolutionEventTimeoutSec(invalid) = %f, 期望 %f", result, expected)
	}
}

// TestResolveEvolutionEventTimeoutSec_emptyMap 测试空 map rail 使用默认值。
func TestResolveEvolutionEventTimeoutSec_emptyMap(t *testing.T) {
	rail := map[string]any{}
	result := ResolveEvolutionEventTimeoutSec(rail)
	expected := TeamEvolutionEventTimeoutSec
	if result != expected {
		t.Errorf("ResolveEvolutionEventTimeoutSec(emptyMap) = %f, 期望 %f", result, expected)
	}
}

// TestResolveEvolutionEventTimeoutSec_fallbackZero 测试 fallback=0.0 不被忽略。
// Python: fallback_sec is not None → 使用传入值（含 0.0）
func TestResolveEvolutionEventTimeoutSec_fallbackZero(t *testing.T) {
	result := ResolveEvolutionEventTimeoutSec(nil, 0.0)
	if result != 0.0 {
		t.Errorf("ResolveEvolutionEventTimeoutSec(fallback=0) = %f, 期望 0.0", result)
	}
}

// ──────────────────────────── IsEvolutionApprovalEvent 测试 ────────────────────────────

// TestIsEvolutionApprovalEvent_true 测试审批事件返回 true。
func TestIsEvolutionApprovalEvent_true(t *testing.T) {
	evt := map[string]any{"event_type": "chat.ask_user_question"}
	if !IsEvolutionApprovalEvent(evt) {
		t.Error("IsEvolutionApprovalEvent(chat.ask_user_question) 应为 true")
	}
}

// TestIsEvolutionApprovalEvent_false 测试非审批事件返回 false。
func TestIsEvolutionApprovalEvent_false(t *testing.T) {
	evt := map[string]any{"event_type": "chat.answer"}
	if IsEvolutionApprovalEvent(evt) {
		t.Error("IsEvolutionApprovalEvent(chat.answer) 应为 false")
	}
}

// ──────────────────────────── EvolutionEventKind 测试 ────────────────────────────

// TestEvolutionEventKind_approval 测试审批事件类别。
func TestEvolutionEventKind_approval(t *testing.T) {
	evt := map[string]any{"event_type": "chat.ask_user_question"}
	result := EvolutionEventKind(evt)
	if result != "approval" {
		t.Errorf("EvolutionEventKind(approval) = %q, 期望 %q", result, "approval")
	}
}

// TestEvolutionEventKind_outcome 测试结果事件类别。
func TestEvolutionEventKind_outcome(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "outcome"},
	}
	result := EvolutionEventKind(evt)
	if result != "outcome" {
		t.Errorf("EvolutionEventKind(outcome) = %q, 期望 %q", result, "outcome")
	}
}

// TestEvolutionEventKind_progress 测试进度事件类别。
func TestEvolutionEventKind_progress(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "progress"},
	}
	result := EvolutionEventKind(evt)
	if result != "progress" {
		t.Errorf("EvolutionEventKind(progress) = %q, 期望 %q", result, "progress")
	}
}

// TestEvolutionEventKind_stream 测试流式事件类别（默认）。
func TestEvolutionEventKind_stream(t *testing.T) {
	evt := map[string]any{"event_type": "chat.answer"}
	result := EvolutionEventKind(evt)
	if result != "stream" {
		t.Errorf("EvolutionEventKind(stream) = %q, 期望 %q", result, "stream")
	}
}

// ──────────────────────────── IsEvolutionOutcomeEvent 测试 ────────────────────────────

// TestIsEvolutionOutcomeEvent_true 测试结果事件返回 true。
func TestIsEvolutionOutcomeEvent_true(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "outcome"},
	}
	if !IsEvolutionOutcomeEvent(evt) {
		t.Error("IsEvolutionOutcomeEvent(outcome) 应为 true")
	}
}

// TestIsEvolutionOutcomeEvent_false 测试非结果事件返回 false。
func TestIsEvolutionOutcomeEvent_false(t *testing.T) {
	evt := map[string]any{"event_type": "chat.answer"}
	if IsEvolutionOutcomeEvent(evt) {
		t.Error("IsEvolutionOutcomeEvent(stream) 应为 false")
	}
}

// ──────────────────────────── EvolutionOutcomeFromEvent 测试 ────────────────────────────

// TestEvolutionOutcomeFromEvent_complete 测试提取完整结果。
func TestEvolutionOutcomeFromEvent_complete(t *testing.T) {
	evt := map[string]any{
		"status":  "completed",
		"message": "Skill updated successfully",
	}
	result := EvolutionOutcomeFromEvent(evt)
	if result["status"] != "completed" || result["message"] != "Skill updated successfully" {
		t.Errorf("EvolutionOutcomeFromEvent() = %v, 期望 status=completed, message=Skill updated successfully", result)
	}
}

// TestEvolutionOutcomeFromEvent_metaStatus 测试从 meta 中提取状态。
func TestEvolutionOutcomeFromEvent_metaStatus(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"status": "failed"},
		"message":         "Error occurred",
	}
	result := EvolutionOutcomeFromEvent(evt)
	if result["status"] != "failed" {
		t.Errorf("EvolutionOutcomeFromEvent(meta) = %v, 期望 status=failed", result)
	}
}

// TestEvolutionOutcomeFromEvent_default 测试默认值。
func TestEvolutionOutcomeFromEvent_default(t *testing.T) {
	evt := map[string]any{}
	result := EvolutionOutcomeFromEvent(evt)
	if result["status"] != "completed" {
		t.Errorf("EvolutionOutcomeFromEvent(default) = %v, 期望 status=completed", result)
	}
}

// TestEvolutionOutcomeFromEvent_nil 测试 nil 事件返回默认值。
func TestEvolutionOutcomeFromEvent_nil(t *testing.T) {
	result := EvolutionOutcomeFromEvent(nil)
	if result["status"] != "completed" {
		t.Errorf("EvolutionOutcomeFromEvent(nil) = %v, 期望 status=completed", result)
	}
}

// ──────────────────────────── ExtractEvolutionRequestID 测试 ────────────────────────────

// TestExtractEvolutionRequestID_payload 测试从 payload 提取。
func TestExtractEvolutionRequestID_payload(t *testing.T) {
	evt := map[string]any{"request_id": "skill_evolve_123"}
	result := ExtractEvolutionRequestID(evt)
	if result == nil || *result != "skill_evolve_123" {
		t.Errorf("ExtractEvolutionRequestID(payload) = %v, 期望 skill_evolve_123", result)
	}
}

// TestExtractEvolutionRequestID_meta 测试从 meta 提取。
func TestExtractEvolutionRequestID_meta(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"request_id": "team_evolve_456"},
	}
	result := ExtractEvolutionRequestID(evt)
	if result == nil || *result != "team_evolve_456" {
		t.Errorf("ExtractEvolutionRequestID(meta) = %v, 期望 team_evolve_456", result)
	}
}

// TestExtractEvolutionRequestID_nil 测试无 request_id 返回 nil。
func TestExtractEvolutionRequestID_nil(t *testing.T) {
	evt := map[string]any{}
	result := ExtractEvolutionRequestID(evt)
	if result != nil {
		t.Errorf("ExtractEvolutionRequestID(nil) = %v, 期望 nil", result)
	}
}

// TestExtractEvolutionRequestID_whitespace 测试空白字符串返回 nil。
func TestExtractEvolutionRequestID_whitespace(t *testing.T) {
	evt := map[string]any{"request_id": "  "}
	result := ExtractEvolutionRequestID(evt)
	if result != nil {
		t.Errorf("ExtractEvolutionRequestID(whitespace) = %v, 期望 nil", result)
	}
}

// ──────────────────────────── EvolutionProgressStatusFromEvent 测试 ────────────────────────────

// TestEvolutionProgressStatusFromEvent_progress 测试提取进度状态。
func TestEvolutionProgressStatusFromEvent_progress(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "progress"},
		"stage":           "generating_updates",
		"message":         "Generating evolution updates",
		"request_id":      "skill_evolve_789",
	}
	result := EvolutionProgressStatusFromEvent(evt)
	if result == nil {
		t.Fatal("EvolutionProgressStatusFromEvent() 返回 nil")
	}
	if result.Stage != "generating" {
		t.Errorf("Stage = %q, 期望 %q", result.Stage, "generating")
	}
	if result.Message != "Generating evolution updates" {
		t.Errorf("Message = %q, 期望 %q", result.Message, "Generating evolution updates")
	}
	if result.RequestID == nil || *result.RequestID != "skill_evolve_789" {
		t.Errorf("RequestID = %v, 期望 skill_evolve_789", result.RequestID)
	}
}

// TestEvolutionProgressStatusFromEvent_noop 测试 noop 检测。
func TestEvolutionProgressStatusFromEvent_noop(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "progress"},
		"stage":           "completed",
		"message":         "No existing skill found for this capability",
	}
	result := EvolutionProgressStatusFromEvent(evt)
	if result == nil {
		t.Fatal("EvolutionProgressStatusFromEvent() 返回 nil")
	}
	if result.Stage != TeamEvolutionNoopNoSkillStage {
		t.Errorf("Stage = %q, 期望 %q", result.Stage, TeamEvolutionNoopNoSkillStage)
	}
}

// TestEvolutionProgressStatusFromEvent_nonProgress 测试非 progress 事件返回 nil。
func TestEvolutionProgressStatusFromEvent_nonProgress(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "outcome"},
	}
	result := EvolutionProgressStatusFromEvent(evt)
	if result != nil {
		t.Errorf("EvolutionProgressStatusFromEvent(outcome) = %v, 期望 nil", result)
	}
}

// TestEvolutionProgressStatusFromEvent_noMeta 测试无 meta 返回 nil。
func TestEvolutionProgressStatusFromEvent_noMeta(t *testing.T) {
	evt := map[string]any{"stage": "generating"}
	result := EvolutionProgressStatusFromEvent(evt)
	if result != nil {
		t.Errorf("EvolutionProgressStatusFromEvent(noMeta) = %v, 期望 nil", result)
	}
}

// TestEvolutionProgressStatusFromEvent_terminal 测试终结阶段。
func TestEvolutionProgressStatusFromEvent_terminal(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "progress"},
		"stage":           "completed",
		"message":         "Evolution completed",
	}
	result := EvolutionProgressStatusFromEvent(evt)
	if result == nil {
		t.Fatal("EvolutionProgressStatusFromEvent() 返回 nil")
	}
	if !result.Terminal {
		t.Error("Terminal 应为 true")
	}
}

// ──────────────────────────── VisibleEvolutionProgressFromEvents 测试 ────────────────────────────

// TestVisibleEvolutionProgressFromEvents 测试过滤可见进度。
func TestVisibleEvolutionProgressFromEvents(t *testing.T) {
	events := []map[string]any{
		{
			"_evolution_meta": map[string]any{"event_kind": "progress"},
			"stage":           "generating_updates",
			"message":         "Generating",
		},
		{
			"_evolution_meta": map[string]any{"event_kind": "progress"},
			"stage":           "cancelled",
			"message":         "Cancelled",
		},
	}
	result := VisibleEvolutionProgressFromEvents(events)
	if len(result) != 1 {
		t.Fatalf("VisibleEvolutionProgressFromEvents() 返回 %d 项, 期望 1", len(result))
	}
	if result[0].Stage != "generating" {
		t.Errorf("Stage = %q, 期望 %q", result[0].Stage, "generating")
	}
}

// ──────────────────────────── ProgressForRequest 测试 ────────────────────────────

// TestProgressForRequest 测试按 requestID 过滤。
func TestProgressForRequest(t *testing.T) {
	rid1 := "req-1"
	rid2 := "req-2"
	statuses := []EvolutionProgressStatus{
		{Stage: "detecting", RequestID: &rid1},
		{Stage: "generating", RequestID: &rid2},
		{Stage: "collecting", RequestID: nil}, // nil 匹配所有
	}
	result := ProgressForRequest(statuses, "req-1")
	if len(result) != 2 {
		t.Errorf("ProgressForRequest() = %d 项, 期望 2", len(result))
	}
}

// ──────────────────────────── TerminalStage 测试 ────────────────────────────

// TestTerminalStage 测试提取终结阶段。
func TestTerminalStage(t *testing.T) {
	terminal := map[string]string{"stage": "completed", "status": "end"}
	result := TerminalStage(terminal)
	if result != "completed" {
		t.Errorf("TerminalStage() = %q, 期望 %q", result, "completed")
	}
}

// TestTerminalStage_fallback 测试 status 作为后备。
func TestTerminalStage_fallback(t *testing.T) {
	terminal := map[string]string{"status": "failed"}
	result := TerminalStage(terminal)
	if result != "failed" {
		t.Errorf("TerminalStage(fallback) = %q, 期望 %q", result, "failed")
	}
}

// ──────────────────────────── TeamEvolutionTerminalProgress 测试 ────────────────────────────

// TestTeamEvolutionTerminalProgress_hidden 测试隐藏终结阶段。
func TestTeamEvolutionTerminalProgress_hidden(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "progress"},
		"stage":           "cancelled",
		"message":         "Cancelled by user",
	}
	result := TeamEvolutionTerminalProgress(evt)
	if result == nil {
		t.Fatal("TeamEvolutionTerminalProgress() 返回 nil")
	}
	if result["stage"] != TeamEvolutionHiddenStage {
		t.Errorf("stage = %q, 期望 %q", result["stage"], TeamEvolutionHiddenStage)
	}
}

// TestTeamEvolutionTerminalProgress_noop 测试 noop 终结。
func TestTeamEvolutionTerminalProgress_noop(t *testing.T) {
	evt := map[string]any{
		"message": "No existing skill found for this capability",
		"status":  "end",
		"stage":   "completed",
	}
	result := TeamEvolutionTerminalProgress(evt)
	if result == nil {
		t.Fatal("TeamEvolutionTerminalProgress() 返回 nil")
	}
	if result["status"] != "completed" {
		t.Errorf("status = %q, 期望 completed", result["status"])
	}
	if result["stage"] != TeamEvolutionNoopNoSkillStage {
		t.Errorf("stage = %q, 期望 %q", result["stage"], TeamEvolutionNoopNoSkillStage)
	}
}

// TestTeamEvolutionTerminalProgress_normalTerminal 测试正常终结。
func TestTeamEvolutionTerminalProgress_normalTerminal(t *testing.T) {
	evt := map[string]any{
		"_evolution_meta": map[string]any{"event_kind": "progress"},
		"stage":           "completed",
		"message":         "Evolution completed successfully",
	}
	result := TeamEvolutionTerminalProgress(evt)
	if result == nil {
		t.Fatal("TeamEvolutionTerminalProgress() 返回 nil")
	}
	if result["stage"] != "completed" {
		t.Errorf("stage = %q, 期望 completed", result["stage"])
	}
}

// TestTeamEvolutionTerminalProgress_notTerminal 测试非终结返回 nil。
func TestTeamEvolutionTerminalProgress_notTerminal(t *testing.T) {
	evt := map[string]any{
		"stage":   "generating",
		"message": "Still working",
	}
	result := TeamEvolutionTerminalProgress(evt)
	if result != nil {
		t.Errorf("TeamEvolutionTerminalProgress(notTerminal) = %v, 期望 nil", result)
	}
}

// TestTeamEvolutionTerminalProgress_endStatus 测试 end 状态。
func TestTeamEvolutionTerminalProgress_endStatus(t *testing.T) {
	evt := map[string]any{
		"status":  "end",
		"stage":   "completed",
		"message": "All done",
	}
	result := TeamEvolutionTerminalProgress(evt)
	if result == nil {
		t.Fatal("TeamEvolutionTerminalProgress() 返回 nil")
	}
	if result["status"] != "end" {
		t.Errorf("status = %q, 期望 end", result["status"])
	}
}

// ──────────────────────────── TerminalProgressFromEvents 测试 ────────────────────────────

// TestTerminalProgressFromEvents 测试从事件列表提取终结进度。
func TestTerminalProgressFromEvents(t *testing.T) {
	events := []map[string]any{
		{
			"_evolution_meta": map[string]any{"event_kind": "progress"},
			"stage":           "completed",
			"message":         "Done",
			"request_id":      "req-1",
		},
		{
			"stage":   "generating",
			"message": "Working",
		},
	}
	result := TerminalProgressFromEvents(events)
	if len(result) != 1 {
		t.Fatalf("TerminalProgressFromEvents() = %d 项, 期望 1", len(result))
	}
	if result[0].Terminal["stage"] != "completed" {
		t.Errorf("stage = %q, 期望 completed", result[0].Terminal["stage"])
	}
}

// ──────────────────────────── BuildEvolutionStatusUpdate 测试 ────────────────────────────

// TestBuildEvolutionStatusUpdate 测试构建状态更新。
func TestBuildEvolutionStatusUpdate(t *testing.T) {
	update := BuildEvolutionStatusUpdate("req-1", "running", "generating", "Updating skills")
	if update.RequestID != "req-1" || update.Status != "running" || update.Stage != "generating" || update.Message != "Updating skills" {
		t.Errorf("BuildEvolutionStatusUpdate() = %+v, 期望所有字段正确", update)
	}
}

// TestBuildEvolutionStatusUpdate_noMessage 测试无消息参数。
func TestBuildEvolutionStatusUpdate_noMessage(t *testing.T) {
	update := BuildEvolutionStatusUpdate("req-1", "running", "generating")
	if update.Message != "" {
		t.Errorf("Message = %q, 期望空字符串", update.Message)
	}
}

// ──────────────────────────── TeamEvolutionEndUpdate 测试 ────────────────────────────

// TestTeamEvolutionEndUpdate_nil 测试 nil terminal。
func TestTeamEvolutionEndUpdate_nil(t *testing.T) {
	update := TeamEvolutionEndUpdate("req-1", nil)
	if update.Status != "end" || update.Stage != "completed" {
		t.Errorf("TeamEvolutionEndUpdate(nil) = %+v, 期望 status=end, stage=completed", update)
	}
}

// TestTeamEvolutionEndUpdate_failed 测试 failed 终结。
func TestTeamEvolutionEndUpdate_failed(t *testing.T) {
	terminal := map[string]string{"stage": "failed", "status": "failed", "message": "Error"}
	update := TeamEvolutionEndUpdate("req-1", terminal)
	if update.Stage != TeamEvolutionHiddenStage {
		t.Errorf("TeamEvolutionEndUpdate(failed) stage = %q, 期望 %q", update.Stage, TeamEvolutionHiddenStage)
	}
}

// TestTeamEvolutionEndUpdate_noop 测试 noop 终结。
func TestTeamEvolutionEndUpdate_noop(t *testing.T) {
	terminal := map[string]string{"stage": TeamEvolutionNoopNoSkillStage, "message": "No skill"}
	update := TeamEvolutionEndUpdate("req-1", terminal)
	if update.Stage != TeamEvolutionNoopNoSkillStage {
		t.Errorf("TeamEvolutionEndUpdate(noop) stage = %q, 期望 %q", update.Stage, TeamEvolutionNoopNoSkillStage)
	}
}

// TestTeamEvolutionEndUpdate_normal 测试正常终结。
func TestTeamEvolutionEndUpdate_normal(t *testing.T) {
	terminal := map[string]string{"stage": "completed", "message": "Done"}
	update := TeamEvolutionEndUpdate("req-1", terminal)
	if update.Stage != "completed" {
		t.Errorf("TeamEvolutionEndUpdate(normal) stage = %q, 期望 completed", update.Stage)
	}
}

// ──────────────────────────── GroupEvolutionApprovals 测试 ────────────────────────────

// TestGroupEvolutionApprovals_basic 测试基本分组。
func TestGroupEvolutionApprovals_basic(t *testing.T) {
	events := []map[string]any{
		{"event_type": "chat.ask_user_question", "request_id": "skill_evolve_1"},
		{"event_type": "chat.ask_user_question", "request_id": "skill_evolve_1"},
		{"event_type": "chat.ask_user_question", "request_id": "skill_evolve_2"},
		{"event_type": "chat.answer"},
	}
	grouped, missing := GroupEvolutionApprovals("sess-1", events)
	if len(grouped) != 2 {
		t.Errorf("GroupEvolutionApprovals() grouped = %d, 期望 2", len(grouped))
	}
	if len(grouped["skill_evolve_1"]) != 2 {
		t.Errorf("skill_evolve_1 事件数 = %d, 期望 2", len(grouped["skill_evolve_1"]))
	}
	if len(missing) != 0 {
		t.Errorf("missing = %d, 期望 0", len(missing))
	}
}

// TestGroupEvolutionApprovals_missingRequestID 测试缺失 requestID。
func TestGroupEvolutionApprovals_missingRequestID(t *testing.T) {
	warnCalled := false
	warnFn := func(sessionID string) { warnCalled = true }

	events := []map[string]any{
		{"event_type": "chat.ask_user_question"},
	}
	grouped, missing := GroupEvolutionApprovals("sess-1", events, WarnMissingRequestIDFunc(warnFn))
	if len(grouped) != 0 {
		t.Errorf("GroupEvolutionApprovals() grouped = %d, 期望 0", len(grouped))
	}
	// Python: 第二项始终为 nil/空列表
	if len(missing) != 0 {
		t.Errorf("missing = %d, 期望 0", len(missing))
	}
	if !warnCalled {
		t.Error("warnMissingRequestID 回调未被调用")
	}
}

// ──────────────────────────── MakeTeamEvolutionCycleRequestID 测试 ────────────────────────────

// TestMakeTeamEvolutionCycleRequestID 测试生成 request_id。
func TestMakeTeamEvolutionCycleRequestID(t *testing.T) {
	result := MakeTeamEvolutionCycleRequestID("sess-1", 3)
	expected := "team_evolve_sess-1_3"
	if result != expected {
		t.Errorf("MakeTeamEvolutionCycleRequestID() = %q, 期望 %q", result, expected)
	}
}

// ──────────────────────────── noopStageFromMessage 测试 ────────────────────────────

// TestNoopStageFromMessage_noSkill 测试无技能 noop。
func TestNoopStageFromMessage_noSkill(t *testing.T) {
	result := noopStageFromMessage(strings.ToLower("No existing skill found"))
	if result == nil || *result != TeamEvolutionNoopNoSkillStage {
		t.Errorf("noopStageFromMessage(noSkill) = %v, 期望 %q", result, TeamEvolutionNoopNoSkillStage)
	}
}

// TestNoopStageFromMessage_noSignal 测试无信号 noop。
func TestNoopStageFromMessage_noSignal(t *testing.T) {
	result := noopStageFromMessage(strings.ToLower("No evolution signals detected"))
	if result == nil || *result != TeamEvolutionNoopNoSignalStage {
		t.Errorf("noopStageFromMessage(noSignal) = %v, 期望 %q", result, TeamEvolutionNoopNoSignalStage)
	}
}

// TestNoopStageFromMessage_noRecords 测试无记录 noop。
func TestNoopStageFromMessage_noRecords(t *testing.T) {
	result := noopStageFromMessage(strings.ToLower("No evolution records generated"))
	if result == nil || *result != TeamEvolutionNoopNoRecordsStage {
		t.Errorf("noopStageFromMessage(noRecords) = %v, 期望 %q", result, TeamEvolutionNoopNoRecordsStage)
	}
}

// TestNoopStageFromMessage_generic 测试通用 noop。
func TestNoopStageFromMessage_generic(t *testing.T) {
	result := noopStageFromMessage(strings.ToLower("No existing skill found for this"))
	if result == nil || *result != TeamEvolutionNoopNoSkillStage {
		t.Errorf("noopStageFromMessage(generic via noSkill marker) = %v, 期望 %q", result, TeamEvolutionNoopNoSkillStage)
	}
}

// TestNoopStageFromMessage_none 测试非 noop 消息返回 nil。
func TestNoopStageFromMessage_none(t *testing.T) {
	result := noopStageFromMessage(strings.ToLower("Evolution completed successfully"))
	if result != nil {
		t.Errorf("noopStageFromMessage(none) = %v, 期望 nil", result)
	}
}

// ──────────────────────────── 15.24d Task2：状态映射 ────────────────────────────

// TestEvolutionStatusResponse 四状态映射与透传。
// Python: evolution_status_response()（evolution_helpers.py:274-309）。
func TestEvolutionStatusResponse(t *testing.T) {
	t.Run("空status返回nil", func(t *testing.T) {
		if got := EvolutionStatusResponse(map[string]any{"status": ""}, "gen-fail", "no-records"); got != nil {
			t.Fatalf("空 status 应返回 nil，实际 %v", got)
		}
	})
	t.Run("generation_failed_内部错误归一", func(t *testing.T) {
		got := EvolutionStatusResponse(map[string]any{"status": "generation_failed", "message": "toolchain boom"}, "gen-fail", "no-records")
		if got == nil || got["result_type"] != "error" || got["output"] != "LLM 服务调用失败，请检查模型配置或稍后重试" {
			t.Fatalf("内部错误链应归一文案，实际 %v", got)
		}
	})
	t.Run("generation_failed_普通错误透传", func(t *testing.T) {
		got := EvolutionStatusResponse(map[string]any{"status": "generation_failed", "message": "model down"}, "gen-fail", "no-records")
		if got == nil || got["output"] != "model down" || got["result_type"] != "error" {
			t.Fatalf("普通 message 应透传，实际 %v", got)
		}
	})
	t.Run("skipped_skill_definition_not_found", func(t *testing.T) {
		got := EvolutionStatusResponse(map[string]any{"status": "skipped_skill_definition_not_found", "message": "extra"}, "gen-fail", "no-records")
		if got == nil || got["result_type"] != "error" || got["output"] != "Skill 缺少 SKILL.md，无法执行演进生成。\nextra" {
			t.Fatalf("实际 %v", got)
		}
	})
	t.Run("persistence_failed", func(t *testing.T) {
		got := EvolutionStatusResponse(map[string]any{"status": "persistence_failed", "message": "disk full"}, "gen-fail", "no-records")
		if got == nil || got["result_type"] != "error" || got["output"] != "演进经验保存失败：disk full" {
			t.Fatalf("实际 %v", got)
		}
	})
	t.Run("no_evolution_no_records", func(t *testing.T) {
		got := EvolutionStatusResponse(map[string]any{"status": "no_evolution_no_records", "message": "empty"}, "gen-fail", "no-records")
		if got == nil || got["result_type"] != "answer" || got["output"] != "no-records\nempty" {
			t.Fatalf("实际 %v", got)
		}
	})
	t.Run("未知status返回nil", func(t *testing.T) {
		if got := EvolutionStatusResponse(map[string]any{"status": "completed"}, "gen-fail", "no-records"); got != nil {
			t.Fatalf("未知 status 应返回 nil，实际 %v", got)
		}
	})
}

// TestUserFacingGenerationError 内部工具链错误链归一。
// Python: _user_facing_generation_error()（evolution_helpers.py:312-324）。
func TestUserFacingGenerationError(t *testing.T) {
	for _, marker := range []string{"toolchain", "tool_call", "invoke_failed", "execution error", "optimizer"} {
		if got := UserFacingGenerationError("failed: " + marker + " x"); got != "LLM 服务调用失败，请检查模型配置或稍后重试" {
			t.Fatalf("marker %q 应归一，实际 %q", marker, got)
		}
	}
	if got := UserFacingGenerationError("model quota exceeded"); got != "model quota exceeded" {
		t.Fatalf("普通错误应透传，实际 %q", got)
	}
}

// ──────────────────────────── 15.24d Task3：slash 结果前端注记 ────────────────────────────

// TestEvolutionSlashDisplayLevel 显示级别判定。
// Python: evolution_slash_display_level()（evolution_helpers.py:327-338）。
func TestEvolutionSlashDisplayLevel(t *testing.T) {
	if got := EvolutionSlashDisplayLevel("error", "x", RegularEvolutionSlashWarningPhrases); got != "error" {
		t.Fatalf("error → error，实际 %q", got)
	}
	if got := EvolutionSlashDisplayLevel("answer", "本次未生成可保存经验", RegularEvolutionSlashWarningPhrases); got != "warning" {
		t.Fatalf("warning 短语 → warning，实际 %q", got)
	}
	if got := EvolutionSlashDisplayLevel("answer", "已完成", RegularEvolutionSlashWarningPhrases); got != "info" {
		t.Fatalf("普通 → info，实际 %q", got)
	}
}

// TestEvolutionSlashCommandName 命令名提取。
// Python: evolution_slash_command_name()（evolution_helpers.py:341-347）。
func TestEvolutionSlashCommandName(t *testing.T) {
	for q, want := range map[string]string{
		"/evolve_list x":   "evolve_list",
		"/evolve_simplify": "evolve_simplify",
		"/evolve skill":    "evolve",
		"/evolve":          "evolve",
		"/other":           "evolve",
	} {
		if got := EvolutionSlashCommandName(q); got != want {
			t.Fatalf("%q → %q, want %q", q, got, want)
		}
	}
}

// TestEvolutionSlashResult 注记不覆盖已有键。
// Python: evolution_slash_result()（evolution_helpers.py:350-368）。
func TestEvolutionSlashResult(t *testing.T) {
	result := EvolutionSlashResult("evolve", map[string]any{"output": "ok", "result_type": "answer", "display_level": "custom"}, nil)
	if result["source"] != "slash_command" || result["slash_command"] != "evolve" || result["display_level"] != "custom" {
		t.Fatalf("setdefault 不得覆盖已有 display_level，实际 %v", result)
	}
	result2 := EvolutionSlashResult("evolve_list", map[string]any{"output": "未生成可保存经验", "result_type": "answer"}, nil)
	if result2["display_level"] != "warning" {
		t.Fatalf("缺省 display_level 应按短语判定，实际 %v", result2["display_level"])
	}
}

// TestApprovedRecordIDsFromAnswers 索引→record_id 映射。
// Python: approved_record_ids_from_answers()（evolution_helpers.py:419-451）。
func TestApprovedRecordIDsFromAnswers(t *testing.T) {
	ids := []string{"r1", "r2", "r3"}
	labels := EvolutionAcceptLabels

	// 全选接受 → 全部 record_id
	accepted, got, hasMapping := ApprovedRecordIDsFromAnswers(
		[][]string{{"接受"}, {"accept"}, {"接受"}}, labels, ids)
	if !accepted || !hasMapping || len(got) != 3 {
		t.Fatalf("全选接受应映射全部，实际 accepted=%v ids=%v", accepted, got)
	}

	// 部分接受（第 2 条拒绝）→ 仅 r1/r3
	accepted, got, _ = ApprovedRecordIDsFromAnswers(
		[][]string{{"接受"}, {"拒绝"}, {"本次允许"}}, labels, ids)
	if !accepted || len(got) != 2 || got[0] != "r1" || got[1] != "r3" {
		t.Fatalf("部分批准应得 [r1 r3]，实际 %v", got)
	}

	// 全拒绝 → accepted=false
	accepted, _, _ = ApprovedRecordIDsFromAnswers(
		[][]string{{"拒绝"}, {"拒绝"}}, labels, ids)
	if accepted {
		t.Fatal("全拒绝应 accepted=false")
	}

	// 无映射（recordIDsByIndex=nil）→ hasMapping=false，保持 all-or-nothing
	var got2 []string
	accepted, got2, hasMapping = ApprovedRecordIDsFromAnswers(
		[][]string{{"接受"}}, labels, nil)
	got = got2
	if !accepted || hasMapping || got != nil && len(got) != 0 {
		t.Fatalf("无映射应 hasMapping=false，实际 %v/%v", got, hasMapping)
	}

	// 空白与大小写归一
	accepted, _, _ = ApprovedRecordIDsFromAnswers(
		[][]string{{"  Accept  "}}, labels, ids)
	if !accepted {
		t.Fatal("标签归一后应命中接受")
	}
}
