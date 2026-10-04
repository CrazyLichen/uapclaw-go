//go:build integration

package evolution

import (
	"testing"

	"github.com/stretchr/testify/suite"

	evolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ApprovalEventsSuite 测试演化审批事件构建函数。
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_evolution_approval_events.py
//   - TestBuildProgressEvent → test_build_progress_event_matches_reasoning_payload
//   - TestBuildEvolutionProgressEvent_* → test_build_evolution_progress_event_includes_normalized_meta
//   - TestBuildSkillApprovalEvent_* → test_build_skill_approval_event_*
//   - TestBuildSimplifyApprovalEvent_* → test_build_simplify_approval_event_*
//   - TestBuildTeamSkillApprovalEventFromRecords → test_build_team_skill_approval_event_from_records_*
//   - TestAttachEvolutionMeta → 附加演化元数据
//   - TestIsEn → 语言判断间接测试
type ApprovalEventsSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestApprovalEventsSuite 运行审批事件集成测试套件。
func TestApprovalEventsSuite(t *testing.T) {
	suite.Run(t, new(ApprovalEventsSuite))
}

// TestBuildProgressEvent 验证 BuildProgressEvent 返回正确的 type/payload。
//
// 对齐 Python: test_build_progress_event_matches_reasoning_payload
func (s *ApprovalEventsSuite) TestBuildProgressEvent() {
	event := evolution.BuildProgressEvent("[Team Skill Evolution]", "analysis started")

	s.Equal("llm_reasoning", event.Type)
	s.Equal(0, event.Index)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	s.Equal("[Team Skill Evolution] analysis started\n", payload["content"])
}

// TestBuildEvolutionProgressEvent_中文 验证中文进度事件的 _evolution_meta 包含 event_kind/rail_kind/stage。
//
// 对齐 Python: test_build_evolution_progress_event_includes_normalized_meta（中文路径）
func (s *ApprovalEventsSuite) TestBuildEvolutionProgressEvent_中文() {
	event := evolution.BuildEvolutionProgressEvent(
		"regular", "approval_required", "等待审批",
		evolution.WithSkillName("skill-a"),
		evolution.WithRequestID("req-1"),
		evolution.WithPrefix("[技能演进]"),
	)

	s.Equal("llm_reasoning", event.Type)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	s.Equal("[技能演进] 等待审批\n", payload["content"])

	meta, ok := payload["_evolution_meta"].(map[string]string)
	s.Require().True(ok)
	s.Equal("progress", meta["event_kind"])
	s.Equal("regular", meta["rail_kind"])
	s.Equal("approval_required", meta["stage"])
	s.Equal("skill-a", meta["skill_name"])
	s.Equal("req-1", meta["request_id"])
}

// TestBuildEvolutionProgressEvent_英文 验证英文进度事件。
//
// 对齐 Python: test_build_evolution_progress_event_includes_normalized_meta（英文路径）
func (s *ApprovalEventsSuite) TestBuildEvolutionProgressEvent_英文() {
	event := evolution.BuildEvolutionProgressEvent(
		"regular", "approval_required", "awaiting approval",
		evolution.WithSkillName("skill-a"),
		evolution.WithRequestID("req-1"),
		evolution.WithPrefix("[Skill Evolution]"),
	)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	s.Equal("[Skill Evolution] awaiting approval\n", payload["content"])

	meta, ok := payload["_evolution_meta"].(map[string]string)
	s.Require().True(ok)
	s.Equal("progress", meta["event_kind"])
	s.Equal("regular", meta["rail_kind"])
	s.Equal("approval_required", meta["stage"])
	s.Equal("skill-a", meta["skill_name"])
	s.Equal("req-1", meta["request_id"])
}

// TestBuildEvolutionProgressEvent_带SkillName 验证 WithSkillName 选项，meta 包含 skill_name。
func (s *ApprovalEventsSuite) TestBuildEvolutionProgressEvent_带SkillName() {
	event := evolution.BuildEvolutionProgressEvent(
		"regular", "started", "begin",
		evolution.WithSkillName("my-skill"),
	)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	meta, ok := payload["_evolution_meta"].(map[string]string)
	s.Require().True(ok)
	s.Equal("my-skill", meta["skill_name"])

	// 无 request_id
	_, hasRequestID := meta["request_id"]
	s.False(hasRequestID)
}

// TestBuildEvolutionProgressEvent_带RequestID 验证 WithRequestID 选项，meta 包含 request_id。
func (s *ApprovalEventsSuite) TestBuildEvolutionProgressEvent_带RequestID() {
	event := evolution.BuildEvolutionProgressEvent(
		"regular", "started", "begin",
		evolution.WithRequestID("req-42"),
	)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	meta, ok := payload["_evolution_meta"].(map[string]string)
	s.Require().True(ok)
	s.Equal("req-42", meta["request_id"])

	// 无 skill_name
	_, hasSkillName := meta["skill_name"]
	s.False(hasSkillName)
}

// TestBuildSkillApprovalEvent_中文 验证中文技能审批事件，type/chat.ask_user_question，含中文 header/options。
//
// 对齐 Python: test_build_skill_approval_event_matches_existing_contract
func (s *ApprovalEventsSuite) TestBuildSkillApprovalEvent_中文() {
	records := []checkpointing.EvolutionRecord{
		makeTestRecord("first experience"),
		makeTestRecord("second experience"),
	}

	event := evolution.BuildSkillApprovalEvent(
		"skill-a", "skill_evolve_1234", records, "zh", false,
	)

	s.Equal("chat.ask_user_question", event.Type)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	s.Equal("skill_evolve_1234", payload["request_id"])

	// _evolution_meta
	meta, ok := payload["_evolution_meta"].(map[string]string)
	s.Require().True(ok)
	s.Equal("approval", meta["event_kind"])
	s.Equal("skill-a", meta["skill_name"])
	s.Equal("skill_evolve_1234", meta["request_id"])

	// questions
	questions, ok := payload["questions"].([]map[string]any)
	s.Require().True(ok)
	s.Len(questions, 2)
	s.Equal("技能演进审批", questions[0]["header"])
	s.Contains(questions[0]["question"], "Skill 'skill-a'")
}

// TestBuildSkillApprovalEvent_英文 验证英文技能审批事件。
//
// 对齐 Python: test_build_skill_approval_event_supports_english_language
func (s *ApprovalEventsSuite) TestBuildSkillApprovalEvent_英文() {
	records := []checkpointing.EvolutionRecord{
		makeTestRecord("english experience"),
	}

	event := evolution.BuildSkillApprovalEvent(
		"skill-a", "skill_evolve_en", records, "en", false,
	)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	questions, ok := payload["questions"].([]map[string]any)
	s.Require().True(ok)

	question := questions[0]
	s.Equal("Skill Evolution Approval", question["header"])
	s.Contains(question["question"], "Skill 'skill-a' generated a new experience")

	options, ok := question["options"].([]map[string]string)
	s.Require().True(ok)
	s.Equal("Accept", options[0]["label"])
	s.Equal("Reject", options[1]["label"])
}

// TestBuildSkillApprovalEvent_共享记录 验证 isSharedRecords=true 时 header 和 meta 包含 shared 信息。
//
// 对齐 Python: test_build_skill_approval_event_uses_shared_header_for_downloaded_records
func (s *ApprovalEventsSuite) TestBuildSkillApprovalEvent_共享记录() {
	records := []checkpointing.EvolutionRecord{
		makeTestRecord("shared experience"),
	}

	event := evolution.BuildSkillApprovalEvent(
		"skill-a", "skill_evolve_shared", records, "zh", true,
	)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	questions, ok := payload["questions"].([]map[string]any)
	s.Require().True(ok)
	s.Equal("在线共享经验审批", questions[0]["header"])

	meta, ok := payload["_evolution_meta"].(map[string]string)
	s.Require().True(ok)
	s.Equal("experience_sharing", meta["source"])
	s.Equal("true", meta["is_shared_records"])
}

// TestBuildSkillApprovalEvent_共享记录英文 验证 isSharedRecords=true 且英文时的 header。
//
// 对齐 Python: test_build_skill_approval_event_shared_header_supports_english_language
func (s *ApprovalEventsSuite) TestBuildSkillApprovalEvent_共享记录英文() {
	records := []checkpointing.EvolutionRecord{
		makeTestRecord("shared experience"),
	}

	event := evolution.BuildSkillApprovalEvent(
		"skill-a", "skill_evolve_shared_en", records, "en", true,
	)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	questions, ok := payload["questions"].([]map[string]any)
	s.Require().True(ok)
	s.Equal("Shared Experience Approval", questions[0]["header"])
}

// TestBuildSimplifyApprovalEvent_中文 验证中文精简审批事件。
//
// 对齐 Python: test_build_simplify_approval_event_matches_existing_contract
func (s *ApprovalEventsSuite) TestBuildSimplifyApprovalEvent_中文() {
	actions := []map[string]any{
		{"action": "DELETE", "record_id": "ev_1", "reason": "old"},
		{"action": "KEEP", "record_id": "ev_2", "reason": "good"},
	}

	event := evolution.BuildSimplifyApprovalEvent(
		"skill-a", "evolve_simplify_1234", actions, "zh",
	)

	s.Equal("chat.ask_user_question", event.Type)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	s.Equal("evolve_simplify_1234", payload["request_id"])

	questions, ok := payload["questions"].([]map[string]any)
	s.Require().True(ok)
	s.Equal("Skill 精简审批", questions[0]["header"])
	s.Contains(questions[0]["question"], "共 2 项操作")
}

// TestBuildSimplifyApprovalEvent_英文 验证英文精简审批事件。
//
// 对齐 Python: test_build_simplify_approval_event_supports_english_language
func (s *ApprovalEventsSuite) TestBuildSimplifyApprovalEvent_英文() {
	actions := []map[string]any{
		{"action": "DELETE", "record_id": "ev_1", "reason": "old"},
	}

	event := evolution.BuildSimplifyApprovalEvent(
		"skill-a", "evolve_simplify_en", actions, "en",
	)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	questions, ok := payload["questions"].([]map[string]any)
	s.Require().True(ok)

	question := questions[0]
	s.Equal("Skill Simplify Approval", question["header"])
	s.Contains(question["question"], "Simplify evolution experiences for Skill 'skill-a'")
	s.Contains(question["question"], "1 action(s)")

	options, ok := question["options"].([]map[string]string)
	s.Require().True(ok)
	s.Equal("Execute", options[0]["label"])
	s.Equal("Cancel", options[1]["label"])
}

// TestBuildTeamSkillApprovalEventFromRecords 验证从 EvolutionRecord 构建团队技能审批事件。
//
// 对齐 Python: test_build_team_skill_approval_event_from_records_matches_record_payloads
func (s *ApprovalEventsSuite) TestBuildTeamSkillApprovalEventFromRecords() {
	records := []checkpointing.EvolutionRecord{
		makeTestRecord("## Workflow\n- improve handoff"),
		makeTestRecord("## Troubleshooting\n- add retry note"),
	}

	event := evolution.BuildTeamSkillApprovalEventFromRecords(
		"team-skill-a", "skill_evolve_team_records", "zh", records,
	)

	s.Equal("chat.ask_user_question", event.Type)

	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	s.Equal("skill_evolve_team_records", payload["request_id"])

	questions, ok := payload["questions"].([]map[string]any)
	s.Require().True(ok)
	s.Len(questions, 2)
	s.Contains(questions[0]["question"], "团队技能 'team-skill-a'")
	s.Contains(questions[0]["question"], "improve handoff")
	s.Contains(questions[1]["question"], "add retry note")
}

// TestAttachEvolutionMeta 验证向事件附加 _evolution_meta。
func (s *ApprovalEventsSuite) TestAttachEvolutionMeta() {
	event := &stream.OutputSchema{
		Type:    "chat.ask_user_question",
		Index:   0,
		Payload: map[string]any{"request_id": "req-1"},
	}

	signalType := "approval"
	signalSource := "skill_experience"
	result := evolution.AttachEvolutionMeta(event, &signalType, &signalSource)

	s.Equal(event, result)

	payload, ok := result.Payload.(map[string]any)
	s.Require().True(ok)
	meta, ok := payload["_evolution_meta"].(map[string]string)
	s.Require().True(ok)
	s.Equal("approval", meta["event_kind"])
	s.Equal("approval", meta["signal_type"])
	s.Equal("skill_experience", meta["source"])
}

// TestAttachEvolutionMeta_已有Meta 验证已有 _evolution_meta 时追加字段。
func (s *ApprovalEventsSuite) TestAttachEvolutionMeta_已有Meta() {
	event := &stream.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"_evolution_meta": map[string]string{"event_kind": "progress"},
		},
	}

	signalSource := "tool_signal"
	result := evolution.AttachEvolutionMeta(event, nil, &signalSource)

	payload, ok := result.Payload.(map[string]any)
	s.Require().True(ok)
	meta, ok := payload["_evolution_meta"].(map[string]string)
	s.Require().True(ok)
	// 已有的 event_kind 不应被覆盖
	s.Equal("progress", meta["event_kind"])
	s.Equal("tool_signal", meta["source"])
}

// TestIsEn 通过导出函数间接验证语言判断逻辑。
// isEn 是非导出函数，通过 BuildSkillApprovalEvent 在不同语言下的行为来间接测试。
func (s *ApprovalEventsSuite) TestIsEn() {
	records := []checkpointing.EvolutionRecord{makeTestRecord("test")}

	// 中文（默认）
	eventZh := evolution.BuildSkillApprovalEvent("s", "r1", records, "zh", false)
	payloadZh, _ := eventZh.Payload.(map[string]any)
	questionsZh, _ := payloadZh["questions"].([]map[string]any)
	s.Equal("技能演进审批", questionsZh[0]["header"])

	// 英文
	eventEn := evolution.BuildSkillApprovalEvent("s", "r2", records, "en", false)
	payloadEn, _ := eventEn.Payload.(map[string]any)
	questionsEn, _ := payloadEn["questions"].([]map[string]any)
	s.Equal("Skill Evolution Approval", questionsEn[0]["header"])

	// 大写 EN
	eventENUpper := evolution.BuildSkillApprovalEvent("s", "r3", records, "EN", false)
	payloadENUpper, _ := eventENUpper.Payload.(map[string]any)
	questionsENUpper, _ := payloadENUpper["questions"].([]map[string]any)
	s.Equal("Skill Evolution Approval", questionsENUpper[0]["header"])

	// 带空格的 " en "
	eventENSpc := evolution.BuildSkillApprovalEvent("s", "r4", records, " en ", false)
	payloadENSpc, _ := eventENSpc.Payload.(map[string]any)
	questionsENSpc, _ := payloadENSpc["questions"].([]map[string]any)
	s.Equal("Skill Evolution Approval", questionsENSpc[0]["header"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeTestRecord 创建测试用 EvolutionRecord。
// 对齐 Python: _make_record
func makeTestRecord(content string) checkpointing.EvolutionRecord {
	return checkpointing.EvolutionRecord{
		Source:  "signal:skill-a",
		Context: "ctx",
		Change: checkpointing.EvolutionPatch{
			Section: "Troubleshooting",
			Action:  "append",
			Content: content,
			Target:  signal.EvolutionTargetBody,
		},
	}
}
