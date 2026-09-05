package evolution

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

func TestBuildProgressEvent(t *testing.T) {
	// 对齐 Python: build_progress_event("[Test]", "something happened")
	event := BuildProgressEvent("[Test]", "something happened")
	payload := event.Payload.(map[string]any)
	content := payload["content"].(string)
	if !strings.Contains(content, "[Test] something happened") {
		t.Errorf("content = %q, 应包含 [Test] something happened", content)
	}
	if event.Type != "llm_reasoning" {
		t.Errorf("Type = %q, want %q", event.Type, "llm_reasoning")
	}
	if event.Index != 0 {
		t.Errorf("Index = %d, want 0", event.Index)
	}
}

func TestBuildEvolutionProgressEvent_默认前缀(t *testing.T) {
	// 对齐 Python: build_evolution_progress_event(rail_kind="skill", stage="generating", message="started")
	event := BuildEvolutionProgressEvent("skill", "generating", "started")
	payload := event.Payload.(map[string]any)
	content := payload["content"].(string)
	if !strings.HasPrefix(content, "[Evolution]") {
		t.Errorf("content 应以 [Evolution] 开头，实际 %q", content)
	}
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["event_kind"] != "progress" {
		t.Errorf("event_kind = %q, want %q", meta["event_kind"], "progress")
	}
	if meta["rail_kind"] != "skill" {
		t.Errorf("rail_kind = %q, want %q", meta["rail_kind"], "skill")
	}
	if meta["stage"] != "generating" {
		t.Errorf("stage = %q, want %q", meta["stage"], "generating")
	}
}

func TestBuildEvolutionProgressEvent_可选参数(t *testing.T) {
	// 对齐 Python: build_evolution_progress_event(rail_kind="skill", stage="generating", message="started", skill_name="my_skill", request_id="req_1", prefix="[Custom]")
	event := BuildEvolutionProgressEvent("skill", "generating", "started",
		WithSkillName("my_skill"),
		WithRequestID("req_1"),
		WithPrefix("[Custom]"),
	)
	payload := event.Payload.(map[string]any)
	content := payload["content"].(string)
	if !strings.HasPrefix(content, "[Custom]") {
		t.Errorf("content 应以 [Custom] 开头，实际 %q", content)
	}
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["skill_name"] != "my_skill" {
		t.Errorf("skill_name = %q, want %q", meta["skill_name"], "my_skill")
	}
	if meta["request_id"] != "req_1" {
		t.Errorf("request_id = %q, want %q", meta["request_id"], "req_1")
	}
}

func TestAttachEvolutionMeta_默认event_kind(t *testing.T) {
	// 对齐 Python: attach_evolution_meta(event, signal_type=None, signal_source=None)
	event := &stream.OutputSchema{
		Type:    "chat.ask_user_question",
		Payload: map[string]any{},
	}
	result := AttachEvolutionMeta(event, nil, nil)
	payload := result.Payload.(map[string]any)
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["event_kind"] != "approval" {
		t.Errorf("event_kind = %q, want %q", meta["event_kind"], "approval")
	}
}

func TestAttachEvolutionMeta_注入signal字段(t *testing.T) {
	// 对齐 Python: attach_evolution_meta(event, signal_type="execution_failure", signal_source="online")
	sigType := "execution_failure"
	sigSource := "online"
	event := &stream.OutputSchema{
		Type:    "chat.ask_user_question",
		Payload: map[string]any{},
	}
	result := AttachEvolutionMeta(event, &sigType, &sigSource)
	payload := result.Payload.(map[string]any)
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["signal_type"] != "execution_failure" {
		t.Errorf("signal_type = %q, want %q", meta["signal_type"], "execution_failure")
	}
	if meta["source"] != "online" {
		t.Errorf("source = %q, want %q", meta["source"], "online")
	}
}

func makeTestRecords() []checkpointing.EvolutionRecord {
	return []checkpointing.EvolutionRecord{
		{
			ID: "ev_001",
			Change: checkpointing.EvolutionPatch{
				Section: "Troubleshooting",
				Content: "If the service fails to start, check the port configuration.",
				Target:  signal.EvolutionTargetBody,
			},
		},
	}
}

func TestBuildSkillApprovalEvent_CN(t *testing.T) {
	// 对齐 Python: build_skill_approval_event("my_skill", "req_1", records, language="cn")
	event := BuildSkillApprovalEvent("my_skill", "req_1", makeTestRecords(), "cn", false)
	if event.Type != "chat.ask_user_question" {
		t.Errorf("Type = %q, want %q", event.Type, "chat.ask_user_question")
	}
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	if len(questions) != 1 {
		t.Fatalf("questions 应有 1 条，实际 %d", len(questions))
	}
	q := questions[0]["question"].(string)
	// 对齐 Python 中文模板: "演进生成了新经验"
	if !strings.Contains(q, "演进生成了新经验") {
		t.Errorf("中文模板应包含 '演进生成了新经验'，实际 %q", q)
	}
	// 对齐 Python L96-99: "目标" / "章节"
	if !strings.Contains(q, "目标") {
		t.Errorf("中文模板应包含 '目标'，实际 %q", q)
	}
	if !strings.Contains(q, "章节") {
		t.Errorf("中文模板应包含 '章节'，实际 %q", q)
	}
	if questions[0]["header"] != "技能演进审批" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "技能演进审批")
	}
	opts := questions[0]["options"].([]map[string]string)
	if opts[0]["label"] != "接收" {
		t.Errorf("第一个 option label = %q, want %q", opts[0]["label"], "接收")
	}
	if opts[1]["label"] != "拒绝" {
		t.Errorf("第二个 option label = %q, want %q", opts[1]["label"], "拒绝")
	}
	if questions[0]["multi_select"] != false {
		t.Error("multi_select 应为 false")
	}
}

func TestBuildSkillApprovalEvent_EN(t *testing.T) {
	// 对齐 Python: build_skill_approval_event("my_skill", "req_1", records, language="en")
	event := BuildSkillApprovalEvent("my_skill", "req_1", makeTestRecords(), "en", false)
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	// 对齐 Python L89: "generated a new experience"
	if !strings.Contains(q, "generated a new experience") {
		t.Errorf("英文模板应包含 'generated a new experience'，实际 %q", q)
	}
	// 对齐 Python L89-90: "Target" / "Section"
	if !strings.Contains(q, "Target") {
		t.Errorf("英文模板应包含 'Target'，实际 %q", q)
	}
	if !strings.Contains(q, "Section") {
		t.Errorf("英文模板应包含 'Section'，实际 %q", q)
	}
	if questions[0]["header"] != "Skill Evolution Approval" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "Skill Evolution Approval")
	}
	opts := questions[0]["options"].([]map[string]string)
	if opts[0]["label"] != "Accept" {
		t.Errorf("第一个 option label = %q, want %q", opts[0]["label"], "Accept")
	}
}

func TestBuildSkillApprovalEvent_共享记录(t *testing.T) {
	// 对齐 Python: build_skill_approval_event(..., is_shared_records=True)
	event := BuildSkillApprovalEvent("my_skill", "req_1", makeTestRecords(), "cn", true)
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	// 对齐 Python L81: "在线共享经验审批"
	if questions[0]["header"] != "在线共享经验审批" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "在线共享经验审批")
	}
	meta := payload["_evolution_meta"].(map[string]string)
	// 对齐 Python L122: source="experience_sharing"
	if meta["source"] != "experience_sharing" {
		t.Errorf("source = %q, want %q", meta["source"], "experience_sharing")
	}
	// 对齐 Python L125: evolution_meta["is_shared_records"] = "true"
	if meta["is_shared_records"] != "true" {
		t.Errorf("is_shared_records = %q, want %q", meta["is_shared_records"], "true")
	}
}

func TestBuildSkillApprovalEvent_共享记录EN(t *testing.T) {
	// 对齐 Python: build_skill_approval_event(..., language="en", is_shared_records=True)
	event := BuildSkillApprovalEvent("my_skill", "req_1", makeTestRecords(), "en", true)
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	// 对齐 Python L81: "Shared Experience Approval"
	if questions[0]["header"] != "Shared Experience Approval" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "Shared Experience Approval")
	}
}

func TestBuildSimplifyApprovalEvent_CN(t *testing.T) {
	// 对齐 Python: build_simplify_approval_event("my_skill", "req_1", actions, language="cn")
	actions := []map[string]any{
		{"action": "remove", "record_id": "ev_001", "reason": "outdated"},
	}
	event := BuildSimplifyApprovalEvent("my_skill", "req_1", actions, "cn")
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	// 对齐 Python L165: "精简"
	if !strings.Contains(q, "精简") {
		t.Errorf("中文模板应包含 '精简'，实际 %q", q)
	}
	// 对齐 Python L167: "共 {len(actions)} 项操作"
	if !strings.Contains(q, "1 项操作") {
		t.Errorf("中文模板应包含 '1 项操作'，实际 %q", q)
	}
	// 对齐 Python L168: "是否执行？"
	if !strings.Contains(q, "是否执行？") {
		t.Errorf("中文模板应包含 '是否执行？'，实际 %q", q)
	}
	opts := questions[0]["options"].([]map[string]string)
	// 对齐 Python L179: "执行"
	if opts[0]["label"] != "执行" {
		t.Errorf("第一个 option label = %q, want %q", opts[0]["label"], "执行")
	}
	// 对齐 Python L180: "取消"
	if opts[1]["label"] != "取消" {
		t.Errorf("第二个 option label = %q, want %q", opts[1]["label"], "取消")
	}
}

func TestBuildSimplifyApprovalEvent_EN(t *testing.T) {
	// 对齐 Python: build_simplify_approval_event("my_skill", "req_1", actions, language="en")
	actions := []map[string]any{
		{"action": "remove", "record_id": "ev_001", "reason": "outdated"},
	}
	event := BuildSimplifyApprovalEvent("my_skill", "req_1", actions, "en")
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	// 对齐 Python L159: "Simplify"
	if !strings.Contains(q, "Simplify") {
		t.Errorf("英文模板应包含 'Simplify'，实际 %q", q)
	}
	// 对齐 Python L161: "action(s)"
	if !strings.Contains(q, "1 action(s)") {
		t.Errorf("英文模板应包含 '1 action(s)'，实际 %q", q)
	}
	// 对齐 Python L162: "Do you want to execute them?"
	if !strings.Contains(q, "Do you want to execute them?") {
		t.Errorf("英文模板应包含 'Do you want to execute them?'，实际 %q", q)
	}
	opts := questions[0]["options"].([]map[string]string)
	// 对齐 Python L175: "Execute"
	if opts[0]["label"] != "Execute" {
		t.Errorf("第一个 option label = %q, want %q", opts[0]["label"], "Execute")
	}
}

func TestBuildTeamSkillApprovalEventFromRecords_CN(t *testing.T) {
	// 对齐 Python: build_team_skill_approval_event_from_records("team_skill", "req_1", records, language="cn")
	event := BuildTeamSkillApprovalEventFromRecords("team_skill", "req_1", "cn", makeTestRecords())
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	// 对齐 Python L207: "团队技能"
	if !strings.Contains(q, "团队技能") {
		t.Errorf("中文模板应包含 '团队技能'，实际 %q", q)
	}
	// 对齐 Python L207: "章节"
	if !strings.Contains(q, "章节") {
		t.Errorf("中文模板应包含 '章节'，实际 %q", q)
	}
	if questions[0]["header"] != "团队技能演进审批" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "团队技能演进审批")
	}
	meta := payload["_evolution_meta"].(map[string]string)
	if meta["skill_name"] != "team_skill" {
		t.Errorf("skill_name = %q, want %q", meta["skill_name"], "team_skill")
	}
	if meta["request_id"] != "req_1" {
		t.Errorf("request_id = %q, want %q", meta["request_id"], "req_1")
	}
}

func TestBuildTeamSkillApprovalEventFromRecords_EN(t *testing.T) {
	// 对齐 Python: build_team_skill_approval_event_from_records("team_skill", "req_1", records, language="en")
	event := BuildTeamSkillApprovalEventFromRecords("team_skill", "req_1", "en", makeTestRecords())
	payload := event.Payload.(map[string]any)
	questions := payload["questions"].([]map[string]any)
	q := questions[0]["question"].(string)
	// 对齐 Python L205: "Team Skill"
	if !strings.Contains(q, "Team Skill") {
		t.Errorf("英文模板应包含 'Team Skill'，实际 %q", q)
	}
	// 对齐 Python L205: "Section"
	if !strings.Contains(q, "Section") {
		t.Errorf("英文模板应包含 'Section'，实际 %q", q)
	}
	if questions[0]["header"] != "Team Skill Evolution Approval" {
		t.Errorf("header = %q, want %q", questions[0]["header"], "Team Skill Evolution Approval")
	}
	opts := questions[0]["options"].([]map[string]string)
	if opts[0]["label"] != "Accept" {
		t.Errorf("第一个 option label = %q, want %q", opts[0]["label"], "Accept")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
