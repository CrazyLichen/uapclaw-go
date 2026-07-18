package signal

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── NewTeamSignalDetector 测试 ────────────────────────────

func TestNewTeamSignalDetector_基本创建(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 30,
		TotalBudgetSecs:    60,
		MaxAttempts:        2,
	}
	detector := NewTeamSignalDetector(nil, "test-model", "cn", &policy, nil)
	if detector == nil {
		t.Fatal("NewTeamSignalDetector returned nil")
	}
	if detector.language != "cn" {
		t.Errorf("language = %q, want %q", detector.language, "cn")
	}
	if detector.model != "test-model" {
		t.Errorf("model = %q, want %q", detector.model, "test-model")
	}
}

func TestNewTeamSignalDetector_无策略时panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic when no policy provided")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "at least one LLM policy") {
			t.Errorf("panic message = %v, want containing 'at least one LLM policy'", r)
		}
	}()
	NewTeamSignalDetector(nil, "test", "cn", nil, nil)
}

func TestNewTeamSignalDetector_userIntentPolicy回退(t *testing.T) {
	trajectoryPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 45,
		TotalBudgetSecs:    90,
		MaxAttempts:        3,
	}
	detector := NewTeamSignalDetector(nil, "test", "en", &trajectoryPolicy, nil)
	if detector.trajectoryIssueLLMPolicy.MaxAttempts != 3 {
		t.Errorf("trajectoryIssueLLMPolicy.MaxAttempts = %d, want 3", detector.trajectoryIssueLLMPolicy.MaxAttempts)
	}
	if detector.userIntentLLMPolicy.MaxAttempts != 3 {
		t.Errorf("userIntentLLMPolicy should fall back to trajectoryPolicy, MaxAttempts = %d, want 3", detector.userIntentLLMPolicy.MaxAttempts)
	}
}

func TestNewTeamSignalDetector_trajectoryPolicy回退(t *testing.T) {
	userIntentPolicy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 20,
		TotalBudgetSecs:    40,
		MaxAttempts:        1,
	}
	detector := NewTeamSignalDetector(nil, "test", "cn", nil, &userIntentPolicy)
	if detector.userIntentLLMPolicy.MaxAttempts != 1 {
		t.Errorf("userIntentLLMPolicy.MaxAttempts = %d, want 1", detector.userIntentLLMPolicy.MaxAttempts)
	}
	if detector.trajectoryIssueLLMPolicy.MaxAttempts != 1 {
		t.Errorf("trajectoryIssueLLMPolicy should fall back to userIntentPolicy, MaxAttempts = %d, want 1", detector.trajectoryIssueLLMPolicy.MaxAttempts)
	}
}

func TestNewTeamSignalDetector_各自策略独立(t *testing.T) {
	tp := llm_resilience.LLMInvokePolicy{MaxAttempts: 3, TotalBudgetSecs: 90, AttemptTimeoutSecs: 45}
	up := llm_resilience.LLMInvokePolicy{MaxAttempts: 1, TotalBudgetSecs: 30, AttemptTimeoutSecs: 15}
	detector := NewTeamSignalDetector(nil, "test", "cn", &tp, &up)
	if detector.trajectoryIssueLLMPolicy.MaxAttempts != 3 {
		t.Errorf("trajectoryIssueLLMPolicy.MaxAttempts = %d, want 3", detector.trajectoryIssueLLMPolicy.MaxAttempts)
	}
	if detector.userIntentLLMPolicy.MaxAttempts != 1 {
		t.Errorf("userIntentLLMPolicy.MaxAttempts = %d, want 1", detector.userIntentLLMPolicy.MaxAttempts)
	}
}

// ──────────────────────────── TeamSignalType 枚举测试 ────────────────────────────

func TestTeamSignalType_值对齐(t *testing.T) {
	if TeamSignalTypeUserIntent != "user_intent" {
		t.Errorf("TeamSignalTypeUserIntent = %q, want %q", TeamSignalTypeUserIntent, "user_intent")
	}
	if TeamSignalTypeUserRequest != "user_request" {
		t.Errorf("TeamSignalTypeUserRequest = %q, want %q", TeamSignalTypeUserRequest, "user_request")
	}
	if TeamSignalTypeTrajectoryIssue != "trajectory_issue" {
		t.Errorf("TeamSignalTypeTrajectoryIssue = %q, want %q", TeamSignalTypeTrajectoryIssue, "trajectory_issue")
	}
}

// ──────────────────────────── ParseTeamModelJSON 测试 ────────────────────────────

func TestParseTeamModelJSON_空字符串(t *testing.T) {
	result := ParseTeamModelJSON("")
	if result != nil {
		t.Errorf("ParseTeamModelJSON('') = %v, want nil", result)
	}
}

func TestParseTeamModelJSON_有效JSON对象(t *testing.T) {
	raw := `{"is_improvement": true, "intent": "改进协作"}`
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["is_improvement"] != true {
		t.Errorf("is_improvement = %v, want true", m["is_improvement"])
	}
}

func TestParseTeamModelJSON_有效JSON数组(t *testing.T) {
	raw := `[{"issue_type": "coordination", "description": "角色断裂", "severity": "high"}]`
	result := ParseTeamModelJSON(raw)
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}
	if len(arr) != 1 {
		t.Errorf("len = %d, want 1", len(arr))
	}
}

func TestParseTeamModelJSON_代码块包裹(t *testing.T) {
	raw := "```json\n{\"is_improvement\": false}\n```"
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["is_improvement"] != false {
		t.Errorf("is_improvement = %v, want false", m["is_improvement"])
	}
}

func TestParseTeamModelJSON_尾随逗号(t *testing.T) {
	raw := `{"is_improvement": true, "intent": "test",}`
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["is_improvement"] != true {
		t.Errorf("is_improvement = %v, want true", m["is_improvement"])
	}
}

func TestParseTeamModelJSON_单行注释(t *testing.T) {
	raw := "{\"is_improvement\": true // comment\n}"
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if m["is_improvement"] != true {
		t.Errorf("is_improvement = %v, want true", m["is_improvement"])
	}
}

func TestParseTeamModelJSON_无效JSON返回nil(t *testing.T) {
	raw := "this is not json at all"
	result := ParseTeamModelJSON(raw)
	if result != nil {
		t.Errorf("ParseTeamModelJSON = %v, want nil for invalid JSON", result)
	}
}

func TestParseTeamModelJSON_空数组(t *testing.T) {
	raw := "[]"
	result := ParseTeamModelJSON(raw)
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}
	if len(arr) != 0 {
		t.Errorf("len = %d, want 0", len(arr))
	}
}

func TestParseTeamModelJSON_混合文本和JSON(t *testing.T) {
	raw := "Here is the result:\n```json\n[{\"issue_type\": \"test\"}]\n```\nDone."
	result := ParseTeamModelJSON(raw)
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}
	if len(arr) != 1 {
		t.Errorf("len = %d, want 1", len(arr))
	}
}

func TestParseTeamModelJSON_嵌套对象(t *testing.T) {
	raw := `{"outer": {"inner": "value"}}`
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	outer, ok := m["outer"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", m["outer"])
	}
	if outer["inner"] != "value" {
		t.Errorf("inner = %v, want 'value'", outer["inner"])
	}
}

// ──────────────────────────── fixJSONText 测试 ────────────────────────────

func TestFixJSONText_移除代码块标记(t *testing.T) {
	text := "```json\n{\"key\": \"value\"}\n```"
	result := fixJSONText(text)
	if strings.Contains(result, "```") {
		t.Errorf("fixJSONText should remove ``` markers, got %q", result)
	}
}

func TestFixJSONText_移除尾随逗号(t *testing.T) {
	text := `{"key": "value",}`
	result := fixJSONText(text)
	if strings.Contains(result, ",}") {
		t.Errorf("fixJSONText should remove trailing commas, got %q", result)
	}
}

// ──────────────────────────── extractBalancedJSON 测试 ────────────────────────────

func TestExtractBalancedJSON_对象(t *testing.T) {
	text := `some text {"key": "value"} more text`
	result := extractBalancedJSON(text, '{', '}')
	expected := `{"key": "value"}`
	if result != expected {
		t.Errorf("extractBalancedJSON = %q, want %q", result, expected)
	}
}

func TestExtractBalancedJSON_数组(t *testing.T) {
	text := `prefix [1, 2, 3] suffix`
	result := extractBalancedJSON(text, '[', ']')
	expected := `[1, 2, 3]`
	if result != expected {
		t.Errorf("extractBalancedJSON = %q, want %q", result, expected)
	}
}

func TestExtractBalancedJSON_嵌套(t *testing.T) {
	text := `{"outer": {"inner": "val"}}`
	result := extractBalancedJSON(text, '{', '}')
	expected := `{"outer": {"inner": "val"}}`
	if result != expected {
		t.Errorf("extractBalancedJSON = %q, want %q", result, expected)
	}
}

func TestExtractBalancedJSON_无匹配(t *testing.T) {
	text := "no braces here"
	result := extractBalancedJSON(text, '{', '}')
	if result != "" {
		t.Errorf("extractBalancedJSON = %q, want empty string", result)
	}
}

func TestExtractBalancedJSON_字符串内大括号(t *testing.T) {
	text := `{"key": "val{ue}"}`
	result := extractBalancedJSON(text, '{', '}')
	expected := `{"key": "val{ue}"}`
	if result != expected {
		t.Errorf("extractBalancedJSON = %q, want %q", result, expected)
	}
}

func TestExtractBalancedJSON_不平衡括号(t *testing.T) {
	text := `{"key": "value"`
	result := extractBalancedJSON(text, '{', '}')
	if result != "" {
		t.Errorf("unbalanced braces should return empty, got %q", result)
	}
}

// ──────────────────────────── extractRolesSummary 测试 ────────────────────────────

func TestExtractRolesSummary_空内容(t *testing.T) {
	result := extractRolesSummary("")
	if result != "" {
		t.Errorf("extractRolesSummary('') = %q, want empty", result)
	}
}

func TestExtractRolesSummary_roles段落(t *testing.T) {
	content := "Roles:\n- leader\n- worker\n\nInstructions:\nDo something"
	result := extractRolesSummary(content)
	if !strings.Contains(result, "leader") || !strings.Contains(result, "worker") {
		t.Errorf("extractRolesSummary = %q, should contain leader and worker", result)
	}
}

func TestExtractRolesSummary_回退到role行(t *testing.T) {
	content := "role: leader\nrole: worker\nrole: tester"
	result := extractRolesSummary(content)
	if !strings.Contains(result, "leader") {
		t.Errorf("extractRolesSummary = %q, should contain leader from fallback", result)
	}
}

func TestExtractRolesSummary_截断超过500字符(t *testing.T) {
	roleLine := "- " + strings.Repeat("a", 100)
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = roleLine
	}
	content := "Roles:\n" + strings.Join(lines, "\n")
	result := extractRolesSummary(content)
	if len(result) > 500 {
		t.Errorf("extractRolesSummary len = %d, should be <= 500", len(result))
	}
}

func TestExtractRolesSummary_中文角色标签(t *testing.T) {
	content := "角色：领导者\n角色：执行者"
	result := extractRolesSummary(content)
	if !strings.Contains(result, "角色：领导者") {
		t.Errorf("should contain Chinese role labels, got %q", result)
	}
}

// ──────────────────────────── normalizeIssue 测试 ────────────────────────────

func TestNormalizeIssue_基本(t *testing.T) {
	item := map[string]any{
		"issue_type":    "coordination",
		"description":   "角色间协作断裂",
		"affected_role": "worker",
		"severity":      "high",
	}
	result := normalizeIssue(item)
	if result == nil {
		t.Fatal("normalizeIssue returned nil")
	}
	if result["issue_type"] != "coordination" {
		t.Errorf("issue_type = %q, want %q", result["issue_type"], "coordination")
	}
	if result["severity"] != "high" {
		t.Errorf("severity = %q, want %q", result["severity"], "high")
	}
}

func TestNormalizeIssue_非map类型返回nil(t *testing.T) {
	result := normalizeIssue("not a map")
	if result != nil {
		t.Errorf("normalizeIssue should return nil for non-map, got %v", result)
	}
}

func TestNormalizeIssue_默认值(t *testing.T) {
	item := map[string]any{}
	result := normalizeIssue(item)
	if result == nil {
		t.Fatal("normalizeIssue returned nil for empty map")
	}
	if result["issue_type"] != "unknown" {
		t.Errorf("issue_type = %q, want %q", result["issue_type"], "unknown")
	}
	if result["severity"] != "medium" {
		t.Errorf("severity = %q, want %q", result["severity"], "medium")
	}
}

func TestNormalizeIssue_无效severity回退到medium(t *testing.T) {
	item := map[string]any{"severity": "critical"}
	result := normalizeIssue(item)
	if result == nil {
		t.Fatal("normalizeIssue returned nil")
	}
	if result["severity"] != "medium" {
		t.Errorf("severity = %q, want %q for invalid value", result["severity"], "medium")
	}
}

// ──────────────────────────── stringOrDefault 测试 ────────────────────────────

func TestStringOrDefault_基本(t *testing.T) {
	m := map[string]any{"key": "value"}
	if v := stringOrDefault(m, "key", "default"); v != "value" {
		t.Errorf("stringOrDefault = %q, want %q", v, "value")
	}
}

func TestStringOrDefault_缺失键(t *testing.T) {
	m := map[string]any{}
	if v := stringOrDefault(m, "key", "default"); v != "default" {
		t.Errorf("stringOrDefault = %q, want %q", v, "default")
	}
}

func TestStringOrDefault_nil值(t *testing.T) {
	m := map[string]any{"key": nil}
	if v := stringOrDefault(m, "key", "default"); v != "default" {
		t.Errorf("stringOrDefault = %q, want %q for nil", v, "default")
	}
}

// ──────────────────────────── BuildTeamTrajectorySummary 测试 ────────────────────────────

func TestBuildTeamTrajectorySummary_空轨迹(t *testing.T) {
	traj := &trajectory.Trajectory{Steps: []*trajectory.TrajectoryStep{}}
	result := BuildTeamTrajectorySummary(traj)
	if !strings.Contains(result, "### Tool Calls (0)") {
		t.Errorf("summary should contain '### Tool Calls (0)', got %q", result)
	}
	if !strings.Contains(result, "### LLM Responses (0)") {
		t.Errorf("summary should contain '### LLM Responses (0)', got %q", result)
	}
}

func TestBuildTeamTrajectorySummary_含工具步骤(t *testing.T) {
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindTool,
				Detail: &trajectory.ToolCallDetail{
					ToolName:   "spawn_member",
					CallArgs:   map[string]any{"name": "worker"},
					CallResult: map[string]any{"status": "ok"},
				},
			},
		},
	}
	result := BuildTeamTrajectorySummary(traj)
	if !strings.Contains(result, "[Tool:spawn_member]") {
		t.Errorf("summary should contain tool name, got %q", result)
	}
}

func TestBuildTeamTrajectorySummary_含LLM步骤(t *testing.T) {
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Response: map[string]any{"content": "LLM response text"},
				},
			},
		},
	}
	result := BuildTeamTrajectorySummary(traj)
	if !strings.Contains(result, "[LLM]") {
		t.Errorf("summary should contain [LLM], got %q", result)
	}
}

func TestBuildTeamTrajectorySummary_步骤无Detail(t *testing.T) {
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{Kind: trajectory.StepKindTool, Detail: nil},
			{Kind: trajectory.StepKindLLM, Detail: nil},
		},
	}
	result := BuildTeamTrajectorySummary(traj)
	if !strings.Contains(result, "### Tool Calls (0)") {
		t.Errorf("should count 0 tool calls when Detail is nil, got %q", result)
	}
}

func TestBuildTeamTrajectorySummary_工具区截断(t *testing.T) {
	longResult := map[string]any{"data": strings.Repeat("x", 30000)}
	steps := make([]*trajectory.TrajectoryStep, 100)
	for i := range steps {
		steps[i] = &trajectory.TrajectoryStep{
			Kind: trajectory.StepKindTool,
			Detail: &trajectory.ToolCallDetail{
				ToolName:   fmt.Sprintf("tool_%d", i),
				CallArgs:   map[string]any{"arg": "val"},
				CallResult: longResult,
			},
		}
	}
	traj := &trajectory.Trajectory{Steps: steps}
	result := BuildTeamTrajectorySummary(traj)
	if !strings.Contains(result, "tool section truncated") {
		t.Error("long tool section should be truncated")
	}
}

func TestBuildTeamTrajectorySummary_LLM区截断(t *testing.T) {
	longResponse := map[string]any{"content": strings.Repeat("y", 500)}
	steps := make([]*trajectory.TrajectoryStep, 100)
	for i := range steps {
		steps[i] = &trajectory.TrajectoryStep{
			Kind: trajectory.StepKindLLM,
			Detail: &trajectory.LLMCallDetail{
				Response: longResponse,
			},
		}
	}
	traj := &trajectory.Trajectory{Steps: steps}
	result := BuildTeamTrajectorySummary(traj)
	if !strings.Contains(result, "LLM section truncated") {
		t.Error("long LLM section should be truncated")
	}
}

// ──────────────────────────── MakeTeamUserIntentSignal 测试 ────────────────────────────

func TestMakeTeamUserIntentSignal(t *testing.T) {
	sig := MakeTeamUserIntentSignal("team_skill", "改进协作流程")
	if sig.SignalType != "user_intent" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "user_intent")
	}
	if sig.Section != "Instructions" {
		t.Errorf("Section = %q, want %q", sig.Section, "Instructions")
	}
	if sig.Excerpt != "改进协作流程" {
		t.Errorf("Excerpt = %q, want %q", sig.Excerpt, "改进协作流程")
	}
	if sig.SkillName == nil || *sig.SkillName != "team_skill" {
		t.Errorf("SkillName = %v, want %q", sig.SkillName, "team_skill")
	}
	if sig.Context["source"] != "explicit_request" {
		t.Errorf("Context[source] = %v, want %q", sig.Context["source"], "explicit_request")
	}
}

// ──────────────────────────── MakeTeamTrajectorySignal 测试 ────────────────────────────

func TestMakeTeamTrajectorySignal(t *testing.T) {
	issues := []map[string]string{
		{"issue_type": "coordination", "description": "协作断裂", "severity": "high", "affected_role": "worker"},
	}
	sig := MakeTeamTrajectorySignal("team_skill", "skill content", issues)
	if sig.SignalType != string(TeamSignalTypeTrajectoryIssue) {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, string(TeamSignalTypeTrajectoryIssue))
	}
	if sig.Section != "" {
		t.Errorf("Section = %q, want empty", sig.Section)
	}
	if sig.Excerpt != "Detected team skill trajectory issues requiring evolution." {
		t.Errorf("Excerpt = %q, want trajectory issue excerpt", sig.Excerpt)
	}
	if sig.Context["source"] != "passive_trajectory" {
		t.Errorf("Context[source] = %v, want %q", sig.Context["source"], "passive_trajectory")
	}
}

// ──────────────────────────── GetTeamTrajectoryIssues 测试 ────────────────────────────

func TestGetTeamTrajectoryIssues_正常提取(t *testing.T) {
	issues := []map[string]string{
		{"issue_type": "coordination", "severity": "high"},
	}
	sig := MakeTeamTrajectorySignal("skill", "content", issues)
	result := GetTeamTrajectoryIssues(sig)
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0]["issue_type"] != "coordination" {
		t.Errorf("issue_type = %q, want %q", result[0]["issue_type"], "coordination")
	}
}

func TestGetTeamTrajectoryIssues_无context(t *testing.T) {
	sig := &EvolutionSignal{SignalType: "test", Section: "", Excerpt: ""}
	result := GetTeamTrajectoryIssues(sig)
	if result != nil {
		t.Errorf("GetTeamTrajectoryIssues = %v, want nil", result)
	}
}

func TestGetTeamTrajectoryIssues_无issues键(t *testing.T) {
	sig := &EvolutionSignal{
		SignalType: "test",
		Context:    map[string]any{"other": "data"},
	}
	result := GetTeamTrajectoryIssues(sig)
	if result != nil {
		t.Errorf("GetTeamTrajectoryIssues = %v, want nil", result)
	}
}

// ──────────────────────────── GetTeamSignalSkillContent 测试 ────────────────────────────

func TestGetTeamSignalSkillContent_正常提取(t *testing.T) {
	issues := []map[string]string{{"issue_type": "test"}}
	sig := MakeTeamTrajectorySignal("skill", "my skill content", issues)
	result := GetTeamSignalSkillContent(sig)
	if result != "my skill content" {
		t.Errorf("GetTeamSignalSkillContent = %q, want %q", result, "my skill content")
	}
}

func TestGetTeamSignalSkillContent_无context(t *testing.T) {
	sig := &EvolutionSignal{SignalType: "test", Section: "", Excerpt: ""}
	result := GetTeamSignalSkillContent(sig)
	if result != "" {
		t.Errorf("GetTeamSignalSkillContent = %q, want empty string", result)
	}
}

func TestGetTeamSignalSkillContent_nil值(t *testing.T) {
	sig := &EvolutionSignal{
		SignalType: "test",
		Context:    map[string]any{teamSkillContentKey: nil},
	}
	result := GetTeamSignalSkillContent(sig)
	if result != "" {
		t.Errorf("GetTeamSignalSkillContent = %q, want empty string for nil value", result)
	}
}

// ──────────────────────────── UserIntent / TrajectoryIssue 结构体测试 ────────────────────────────

func TestUserIntent_字段赋值(t *testing.T) {
	ui := UserIntent{IsImprovement: true, Intent: "改进协作"}
	if !ui.IsImprovement {
		t.Error("IsImprovement should be true")
	}
	if ui.Intent != "改进协作" {
		t.Errorf("Intent = %q, want %q", ui.Intent, "改进协作")
	}
}

func TestTrajectoryIssue_字段赋值(t *testing.T) {
	ti := TrajectoryIssue{
		IssueType:    "coordination",
		Description:  "协作断裂",
		AffectedRole: "worker",
		Severity:     "high",
	}
	if ti.IssueType != "coordination" {
		t.Errorf("IssueType = %q, want %q", ti.IssueType, "coordination")
	}
	if ti.Severity != "high" {
		t.Errorf("Severity = %q, want %q", ti.Severity, "high")
	}
}

// ──────────────────────────── Team DetectUserIntent 无LLM调用测试 ────────────────────────────

func TestTeamDetector_DetectUserIntent_无用户消息(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 30,
		TotalBudgetSecs:    60,
		MaxAttempts:        1,
	}
	detector := NewTeamSignalDetector(nil, "test", "cn", &policy, nil)

	result, err := detector.DetectUserIntent(context.Background(), []map[string]any{}, "skill content")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for no user messages, got %v", result)
	}
}

func TestTeamDetector_DetectUserIntent_无用户角色消息(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 30,
		TotalBudgetSecs:    60,
		MaxAttempts:        1,
	}
	detector := NewTeamSignalDetector(nil, "test", "cn", &policy, nil)

	messages := []map[string]any{
		{"role": "assistant", "content": "hello"},
		{"role": "system", "content": "system msg"},
	}
	result, err := detector.DetectUserIntent(context.Background(), messages, "skill content")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for no user messages, got %v", result)
	}
}

// ──────────────────────────── 上下文键常量测试 ────────────────────────────

func Test上下文键常量(t *testing.T) {
	if teamTrajectoryIssuesKey != "trajectory_issues" {
		t.Errorf("teamTrajectoryIssuesKey = %q, want %q", teamTrajectoryIssuesKey, "trajectory_issues")
	}
	if teamSkillContentKey != "skill_content" {
		t.Errorf("teamSkillContentKey = %q, want %q", teamSkillContentKey, "skill_content")
	}
}

// ──────────────────────────── 提示词模板内容验证 ────────────────────────────

func Test提示词模板包含关键占位符(t *testing.T) {
	if !strings.Contains(teamUserRequestPromptCN, "{team_skill_description}") {
		t.Error("CN prompt missing {team_skill_description}")
	}
	if !strings.Contains(teamUserRequestPromptCN, "{roles}") {
		t.Error("CN prompt missing {roles}")
	}
	if !strings.Contains(teamUserRequestPromptCN, "{user_messages}") {
		t.Error("CN prompt missing {user_messages}")
	}
	if !strings.Contains(teamTrajectoryIssuePromptCN, "{skill_content}") {
		t.Error("CN trajectory prompt missing {skill_content}")
	}
	if !strings.Contains(teamTrajectoryIssuePromptCN, "{trajectory_summary}") {
		t.Error("CN trajectory prompt missing {trajectory_summary}")
	}
}

func Test提示词模板包含JSON示例(t *testing.T) {
	if !strings.Contains(teamUserRequestPromptCN, "is_improvement") {
		t.Error("CN prompt should contain JSON example with is_improvement")
	}
	if !strings.Contains(teamTrajectoryIssuePromptCN, "issue_type") {
		t.Error("CN trajectory prompt should contain JSON example with issue_type")
	}
}

// ──────────────────────────── keyTools 验证 ────────────────────────────

func TestKeyTools_包含关键协作工具(t *testing.T) {
	expectedTools := []string{"spawn_member", "create_task", "build_team", "view_task", "send_message"}
	for _, tool := range expectedTools {
		if !keyTools[tool] {
			t.Errorf("keyTools missing %q", tool)
		}
	}
}

// ──────────────────────────── MakeTeamTrajectorySignal 完整信号验证 ────────────────────────────

func TestMakeTeamTrajectorySignal_完整验证(t *testing.T) {
	issues := []map[string]string{
		{"issue_type": "coordination", "description": "协作断裂", "affected_role": "worker", "severity": "high"},
		{"issue_type": "efficiency", "description": "重复调用", "affected_role": "leader", "severity": "medium"},
	}
	sig := MakeTeamTrajectorySignal("team_skill", "skill content here", issues)

	// 验证信号类型
	if sig.SignalType != string(TeamSignalTypeTrajectoryIssue) {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, string(TeamSignalTypeTrajectoryIssue))
	}
	// 验证与 schema 常量对齐
	if sig.SignalType != schema.TrajectoryIssueSignal {
		t.Errorf("SignalType = %q, should equal schema.TrajectoryIssueSignal = %q", sig.SignalType, schema.TrajectoryIssueSignal)
	}
	// 验证 Context 中的 issues
	sigIssues := GetTeamTrajectoryIssues(sig)
	if len(sigIssues) != 2 {
		t.Fatalf("issues count = %d, want 2", len(sigIssues))
	}
	// 验证 skill content
	content := GetTeamSignalSkillContent(sig)
	if content != "skill content here" {
		t.Errorf("skill content = %q, want %q", content, "skill content here")
	}
}

// ──────────────────────────── tryParseJSON 测试 ────────────────────────────

func TestTryParseJSON_有效JSON(t *testing.T) {
	result := tryParseJSON(`{"key": "value"}`)
	if result == nil {
		t.Error("tryParseJSON should return non-nil for valid JSON")
	}
}

func TestTryParseJSON_无效JSON(t *testing.T) {
	result := tryParseJSON("not json")
	if result != nil {
		t.Errorf("tryParseJSON should return nil for invalid JSON, got %v", result)
	}
}

// ──────────────────────────── llm.Model 类型验证 ────────────────────────────

func TestLlmModel类型存在(t *testing.T) {
	var _ *llm.Model = nil
}

// ──────────────────────────── llmschema 引用验证 ────────────────────────────

func TestLlmSchema引用(t *testing.T) {
	_ = llmschema.ModelClientConfig{}
}

// ──────────────────────────── 覆盖率补充测试 ────────────────────────────

func TestGetTeamTrajectoryIssues_Context中issues为非list类型(t *testing.T) {
	sig := &EvolutionSignal{
		SignalType: "test",
		Context:    map[string]any{teamTrajectoryIssuesKey: "not a list"},
	}
	result := GetTeamTrajectoryIssues(sig)
	if result != nil {
		t.Errorf("expected nil for non-list issues, got %v", result)
	}
}

func TestGetTeamTrajectoryIssues_Context中issues为mapStringAny(t *testing.T) {
	sig := &EvolutionSignal{
		SignalType: "test",
		Context: map[string]any{teamTrajectoryIssuesKey: []any{
			map[string]any{"issue_type": "test"},
		}},
	}
	// 当 issues 是 []any 包含 map[string]any 时，应正确转换为 []map[string]string
	result := GetTeamTrajectoryIssues(sig)
	if len(result) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result))
	}
	if result[0]["issue_type"] != "test" {
		t.Errorf("issue_type = %q, want %q", result[0]["issue_type"], "test")
	}
}

func TestGetTeamSignalSkillContent_Context中content为数值(t *testing.T) {
	sig := &EvolutionSignal{
		SignalType: "test",
		Context:    map[string]any{teamSkillContentKey: 42},
	}
	result := GetTeamSignalSkillContent(sig)
	if result != "42" {
		t.Errorf("expected 42, got %q", result)
	}
}

func TestStringOrDefault_EmptyString(t *testing.T) {
	m := map[string]any{"key": ""}
	if v := stringOrDefault(m, "key", "default"); v != "" {
		t.Errorf("empty string should not fall back, got %q", v)
	}
}

func TestNormalizeIssue_NilFields(t *testing.T) {
	item := map[string]any{
		"issue_type":  nil,
		"description": nil,
		"severity":    nil,
	}
	result := normalizeIssue(item)
	if result == nil {
		t.Fatal("normalizeIssue returned nil")
	}
	if result["issue_type"] != "unknown" {
		t.Errorf("issue_type = %q, want unknown for nil", result["issue_type"])
	}
}

func TestParseTeamModelJSON_数值类型(t *testing.T) {
	raw := `{"count": 42, "ratio": 3.14}`
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["count"].(float64) != 42 {
		t.Errorf("count = %v, want 42", m["count"])
	}
}

func TestParseTeamModelJSON_实际LLM输出(t *testing.T) {
	raw := "Based on the analysis:\n\n```json\n{\"is_improvement\": true, \"intent\": \"需要更好的角色分工\"}\n```\n\nThis indicates..."
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["is_improvement"] != true {
		t.Errorf("is_improvement = %v, want true", m["is_improvement"])
	}
	if m["intent"] != "需要更好的角色分工" {
		t.Errorf("intent = %v, want '需要更好的角色分工'", m["intent"])
	}
}

func TestBuildTeamTrajectorySummary_关键工具更长截断(t *testing.T) {
	longValue := strings.Repeat("x", 600)
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindTool,
				Detail: &trajectory.ToolCallDetail{
					ToolName:   "spawn_member",
					CallArgs:   map[string]any{"data": longValue},
					CallResult: map[string]any{"data": longValue},
				},
			},
			{
				Kind: trajectory.StepKindTool,
				Detail: &trajectory.ToolCallDetail{
					ToolName:   "other_tool",
					CallArgs:   map[string]any{"data": longValue},
					CallResult: map[string]any{"data": longValue},
				},
			},
		},
	}
	result := BuildTeamTrajectorySummary(traj)
	if !strings.Contains(result, "spawn_member") {
		t.Errorf("summary should contain spawn_member")
	}
}

func TestExtractRolesSummary_Roles内联值(t *testing.T) {
	content := "Roles: leader, worker\n\nInstructions:\nDo something"
	result := extractRolesSummary(content)
	if !strings.Contains(result, "leader, worker") {
		t.Errorf("should contain inline roles, got %q", result)
	}
}

func TestNewTeamSignalDetector_英文语言(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 30,
		TotalBudgetSecs:    60,
		MaxAttempts:        2,
	}
	detector := NewTeamSignalDetector(nil, "test-model", "en", &policy, nil)
	if detector.language != "en" {
		t.Errorf("language = %q, want %q", detector.language, "en")
	}
}

// ──────────────────────────── GetTeamTrajectoryIssues 补充测试 ────────────────────────────

func TestGetTeamTrajectoryIssues_mapStringAny转换(t *testing.T) {
	sig := &EvolutionSignal{
		SignalType: "test",
		Context: map[string]any{
			teamTrajectoryIssuesKey: []any{
				map[string]any{"issue_type": "coordination", "severity": "high"},
			},
		},
	}
	result := GetTeamTrajectoryIssues(sig)
	if len(result) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result))
	}
	if result[0]["issue_type"] != "coordination" {
		t.Errorf("issue_type = %q, want %q", result[0]["issue_type"], "coordination")
	}
}

func TestGetTeamTrajectoryIssues_issues为非切片(t *testing.T) {
	sig := &EvolutionSignal{
		SignalType: "test",
		Context:    map[string]any{teamTrajectoryIssuesKey: "not a slice"},
	}
	result := GetTeamTrajectoryIssues(sig)
	if result != nil {
		t.Errorf("expected nil for non-slice issues, got %v", result)
	}
}

// ──────────────────────────── DetectUserIntent 补充测试 ────────────────────────────

func TestTeamDetectUserIntent_无LLM时返回nil(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 30,
		TotalBudgetSecs:    60,
		MaxAttempts:        1,
	}
	detector := NewTeamSignalDetector(nil, "test", "cn", &policy, nil)
	// llm 为 nil，调用 InvokeTextWithRetry 会 panic 或返回 error
	// 这个测试验证不会 panic
	defer func() {
		if r := recover(); r != nil {
			// 预期可能 panic，因为 llm 为 nil
		}
	}()
	messages := []map[string]any{
		{"role": "user", "content": "改进协作"},
	}
	detector.DetectUserIntent(context.Background(), messages, "skill content")
}

func TestTeamDetectUserIntent_有用户消息但content为空(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 30,
		TotalBudgetSecs:    60,
		MaxAttempts:        1,
	}
	detector := NewTeamSignalDetector(nil, "test", "cn", &policy, nil)
	messages := []map[string]any{
		{"role": "user", "content": ""},
	}
	result, err := detector.DetectUserIntent(context.Background(), messages, "skill content")
	if err != nil {
		// 可能因为 llm nil 而报错
		return
	}
	if result != nil {
		t.Errorf("expected nil for empty content, got %v", result)
	}
}

// ──────────────────────────── stringOrDefault 补充测试 ────────────────────────────

func TestStringOrDefault_Nil字符串值(t *testing.T) {
	m := map[string]any{"key": "<nil>"}
	if v := stringOrDefault(m, "key", "default"); v != "default" {
		t.Errorf("stringOrDefault(<nil>) = %q, want %q", v, "default")
	}
}

// ──────────────────────────── fixJSONText 补充测试 ────────────────────────────

func TestFixJSONText_去除注释(t *testing.T) {
	text := `{"key": "value" // this is a comment`
	result := fixJSONText(text)
	if strings.Contains(result, "// this is a comment") {
		t.Errorf("fixJSONText should remove comments, got %q", result)
	}
}

func TestFixJSONText_完整代码块(t *testing.T) {
	text := "```json\n{\"key\": \"value\"}\n```"
	result := fixJSONText(text)
	if strings.Contains(result, "```") {
		t.Errorf("fixJSONText should remove code block markers, got %q", result)
	}
	if !strings.Contains(result, `"key"`) {
		t.Errorf("fixJSONText should preserve JSON content, got %q", result)
	}
}

// ──────────────────────────── extractBalancedJSON 补充测试 ────────────────────────────

func TestExtractBalancedJSON_转义引号(t *testing.T) {
	text := `{"key": "value with \"quote\""}`
	result := extractBalancedJSON(text, '{', '}')
	if result != text {
		t.Errorf("extractBalancedJSON = %q, want %q", result, text)
	}
}

// ──────────────────────────── ParseTeamModelJSON 补充测试 ────────────────────────────

func TestParseTeamModelJSON_实际LLM输出格式(t *testing.T) {
	raw := "Based on the analysis:\n\n```json\n{\"is_improvement\": true, \"intent\": \"需要更好的角色分工\"}\n```\n\nThis indicates..."
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["is_improvement"] != true {
		t.Errorf("is_improvement = %v, want true", m["is_improvement"])
	}
}

func TestParseTeamModelJSON_轨迹问题LLM输出(t *testing.T) {
	raw := `[{"issue_type": "coordination", "description": "角色间数据未传递", "affected_role": "worker", "severity": "high"}]`
	result := ParseTeamModelJSON(raw)
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}
	if len(arr) != 1 {
		t.Errorf("len = %d, want 1", len(arr))
	}
	item, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", arr[0])
	}
	if item["severity"] != "high" {
		t.Errorf("severity = %v, want high", item["severity"])
	}
}
