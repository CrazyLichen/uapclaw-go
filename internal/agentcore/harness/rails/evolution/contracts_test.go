package evolution

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

func TestEvolutionHostEventMeta_ToPayload_仅必选字段(t *testing.T) {
	meta := EvolutionHostEventMeta{EventKind: EvolutionEventKindApproval}
	payload := meta.ToPayload()
	if len(payload) != 1 {
		t.Fatalf("payload 应有 1 个键，实际 %d", len(payload))
	}
	if payload["event_kind"] != "approval" {
		t.Errorf("event_kind = %q, want %q", payload["event_kind"], "approval")
	}
}

func TestEvolutionHostEventMeta_ToPayload_全字段(t *testing.T) {
	railKind := "skill"
	stage := "generating"
	skillName := "my_skill"
	requestID := "req_123"
	signalType := "execution_failure"
	source := "online"
	status := "completed"

	meta := EvolutionHostEventMeta{
		EventKind:  EvolutionEventKindProgress,
		RailKind:   &railKind,
		Stage:      &stage,
		SkillName:  &skillName,
		RequestID:  &requestID,
		SignalType: &signalType,
		Source:     &source,
		Status:     &status,
	}
	payload := meta.ToPayload()

	want := map[string]string{
		"event_kind":  "progress",
		"rail_kind":   "skill",
		"stage":       "generating",
		"skill_name":  "my_skill",
		"request_id":  "req_123",
		"signal_type": "execution_failure",
		"source":      "online",
		"status":      "completed",
	}
	if len(payload) != len(want) {
		t.Fatalf("payload 有 %d 个键, want %d", len(payload), len(want))
	}
	for k, v := range want {
		if payload[k] != v {
			t.Errorf("payload[%q] = %q, want %q", k, payload[k], v)
		}
	}
}

func TestEvolutionHostEventMeta_ToPayload_部分可选字段(t *testing.T) {
	railKind := "team-skill"
	skillName := "team_skill"

	meta := EvolutionHostEventMeta{
		EventKind: EvolutionEventKindOutcome,
		RailKind:  &railKind,
		SkillName: &skillName,
	}
	payload := meta.ToPayload()

	if len(payload) != 3 {
		t.Fatalf("payload 应有 3 个键，实际 %d", len(payload))
	}
	if _, ok := payload["stage"]; ok {
		t.Error("stage 不应出现在 payload 中")
	}
	if payload["rail_kind"] != "team-skill" {
		t.Errorf("rail_kind = %q, want %q", payload["rail_kind"], "team-skill")
	}
}

func TestEvolutionSnapshot_ToLegacyDict_全字段(t *testing.T) {
	skill := "my_skill"
	snap := EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{ExecutionID: "exec_1"},
		Messages:   []map[string]any{{"role": "user", "content": "hello"}},
		SkillName:  &skill,
	}
	dict := snap.ToLegacyDict()
	if dict["trajectory"] == nil {
		t.Error("trajectory 不应为 nil")
	}
	if len(dict["messages"].([]map[string]any)) != 1 {
		t.Error("messages 应有 1 条")
	}
	if dict["skill_name"] != "my_skill" {
		t.Errorf("skill_name = %v, want %q", dict["skill_name"], "my_skill")
	}
}

func TestEvolutionSnapshot_ToLegacyDict_无SkillName(t *testing.T) {
	snap := EvolutionSnapshot{
		Trajectory: &trajectory.Trajectory{ExecutionID: "exec_1"},
		Messages:   []map[string]any{},
	}
	dict := snap.ToLegacyDict()
	if _, ok := dict["skill_name"]; ok {
		t.Error("skill_name 不应出现")
	}
}

func TestEvolutionSnapshot_FromLegacyDict_全字段(t *testing.T) {
	skill := "my_skill"
	dict := map[string]any{
		"trajectory": &trajectory.Trajectory{ExecutionID: "exec_1"},
		"messages":   []map[string]any{{"role": "user"}},
		"skill_name": skill,
	}
	snap := FromLegacyDict(dict)
	if snap.Trajectory == nil || snap.Trajectory.ExecutionID != "exec_1" {
		t.Error("Trajectory 未正确恢复")
	}
	if len(snap.Messages) != 1 {
		t.Error("Messages 未正确恢复")
	}
	if snap.SkillName == nil || *snap.SkillName != "my_skill" {
		t.Error("SkillName 未正确恢复")
	}
}

func TestEvolutionSnapshot_FromLegacyDict_缺失字段(t *testing.T) {
	dict := map[string]any{
		"trajectory": &trajectory.Trajectory{},
	}
	snap := FromLegacyDict(dict)
	if len(snap.Messages) != 0 {
		t.Errorf("Messages 应为空切片，实际 %d", len(snap.Messages))
	}
	if snap.SkillName != nil {
		t.Error("SkillName 应为 nil")
	}
}

func TestEvolutionRequestResult_HasChanges_有Records(t *testing.T) {
	r := EvolutionRequestResult{Records: []checkpointing.EvolutionRecord{{ID: "ev_1"}}}
	if !r.HasChanges() {
		t.Error("有 Records 时 HasChanges 应返回 true")
	}
}

func TestEvolutionRequestResult_HasChanges_有ApprovalEvent(t *testing.T) {
	r := EvolutionRequestResult{ApprovalEvent: &stream.OutputSchema{}}
	if !r.HasChanges() {
		t.Error("有 ApprovalEvent 时 HasChanges 应返回 true")
	}
}

func TestEvolutionRequestResult_HasChanges_两者均空(t *testing.T) {
	r := EvolutionRequestResult{}
	if r.HasChanges() {
		t.Error("两者均空时 HasChanges 应返回 false")
	}
}

func TestSimplifyRequestResult_HasChanges_有Actions(t *testing.T) {
	r := SimplifyRequestResult{Actions: []map[string]any{{"action": "remove"}}}
	if !r.HasChanges() {
		t.Error("有 Actions 时 HasChanges 应返回 true")
	}
}

func TestSimplifyRequestResult_HasChanges_有ApprovalEvent(t *testing.T) {
	r := SimplifyRequestResult{ApprovalEvent: &stream.OutputSchema{}}
	if !r.HasChanges() {
		t.Error("有 ApprovalEvent 时 HasChanges 应返回 true")
	}
}

func TestSimplifyRequestResult_HasChanges_两者均空(t *testing.T) {
	r := SimplifyRequestResult{}
	if r.HasChanges() {
		t.Error("两者均空时 HasChanges 应返回 false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
