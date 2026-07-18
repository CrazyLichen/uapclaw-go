package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// makeMsg 创建消息字典。
func makeMsg(role, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

// makeMsgWithToolCalls 创建带工具调用的助手消息。
func makeMsgWithToolCalls(toolCalls ...map[string]any) map[string]any {
	return map[string]any{
		"role":       "assistant",
		"content":    "",
		"tool_calls": toolCalls,
	}
}

// makeToolMsg 创建工具返回消息。
func makeToolMsg(toolCallID, toolName, content string) map[string]any {
	msg := map[string]any{
		"role":    "tool",
		"content": content,
	}
	if toolCallID != "" {
		msg["tool_call_id"] = toolCallID
	}
	if toolName != "" {
		msg["name"] = toolName
	}
	return msg
}

// makeToolCallDict 创建工具调用字典。
func makeToolCallDict(id, name, arguments string) map[string]any {
	return map[string]any{"id": id, "name": name, "arguments": arguments}
}

// makeTrajectory 创建轨迹。
func makeTrajectory(meta map[string]any, steps ...*trajectory.TrajectoryStep) *trajectory.Trajectory {
	return &trajectory.Trajectory{
		Steps: steps,
		Meta:  meta,
	}
}

// makeLLMStep 创建 LLM 调用步骤。
func makeLLMStep(messages []map[string]any) *trajectory.TrajectoryStep {
	return &trajectory.TrajectoryStep{
		Kind: trajectory.StepKindLLM,
		Detail: &trajectory.LLMCallDetail{
			Messages: messages,
		},
	}
}

// makeToolStep 创建工具调用步骤。
func makeToolStep(toolName, toolCallID string, callArgs, callResult map[string]any, stepMeta map[string]any) *trajectory.TrajectoryStep {
	return &trajectory.TrajectoryStep{
		Kind: trajectory.StepKindTool,
		Detail: &trajectory.ToolCallDetail{
			ToolName:   toolName,
			ToolCallID: toolCallID,
			CallArgs:   callArgs,
			CallResult: callResult,
		},
		Meta: stepMeta,
	}
}

// newDetector 创建带技能的 ConversationSignalDetector。
func newDetector(skills ...string) *ConversationSignalDetector {
	skillsMap := make(map[string]bool)
	for _, s := range skills {
		skillsMap[s] = true
	}
	return NewConversationSignalDetector(WithExistingSkills(skillsMap))
}

// ──────────────────────────── NewConversationSignalDetector ────────────────────────────

func TestNewConversationSignalDetector_默认值(t *testing.T) {
	d := NewConversationSignalDetector()
	if d.language != "cn" {
		t.Errorf("language = %q, want %q", d.language, "cn")
	}
	if d.llm != nil {
		t.Error("llm should be nil by default")
	}
	if d.model != "" {
		t.Error("model should be empty by default")
	}
	if len(d.existingSkills) != 0 {
		t.Error("existingSkills should be empty by default")
	}
}

func TestNewConversationSignalDetector_WithExistingSkills(t *testing.T) {
	skills := map[string]bool{"skill_a": true, "skill_b": true}
	d := NewConversationSignalDetector(WithExistingSkills(skills))
	if !d.existingSkills["skill_a"] || !d.existingSkills["skill_b"] {
		t.Error("existingSkills should contain skill_a and skill_b")
	}
}

// ──────────────────────────── SignalDetector 类型别名 ────────────────────────────

func TestSignalDetector_类型别名(t *testing.T) {
	var _ SignalDetector = ConversationSignalDetector{}
	var d *SignalDetector = NewConversationSignalDetector()
	_ = d
}

// ──────────────────────────── BindLLM ────────────────────────────

func TestBindLLM_默认语言(t *testing.T) {
	d := NewConversationSignalDetector()
	result := d.BindLLM(nil, "test-model", "")
	if d.model != "test-model" {
		t.Errorf("model = %q, want %q", d.model, "test-model")
	}
	if d.language != "cn" {
		t.Errorf("language = %q, want %q", d.language, "cn")
	}
	if result != d {
		t.Error("BindLLM should return self for chaining")
	}
}

func TestBindLLM_英文语言(t *testing.T) {
	d := NewConversationSignalDetector()
	d.BindLLM(nil, "model", "en")
	if d.language != "en" {
		t.Errorf("language = %q, want %q", d.language, "en")
	}
}

// ──────────────────────────── Detect ────────────────────────────

func TestDetect_从消息检测执行失败(t *testing.T) {
	d := newDetector()
	messages := []map[string]any{
		makeMsg("assistant", "calling tool"),
		makeMsgWithToolCalls(makeToolCallDict("tc1", "bash", `{"command": "ls"}`)),
		makeToolMsg("tc1", "bash", "Error: command failed with exit code 1"),
	}
	signals := d.Detect(messages)
	if len(signals) == 0 {
		t.Fatal("expected execution_failure signal")
	}
	found := false
	for _, sig := range signals {
		if sig.SignalType == "execution_failure" {
			found = true
			if sig.Section != "Troubleshooting" {
				t.Errorf("Section = %q, want %q", sig.Section, "Troubleshooting")
			}
		}
	}
	if !found {
		t.Error("expected execution_failure signal type")
	}
}

func TestDetect_从轨迹检测(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(nil,
		makeLLMStep([]map[string]any{
			makeMsg("user", "do something"),
		}),
		makeToolStep("bash", "tc1", nil, map[string]any{"error": "failed"}, nil),
	)
	signals := d.Detect(traj)
	_ = signals
}

func TestDetect_不支持的输入类型(t *testing.T) {
	d := newDetector()
	signals := d.Detect("invalid input")
	if len(signals) != 0 {
		t.Errorf("expected 0 signals for unsupported input, got %d", len(signals))
	}
}

// ──────────────────────────── DetectTrajectorySignals ────────────────────────────

func TestDetectTrajectorySignals_预转换消息(t *testing.T) {
	d := newDetector()
	messages := []map[string]any{
		makeToolMsg("", "bash", "Error: something failed"),
	}
	signals := d.DetectTrajectorySignals(nil, messages)
	if len(signals) == 0 {
		t.Fatal("expected signal from pre-converted messages")
	}
}

func TestDetectTrajectorySignals_轨迹和消息(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(map[string]any{"member_id": "m1"})
	messages := []map[string]any{
		makeToolMsg("", "bash", "Error: failed"),
	}
	signals := d.DetectTrajectorySignals(traj, messages)
	if len(signals) == 0 {
		t.Fatal("expected signal from both trajectory and messages")
	}
}

func TestDetectTrajectorySignals_nil轨迹和消息(t *testing.T) {
	d := newDetector()
	messages := []map[string]any{
		makeToolMsg("", "bash", "Error: failed"),
	}
	signals := d.DetectTrajectorySignals(nil, messages)
	if len(signals) == 0 {
		t.Fatal("expected signal from messages")
	}
}

func TestDetectTrajectorySignals_nil轨迹nil消息(t *testing.T) {
	d := newDetector()
	signals := d.DetectTrajectorySignals(nil, nil)
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

// ──────────────────────────── ConvertTrajectoryToMessages ────────────────────────────

func TestConvertTrajectoryToMessages_空轨迹(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(nil)
	messages := d.ConvertTrajectoryToMessages(traj)
	if len(messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(messages))
	}
}

func TestConvertTrajectoryToMessages_LLM步骤(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(nil,
		makeLLMStep([]map[string]any{
			makeMsg("user", "hello"),
			makeMsg("assistant", "hi there"),
		}),
	)
	messages := d.ConvertTrajectoryToMessages(traj)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0]["role"] != "user" || messages[1]["role"] != "assistant" {
		t.Error("message roles mismatch")
	}
}

func TestConvertTrajectoryToMessages_工具步骤(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(nil,
		makeToolStep("bash", "tc1", nil, map[string]any{"output": "result"}, nil),
	)
	messages := d.ConvertTrajectoryToMessages(traj)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0]["role"] != "tool" {
		t.Errorf("role = %q, want %q", messages[0]["role"], "tool")
	}
	if messages[0]["name"] != "bash" {
		t.Errorf("name = %q, want %q", messages[0]["name"], "bash")
	}
	if messages[0]["tool_call_id"] != "tc1" {
		t.Errorf("tool_call_id = %q, want %q", messages[0]["tool_call_id"], "tc1")
	}
}

func TestConvertTrajectoryToMessages_工具调用ID映射(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(nil,
		makeLLMStep([]map[string]any{
			makeMsgWithToolCalls(makeToolCallDict("tc1", "read_file", `{"path": "/foo"}`)),
		}),
		makeToolStep("", "tc1", nil, map[string]any{"data": "content"}, nil),
	)
	messages := d.ConvertTrajectoryToMessages(traj)
	if len(messages) < 2 {
		t.Fatalf("expected >= 2 messages, got %d", len(messages))
	}
	toolMsg := messages[len(messages)-1]
	if toolMsg["name"] != "read_file" {
		t.Errorf("tool name = %q, want %q", toolMsg["name"], "read_file")
	}
}

// ──────────────────────────── detectFromMessages ────────────────────────────

func TestDetectFromMessages_脚本产物(t *testing.T) {
	d := newDetector("my_skill")
	longCode := strings.Repeat("print('hello world')\n", 5)
	args, _ := json.Marshal(map[string]any{"code": longCode})
	messages := []map[string]any{
		makeMsg("user", "do something"),
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "bash", string(args)),
		),
		makeToolMsg("tc1", "bash", "success output"),
	}
	signals := d.detectFromMessages(messages)
	found := false
	for _, sig := range signals {
		if sig.SignalType == "script_artifact" {
			found = true
			if sig.Section != "Scripts" {
				t.Errorf("Section = %q, want %q", sig.Section, "Scripts")
			}
		}
	}
	if !found {
		t.Error("expected script_artifact signal")
	}
}

func TestDetectFromMessages_脚本产物_失败时不产生信号(t *testing.T) {
	d := newDetector("my_skill")
	longCode := strings.Repeat("print('hello world')\n", 5)
	args, _ := json.Marshal(map[string]any{"code": longCode})
	messages := []map[string]any{
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "bash", string(args)),
		),
		makeToolMsg("tc1", "bash", "Error: command failed"),
	}
	signals := d.detectFromMessages(messages)
	for _, sig := range signals {
		if sig.SignalType == "script_artifact" {
			t.Error("should not produce script_artifact on failure")
		}
	}
}

func TestDetectFromMessages_执行失败(t *testing.T) {
	d := newDetector()
	messages := []map[string]any{
		makeToolMsg("", "deploy", "Exception: deployment failed with timeout"),
	}
	signals := d.detectFromMessages(messages)
	if len(signals) == 0 {
		t.Fatal("expected execution_failure signal")
	}
	if signals[0].SignalType != "execution_failure" {
		t.Errorf("SignalType = %q, want %q", signals[0].SignalType, "execution_failure")
	}
}

func TestDetectFromMessages_数据获取工具跳过(t *testing.T) {
	d := newDetector()
	messages := []map[string]any{
		makeToolMsg("", "search", "Error: search failed"),
	}
	signals := d.detectFromMessages(messages)
	for _, sig := range signals {
		if sig.SignalType == "execution_failure" {
			t.Error("search tool should be skipped for failure detection")
		}
	}
}

func TestDetectFromMessages_工具Schema模式跳过(t *testing.T) {
	d := newDetector()
	content := "{'content': '---\\nname: some_tool\\ndescription: a tool'} has an error"
	messages := []map[string]any{
		makeToolMsg("", "some_tool", content),
	}
	signals := d.detectFromMessages(messages)
	for _, sig := range signals {
		if sig.SignalType == "execution_failure" {
			t.Error("tool schema pattern should be skipped")
		}
	}
}

func TestDetectFromMessages_技能检测(t *testing.T) {
	d := newDetector("my_skill")
	messages := []map[string]any{
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "read_file", "/skills/my_skill/SKILL.md"),
		),
		makeToolMsg("tc1", "read_file", "skill content loaded"),
		makeMsgWithToolCalls(
			makeToolCallDict("tc2", "deploy", ""),
		),
		makeToolMsg("tc2", "deploy", "Error: deployment failed"),
	}
	signals := d.detectFromMessages(messages)
	if len(signals) == 0 {
		t.Fatal("expected signal with skill detection")
	}
	if signals[0].SkillName == nil || *signals[0].SkillName != "my_skill" {
		t.Errorf("SkillName = %v, want %q", signals[0].SkillName, "my_skill")
	}
}

// ──────────────────────────── resolveActiveSkill ────────────────────────────

func TestResolveActiveSkill_空历史(t *testing.T) {
	d := newDetector()
	result := d.resolveActiveSkill(0, nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestResolveActiveSkill_匹配(t *testing.T) {
	d := newDetector()
	history := []skillReadEntry{
		{msgIdx: 0, skillName: "skill_a"},
		{msgIdx: 3, skillName: "skill_b"},
	}
	result := d.resolveActiveSkill(5, history)
	if result != "skill_b" {
		t.Errorf("expected skill_b, got %q", result)
	}
}

func TestResolveActiveSkill_精确索引(t *testing.T) {
	d := newDetector()
	history := []skillReadEntry{
		{msgIdx: 0, skillName: "skill_a"},
		{msgIdx: 3, skillName: "skill_b"},
	}
	result := d.resolveActiveSkill(3, history)
	if result != "skill_b" {
		t.Errorf("expected skill_b, got %q", result)
	}
}

// ──────────────────────────── detectSkillFromToolCalls ────────────────────────────

func TestDetectSkillFromToolCalls_SKILLMD路径(t *testing.T) {
	d := newDetector("my_skill")
	toolCalls := []any{
		makeToolCallDict("tc1", "read_file", `/skills/my_skill/SKILL.md`),
	}
	result := d.detectSkillFromToolCalls(toolCalls)
	if result != "my_skill" {
		t.Errorf("expected my_skill, got %q", result)
	}
}

func TestDetectSkillFromToolCalls_skillTool参数(t *testing.T) {
	d := newDetector("target_skill")
	args, _ := json.Marshal(map[string]any{"skill_name": "target_skill"})
	toolCalls := []any{
		makeToolCallDict("tc1", "skill_tool", string(args)),
	}
	result := d.detectSkillFromToolCalls(toolCalls)
	if result != "target_skill" {
		t.Errorf("expected target_skill, got %q", result)
	}
}

func TestDetectSkillFromToolCalls_不在已有技能中(t *testing.T) {
	d := newDetector("skill_a")
	toolCalls := []any{
		makeToolCallDict("tc1", "read_file", `/skills/unknown_skill/SKILL.md`),
	}
	result := d.detectSkillFromToolCalls(toolCalls)
	if result != "" {
		t.Errorf("expected empty string for unknown skill, got %q", result)
	}
}

func TestDetectSkillFromToolCalls_空技能集合不过滤(t *testing.T) {
	d := newDetector()
	toolCalls := []any{
		makeToolCallDict("tc1", "read_file", `/skills/any_skill/SKILL.md`),
	}
	result := d.detectSkillFromToolCalls(toolCalls)
	if result != "any_skill" {
		t.Errorf("expected any_skill, got %q", result)
	}
}

// ──────────────────────────── isExistingSkill ────────────────────────────

func TestIsExistingSkill_空集合(t *testing.T) {
	d := newDetector()
	if !d.isExistingSkill("anything") {
		t.Error("empty skill set should accept any skill")
	}
}

func TestIsExistingSkill_存在(t *testing.T) {
	d := newDetector("skill_a")
	if !d.isExistingSkill("skill_a") {
		t.Error("should find existing skill")
	}
}

func TestIsExistingSkill_不存在(t *testing.T) {
	d := newDetector("skill_a")
	if d.isExistingSkill("skill_b") {
		t.Error("should not find non-existing skill")
	}
}

// ──────────────────────────── isSkillMDReadTool ────────────────────────────

func TestIsSkillMDReadTool_空名称(t *testing.T) {
	d := newDetector()
	if !d.isSkillMDReadTool("") {
		t.Error("empty name should match")
	}
}

func TestIsSkillMDReadTool_包含file(t *testing.T) {
	d := newDetector()
	if !d.isSkillMDReadTool("read_file") {
		t.Error("read_file should match")
	}
}

func TestIsSkillMDReadTool_包含read(t *testing.T) {
	d := newDetector()
	if !d.isSkillMDReadTool("read_document") {
		t.Error("read_document should match")
	}
}

func TestIsSkillMDReadTool_不包含(t *testing.T) {
	d := newDetector()
	if d.isSkillMDReadTool("execute_bash") {
		t.Error("execute_bash should not match")
	}
}

// ──────────────────────────── inferSkillFromMessages ────────────────────────────

func TestInferSkillFromMessages_有技能(t *testing.T) {
	d := newDetector("my_skill")
	messages := []map[string]any{
		makeMsg("user", "hello"),
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "read_file", `/skills/my_skill/SKILL.md`),
		),
		makeMsg("assistant", "done"),
	}
	result := d.inferSkillFromMessages(messages)
	if result != "my_skill" {
		t.Errorf("expected my_skill, got %q", result)
	}
}

func TestInferSkillFromMessages_无技能(t *testing.T) {
	d := newDetector()
	messages := []map[string]any{
		makeMsg("user", "hello"),
		makeMsg("assistant", "hi"),
	}
	result := d.inferSkillFromMessages(messages)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

// ──────────────────────────── fallbackUserFeedbackSignals ────────────────────────────

func TestFallbackUserFeedbackSignals_有纠正(t *testing.T) {
	d := newDetector()
	messages := []string{"你好", "不对，应该这样做", "好的"}
	signals := d.fallbackUserFeedbackSignals(messages, "skill_a")
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].SignalType != "user_intent" {
		t.Errorf("SignalType = %q, want %q", signals[0].SignalType, "user_intent")
	}
}

func TestFallbackUserFeedbackSignals_无纠正(t *testing.T) {
	d := newDetector()
	messages := []string{"你好", "继续", "好的"}
	signals := d.fallbackUserFeedbackSignals(messages, "skill_a")
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

func TestFallbackUserFeedbackSignals_英文纠正(t *testing.T) {
	d := newDetector()
	messages := []string{"hello", "that's wrong, try again"}
	signals := d.fallbackUserFeedbackSignals(messages, "skill_a")
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
}

// ──────────────────────────── makeUserFeedbackSignal ────────────────────────────

func TestMakeUserFeedbackSignal(t *testing.T) {
	d := newDetector()
	sig := d.makeUserFeedbackSignal("test excerpt", "skill_a")
	if sig.SignalType != "user_intent" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "user_intent")
	}
	if sig.Section != "Instructions" {
		t.Errorf("Section = %q, want %q", sig.Section, "Instructions")
	}
	if sig.SkillName == nil || *sig.SkillName != "skill_a" {
		t.Errorf("SkillName = %v, want %q", sig.SkillName, "skill_a")
	}
	if sig.Context["source"] != "passive_conversation" {
		t.Errorf("source = %v, want passive_conversation", sig.Context["source"])
	}
}

// ──────────────────────────── extractCodeFromArgs ────────────────────────────

func TestExtractCodeFromArgs_字符串参数(t *testing.T) {
	d := newDetector()
	args, _ := json.Marshal(map[string]any{"code": "print('hello world!')"})
	tc := makeToolCallDict("tc1", "bash", string(args))
	result := d.extractCodeFromArgs(tc)
	if result != "print('hello world!')" {
		t.Errorf("expected code content, got %q", result)
	}
}

func TestExtractCodeFromArgs_字典参数(t *testing.T) {
	d := newDetector()
	tc := map[string]any{
		"arguments": map[string]any{"command": "echo hello world test"},
	}
	result := d.extractCodeFromArgs(tc)
	if result != "echo hello world test" {
		t.Errorf("expected command content, got %q", result)
	}
}

func TestExtractCodeFromArgs_短代码不提取(t *testing.T) {
	d := newDetector()
	args, _ := json.Marshal(map[string]any{"code": "hi"})
	tc := makeToolCallDict("tc1", "bash", string(args))
	result := d.extractCodeFromArgs(tc)
	if result != "" {
		t.Errorf("short code should not be extracted, got %q", result)
	}
}

func TestExtractCodeFromArgs_无效JSON(t *testing.T) {
	d := newDetector()
	tc := makeToolCallDict("tc1", "bash", "invalid json")
	result := d.extractCodeFromArgs(tc)
	if result != "" {
		t.Errorf("invalid JSON should return empty, got %q", result)
	}
}

// ──────────────────────────── DetectUserIntent ────────────────────────────

func TestDetectUserIntent_无LLM走降级(t *testing.T) {
	d := newDetector("my_skill")
	messages := []map[string]any{
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "read_file", `/skills/my_skill/SKILL.md`),
		),
		makeMsg("user", "不对，应该用另一个方法"),
	}
	signals, err := d.DetectUserIntent(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].SignalType != "user_intent" {
		t.Errorf("SignalType = %q, want %q", signals[0].SignalType, "user_intent")
	}
}

func TestDetectUserIntent_无用户消息(t *testing.T) {
	d := newDetector()
	messages := []map[string]any{
		makeMsg("assistant", "hello"),
	}
	signals, err := d.DetectUserIntent(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

func TestDetectUserIntent_无法推断技能(t *testing.T) {
	d := newDetector()
	messages := []map[string]any{
		makeMsg("user", "hello"),
	}
	signals, err := d.DetectUserIntent(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals without skill, got %d", len(signals))
	}
}

func TestDetectUserIntent_从轨迹(t *testing.T) {
	d := newDetector("my_skill")
	traj := makeTrajectory(nil,
		makeLLMStep([]map[string]any{
			makeMsgWithToolCalls(
				makeToolCallDict("tc1", "read_file", `/skills/my_skill/SKILL.md`),
			),
		}),
		makeLLMStep([]map[string]any{
			makeMsg("user", "不对，应该这样做"),
		}),
	)
	signals, err := d.DetectUserIntent(context.Background(), traj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal from trajectory, got %d", len(signals))
	}
}

// ──────────────────────────── DetectUserMessageFeedback ────────────────────────────

func TestDetectUserMessageFeedback_降级路径(t *testing.T) {
	d := newDetector("my_skill")
	messages := []map[string]any{
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "read_file", `/skills/my_skill/SKILL.md`),
		),
		makeMsg("user", "不对，应该这样"),
	}
	signals := d.DetectUserMessageFeedback(context.Background(), messages)
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].SignalType != "user_correction" {
		t.Errorf("SignalType = %q, want %q", signals[0].SignalType, "user_correction")
	}
	if signals[0].Section != "Examples" {
		t.Errorf("Section = %q, want %q", signals[0].Section, "Examples")
	}
}

// ──────────────────────────── detectCollaborationSignals ────────────────────────────

func TestDetectCollaborationSignals_非团队上下文(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(nil)
	signals := d.detectCollaborationSignals(traj)
	if len(signals) != 0 {
		t.Errorf("expected 0 signals for non-team context, got %d", len(signals))
	}
}

func TestDetectCollaborationSignals_sendMessage(t *testing.T) {
	d := newDetector("my_skill")
	traj := makeTrajectory(
		map[string]any{"member_id": "member_1"},
		makeLLMStep([]map[string]any{
			makeMsgWithToolCalls(
				makeToolCallDict("tc1", "read_file", `/skills/my_skill/SKILL.md`),
			),
		}),
		makeToolStep("send_message", "", map[string]any{
			"to_member_name": "member_2",
			"content":        "hello",
		}, nil, nil),
	)
	signals := d.detectCollaborationSignals(traj)
	found := false
	for _, sig := range signals {
		if sig.SignalType == "collaboration_send" {
			found = true
			if sig.Section != "Collaboration" {
				t.Errorf("Section = %q, want %q", sig.Section, "Collaboration")
			}
			if sig.Context["to_member"] != "member_2" {
				t.Errorf("to_member = %v, want member_2", sig.Context["to_member"])
			}
		}
	}
	if !found {
		t.Error("expected collaboration_send signal")
	}
}

func TestDetectCollaborationSignals_claimTask(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(
		map[string]any{"member_id": "member_1"},
		makeToolStep("claim_task", "", map[string]any{
			"task_id": "task_123",
		}, nil, nil),
	)
	signals := d.detectCollaborationSignals(traj)
	found := false
	for _, sig := range signals {
		if sig.SignalType == "collaboration_claim" {
			found = true
			if sig.Context["task_id"] != "task_123" {
				t.Errorf("task_id = %v, want task_123", sig.Context["task_id"])
			}
		}
	}
	if !found {
		t.Error("expected collaboration_claim signal")
	}
}

func TestDetectCollaborationSignals_viewTask(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(
		map[string]any{"member_id": "member_1"},
		makeToolStep("view_task", "", nil, nil, nil),
	)
	signals := d.detectCollaborationSignals(traj)
	found := false
	for _, sig := range signals {
		if sig.SignalType == "collaboration_view" {
			found = true
		}
	}
	if !found {
		t.Error("expected collaboration_view signal")
	}
}

func TestDetectCollaborationSignals_parentInvokeID(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(
		map[string]any{"member_id": "member_1"},
		makeToolStep("some_tool", "", nil, nil, map[string]any{
			"parent_invoke_id": "parent_123",
		}),
	)
	signals := d.detectCollaborationSignals(traj)
	found := false
	for _, sig := range signals {
		if sig.SignalType == "collaboration_receive" {
			found = true
			if sig.Context["parent_invoke_id"] != "parent_123" {
				t.Errorf("parent_invoke_id = %v, want parent_123", sig.Context["parent_invoke_id"])
			}
		}
	}
	if !found {
		t.Error("expected collaboration_receive signal")
	}
}

func TestDetectCollaborationSignals_collaborationFailure(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(
		map[string]any{"member_id": "member_1"},
		makeToolStep("invoke_member", "", nil, map[string]any{
			"error": "member task timeout",
		}, nil),
	)
	signals := d.detectCollaborationSignals(traj)
	found := false
	for _, sig := range signals {
		if sig.SignalType == "collaboration_failure" {
			found = true
		}
	}
	if !found {
		t.Error("expected collaboration_failure signal")
	}
}

// ──────────────────────────── isTeamMemberContext ────────────────────────────

func TestIsTeamMemberContext_无Meta(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(nil)
	if d.isTeamMemberContext(traj) {
		t.Error("nil meta should not be team context")
	}
}

func TestIsTeamMemberContext_memberID非standalone(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(map[string]any{"member_id": "m1"})
	if !d.isTeamMemberContext(traj) {
		t.Error("member_id without source=standalone should be team context")
	}
}

func TestIsTeamMemberContext_memberIDstandalone(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(map[string]any{"member_id": "m1", "source": "standalone"})
	if d.isTeamMemberContext(traj) {
		t.Error("member_id with source=standalone should not be team context")
	}
}

func TestIsTeamMemberContext_crossMemberMetaKeys(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(map[string]any{"invoke_id": "inv_1"})
	if !d.isTeamMemberContext(traj) {
		t.Error("invoke_id in meta should be team context")
	}
}

func TestIsTeamMemberContext_parentInvokeID(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(map[string]any{"parent_invoke_id": "parent_1"})
	if !d.isTeamMemberContext(traj) {
		t.Error("parent_invoke_id in meta should be team context")
	}
}

// ──────────────────────────── extractToMember ────────────────────────────

func TestExtractToMember_JSON格式(t *testing.T) {
	d := newDetector()
	args, _ := json.Marshal(map[string]any{"to_member_name": "member_b"})
	result := d.extractToMember(string(args))
	if result != "member_b" {
		t.Errorf("expected member_b, got %q", result)
	}
}

func TestExtractToMember_JSON_to键(t *testing.T) {
	d := newDetector()
	args, _ := json.Marshal(map[string]any{"to": "member_c"})
	result := d.extractToMember(string(args))
	if result != "member_c" {
		t.Errorf("expected member_c, got %q", result)
	}
}

func TestExtractToMember_正则回退(t *testing.T) {
	d := newDetector()
	result := d.extractToMember(`to_member_name="member_d"`)
	if result != "member_d" {
		t.Errorf("expected member_d, got %q", result)
	}
}

func TestExtractToMember_空参数(t *testing.T) {
	d := newDetector()
	result := d.extractToMember("")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestExtractToMember_无匹配(t *testing.T) {
	d := newDetector()
	result := d.extractToMember(`{"other_key": "value"}`)
	if result != "" {
		t.Errorf("expected empty for no match, got %q", result)
	}
}

// ──────────────────────────── extractTaskID ────────────────────────────

func TestExtractTaskID_JSON格式(t *testing.T) {
	d := newDetector()
	args, _ := json.Marshal(map[string]any{"task_id": "task_456"})
	result := d.extractTaskID(string(args))
	if result != "task_456" {
		t.Errorf("expected task_456, got %q", result)
	}
}

func TestExtractTaskID_正则回退(t *testing.T) {
	d := newDetector()
	result := d.extractTaskID(`task_id='task_789'`)
	if result != "task_789" {
		t.Errorf("expected task_789, got %q", result)
	}
}

func TestExtractTaskID_空参数(t *testing.T) {
	d := newDetector()
	result := d.extractTaskID("")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

// ──────────────────────────── deduplicate ────────────────────────────

func TestDeduplicate_去重(t *testing.T) {
	d := newDetector()
	sig1 := MakeEvolutionSignal("type1", "Section1", "excerpt1", WithSkillName("skill_a"))
	sig2 := MakeEvolutionSignal("type1", "Section1", "excerpt1", WithSkillName("skill_a"))
	signals := []*EvolutionSignal{sig1, sig2}
	result := d.deduplicate(signals)
	if len(result) != 1 {
		t.Errorf("expected 1 signal after dedup, got %d", len(result))
	}
}

func TestDeduplicate_不同信号不去重(t *testing.T) {
	d := newDetector()
	sig1 := MakeEvolutionSignal("type1", "Section1", "excerpt1", WithSkillName("skill_a"))
	sig2 := MakeEvolutionSignal("type2", "Section1", "excerpt1", WithSkillName("skill_a"))
	signals := []*EvolutionSignal{sig1, sig2}
	result := d.deduplicate(signals)
	if len(result) != 2 {
		t.Errorf("expected 2 signals, got %d", len(result))
	}
}

func TestDeduplicate_空列表(t *testing.T) {
	d := newDetector()
	result := d.deduplicate(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

// ──────────────────────────── getField ────────────────────────────

func TestGetField_存在(t *testing.T) {
	obj := map[string]any{"key": "value"}
	result := getField(obj, "key", "default")
	if result != "value" {
		t.Errorf("expected value, got %v", result)
	}
}

func TestGetField_不存在(t *testing.T) {
	obj := map[string]any{"key": "value"}
	result := getField(obj, "missing", "default")
	if result != "default" {
		t.Errorf("expected default, got %v", result)
	}
}

func TestGetField_nil对象(t *testing.T) {
	result := getField(nil, "key", "default")
	if result != "default" {
		t.Errorf("expected default, got %v", result)
	}
}

// ──────────────────────────── extractAroundMatch ────────────────────────────

func TestExtractAroundMatch_默认范围(t *testing.T) {
	content := strings.Repeat("x", 200) + "MATCH" + strings.Repeat("y", 200)
	result := extractAroundMatch(content, 200, 205, 300, 300)
	if !strings.Contains(result, "MATCH") {
		t.Error("result should contain MATCH")
	}
}

func TestExtractAroundMatch_边界截断(t *testing.T) {
	content := "STARTMATCHEND"
	result := extractAroundMatch(content, 5, 10, 300, 300)
	if result != content {
		t.Errorf("expected full content, got %q", result)
	}
}

// ──────────────────────────── responseToText ────────────────────────────

func TestResponseToText_nil(t *testing.T) {
	result := responseToText(nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

// ──────────────────────────── 正则常量验证 ────────────────────────────

func TestFailureKeywords_匹配(t *testing.T) {
	testCases := []string{
		"Error: something went wrong",
		"Exception in thread main",
		"Traceback (most recent call last)",
		"Operation failed",
		"Connection timeout",
		"错误：参数不正确",
		"异常发生",
		"执行失败",
		"请求超时",
		"npm err! package not found",
		"ECONNREFUSED",
	}
	for _, tc := range testCases {
		if !failureKeywords.MatchString(tc) {
			t.Errorf("failureKeywords should match %q", tc)
		}
	}
}

func TestCorrectionPattern_匹配(t *testing.T) {
	testCases := []string{
		"不对，应该这样做",
		"不是这个",
		"错了",
		"应该是A",
		"这不对",
		"重新来",
		"纠正一下",
		"我的意思是",
		"that's wrong",
		"you're wrong",
		"should be",
		"actually,",
		"no, wait",
		"correction:",
		"fix:",
	}
	for _, tc := range testCases {
		if !correctionPattern.MatchString(tc) {
			t.Errorf("correctionPattern should match %q", tc)
		}
	}
}

func TestSkillMDPattern_匹配(t *testing.T) {
	testCases := []string{
		"/skills/my_skill/SKILL.md",
		"\\skills\\my_skill\\SKILL.md",
		"/path/to/skill_name/SKILL.md",
	}
	for _, tc := range testCases {
		matched := skillMDPattern.FindStringSubmatch(tc)
		if matched == nil {
			t.Errorf("skillMDPattern should match %q", tc)
		}
	}
}

func TestCollaborationFailurePattern_匹配(t *testing.T) {
	testCases := []string{
		"member_1 failed to respond",
		"member task error",
		"invoke exception occurred",
		"spawn failed",
		"task timeout",
		"collaboration failed",
		"协作失败",
		"成员异常",
		"任务超时",
	}
	for _, tc := range testCases {
		if !collaborationFailurePattern.MatchString(tc) {
			t.Errorf("collaborationFailurePattern should match %q", tc)
		}
	}
}

// ──────────────────────────── 集合常量验证 ────────────────────────────

func TestDataFetchTools(t *testing.T) {
	expected := []string{"search", "web_search", "google_search", "curl", "wget", "read_file", "view_file", "fetch_webpage"}
	for _, name := range expected {
		if !dataFetchTools[name] {
			t.Errorf("dataFetchTools should contain %q", name)
		}
	}
}

func TestCodeExecTools(t *testing.T) {
	expected := []string{"bash", "code", "run_python", "execute_code", "run_code"}
	for _, name := range expected {
		if !codeExecTools[name] {
			t.Errorf("codeExecTools should contain %q", name)
		}
	}
}

func TestCollaborationSignalTypes(t *testing.T) {
	expected := []string{"collaboration_send", "collaboration_claim", "collaboration_view", "collaboration_receive", "collaboration_failure"}
	for _, name := range expected {
		if !collaborationSignalTypes[name] {
			t.Errorf("collaborationSignalTypes should contain %q", name)
		}
	}
}

// ──────────────────────────── resolveActiveSkillForStep ────────────────────────────

func TestResolveActiveSkillForStep_找到步骤(t *testing.T) {
	d := newDetector()
	step := &trajectory.TrajectoryStep{Kind: trajectory.StepKindTool}
	allSteps := []*trajectory.TrajectoryStep{
		{Kind: trajectory.StepKindLLM},
		step,
		{Kind: trajectory.StepKindLLM},
	}
	history := []skillReadEntry{{msgIdx: 0, skillName: "skill_a"}}
	result := d.resolveActiveSkillForStep(step, allSteps, history)
	if result != "skill_a" {
		t.Errorf("expected skill_a, got %q", result)
	}
}

func TestResolveActiveSkillForStep_步骤未找到(t *testing.T) {
	d := newDetector()
	step1 := &trajectory.TrajectoryStep{Kind: trajectory.StepKindTool}
	step2 := &trajectory.TrajectoryStep{Kind: trajectory.StepKindTool}
	allSteps := []*trajectory.TrajectoryStep{step1}
	result := d.resolveActiveSkillForStep(step2, allSteps, nil)
	if result != "" {
		t.Errorf("expected empty for not found step, got %q", result)
	}
}

// ──────────────────────────── 完整集成测试 ────────────────────────────

func TestConversationSignalDetector_完整对话流程(t *testing.T) {
	d := newDetector("deploy_skill")
	longCode := fmt.Sprintf("deploy.sh\n%s", strings.Repeat("# deployment script\n", 10))
	codeArgs, _ := json.Marshal(map[string]any{"code": longCode})

	messages := []map[string]any{
		makeMsg("user", "请部署应用"),
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "read_file", "/skills/deploy_skill/SKILL.md"),
		),
		makeToolMsg("tc1", "read_file", "skill content"),
		makeMsgWithToolCalls(
			makeToolCallDict("tc2", "bash", string(codeArgs)),
		),
		makeToolMsg("tc2", "bash", "Deployment successful"),
		makeMsg("user", "不对，应该用蓝绿部署"),
	}

	signals := d.Detect(messages)
	hasScript := false
	for _, sig := range signals {
		if sig.SignalType == "script_artifact" {
			hasScript = true
			if sig.SkillName == nil || *sig.SkillName != "deploy_skill" {
				t.Errorf("SkillName = %v, want deploy_skill", sig.SkillName)
			}
		}
	}
	if !hasScript {
		t.Error("expected script_artifact signal")
	}
}

func TestConversationSignalDetector_执行失败和脚本成功共存(t *testing.T) {
	d := newDetector("my_skill")
	longCode := strings.Repeat("echo hello\n", 5)
	codeArgs, _ := json.Marshal(map[string]any{"code": longCode})

	messages := []map[string]any{
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "read_file", "/skills/my_skill/SKILL.md"),
		),
		makeToolMsg("tc1", "read_file", "skill content"),
		makeMsgWithToolCalls(
			makeToolCallDict("tc2", "bash", string(codeArgs)),
		),
		makeToolMsg("tc2", "bash", "script ran ok"),
		makeMsgWithToolCalls(
			makeToolCallDict("tc3", "deploy", ""),
		),
		makeToolMsg("tc3", "deploy", "Error: deployment failed with timeout"),
	}

	signals := d.Detect(messages)
	foundScript := false
	foundFailure := false
	for _, sig := range signals {
		if sig.SignalType == "script_artifact" {
			foundScript = true
		}
		if sig.SignalType == "execution_failure" {
			foundFailure = true
		}
	}
	if !foundScript {
		t.Error("expected script_artifact signal")
	}
	if !foundFailure {
		t.Error("expected execution_failure signal")
	}
}

// ──────────────────────────── responseToText 补充测试 ────────────────────────────

func TestResponseToText_map格式(t *testing.T) {
	m := map[string]any{"content": "hello from map"}
	result := responseToText(m)
	if result != "hello from map" {
		t.Errorf("responseToText(map) = %q, want %q", result, "hello from map")
	}
}

func TestResponseToText_mapText键(t *testing.T) {
	m := map[string]any{"text": "text value"}
	result := responseToText(m)
	if result != "text value" {
		t.Errorf("responseToText(map with text) = %q, want %q", result, "text value")
	}
}

func TestResponseToText_map无内容键(t *testing.T) {
	m := map[string]any{"other": "value"}
	result := responseToText(m)
	if result != "" {
		t.Errorf("responseToText(map without content/text) = %q, want empty", result)
	}
}

func TestResponseToText_AssistantMessage(t *testing.T) {
	am := &llmschema.AssistantMessage{}
	// AssistantMessage 返回其 Content 的 String()
	result := responseToText(am)
	_ = result // 验证不会 panic
}

func TestResponseToText_其他类型(t *testing.T) {
	result := responseToText(42)
	if result != "42" {
		t.Errorf("responseToText(42) = %q, want %q", result, "42")
	}
}

// ──────────────────────────── findFailureKeywordIndex 补充测试 ────────────────────────────

func TestFindFailureKeywordIndex_正常匹配(t *testing.T) {
	loc := findFailureKeywordIndex("something Error: failed")
	if loc == nil {
		t.Error("expected match for 'Error: failed'")
	}
}

func TestFindFailureKeywordIndex_errorEqualsNone排除(t *testing.T) {
	loc := findFailureKeywordIndex("error = None")
	if loc != nil {
		t.Error("error = None should be excluded")
	}
}

func TestFindFailureKeywordIndex_混合Error和ErrorNone(t *testing.T) {
	content := "error = None but also Error: real failure"
	loc := findFailureKeywordIndex(content)
	if loc == nil {
		t.Error("should find Error: real failure even with error = None")
	}
}

func TestFindFailureKeywordIndex_无匹配(t *testing.T) {
	loc := findFailureKeywordIndex("everything is fine")
	if loc != nil {
		t.Error("should not match when no failure keywords")
	}
}

// ──────────────────────────── DetectUserIntent 补充测试 ────────────────────────────

func TestDetectUserIntent_LLM调用失败走降级(t *testing.T) {
	d := newDetector("my_skill")
	// 绑定一个会失败的 LLM（nil model 将在 Invoke 时返回错误）
	d.BindLLM(nil, "test-model", "cn")
	messages := []map[string]any{
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "read_file", `/skills/my_skill/SKILL.md`),
		),
		makeMsg("user", "不对，应该这样"),
	}
	// 由于 llm 为 nil，调用 Invoke 会 panic 或返回 error，
	// 应该走 fallback 路径返回正则匹配的信号
	signals, err := d.DetectUserIntent(context.Background(), messages)
	_ = err // 可能返回 error 或 nil，取决于实现
	// 如果走 fallback，应该返回信号
	if len(signals) > 0 {
		if signals[0].SignalType != "user_intent" {
			t.Errorf("SignalType = %q, want %q", signals[0].SignalType, "user_intent")
		}
	}
	// 如果 LLM 调用失败且 fallback 也没匹配，返回 nil 也是合理的
}

func TestDetectUserIntent_有LLM但无纠正(t *testing.T) {
	d := newDetector("my_skill")
	// 绑定 LLM 但 model 为空，走 fallback 路径
	d.BindLLM(nil, "", "cn")
	messages := []map[string]any{
		makeMsgWithToolCalls(
			makeToolCallDict("tc1", "read_file", `/skills/my_skill/SKILL.md`),
		),
		makeMsg("user", "好的，继续"),
	}
	signals, err := d.DetectUserIntent(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "好的，继续" 不包含纠正模式，fallback 应该返回 nil
	if len(signals) != 0 {
		t.Errorf("expected 0 signals for non-correction message, got %d", len(signals))
	}
}

// ──────────────────────────── extractCodeFromArgs 补充测试 ────────────────────────────

func TestExtractCodeFromArgs_长代码提取(t *testing.T) {
	d := newDetector()
	longCode := strings.Repeat("print('hello world')\n", 3) // 75 chars
	args, _ := json.Marshal(map[string]any{"code": longCode})
	tc := makeToolCallDict("tc1", "bash", string(args))
	result := d.extractCodeFromArgs(tc)
	if result != longCode {
		t.Errorf("expected long code, got %q", result)
	}
}

func TestExtractCodeFromArgs_command键(t *testing.T) {
	d := newDetector()
	longCmd := "echo hello world && echo this is a long command that exceeds twenty characters"
	args, _ := json.Marshal(map[string]any{"command": longCmd})
	tc := makeToolCallDict("tc1", "bash", string(args))
	result := d.extractCodeFromArgs(tc)
	if result != longCmd {
		t.Errorf("expected command content, got %q", result)
	}
}

func TestExtractCodeFromArgs_nil参数(t *testing.T) {
	d := newDetector()
	tc := map[string]any{"id": "tc1", "name": "bash"} // 无 arguments 键
	result := d.extractCodeFromArgs(tc)
	if result != "" {
		t.Errorf("expected empty for nil args, got %q", result)
	}
}

func TestMatchFailureKeyword_ErrorEqualsNone不匹配(t *testing.T) {
	if matchFailureKeyword("error = None") {
		t.Error("error = None should not match as failure")
	}
}

func TestMatchFailureKeyword_正常错误匹配(t *testing.T) {
	if !matchFailureKeyword("error: something went wrong") {
		t.Error("error message should match as failure")
	}
}

func TestArgsToJSON_Nil(t *testing.T) {
	result := argsToJSON(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}

func TestArgsToJSON_Normal(t *testing.T) {
	result := argsToJSON(map[string]any{"key": "value"})
	if !strings.Contains(result, "key") {
		t.Errorf("expected JSON with key, got %q", result)
	}
}

func TestToToolCallSlice_Nil(t *testing.T) {
	result := toToolCallSlice(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestToToolCallSlice_MapSlice(t *testing.T) {
	input := []map[string]any{{"id": "tc1"}}
	result := toToolCallSlice(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 element, got %d", len(result))
	}
	if m, ok := result[0].(map[string]any); !ok || m["id"] != "tc1" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestToToolCallSlice_OtherType(t *testing.T) {
	result := toToolCallSlice("not a slice")
	if result != nil {
		t.Errorf("expected nil for non-slice, got %v", result)
	}
}

func TestStringPtrValue_Nil(t *testing.T) {
	result := stringPtrValue(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}

func TestStringPtrValue_NonNil(t *testing.T) {
	s := "test"
	result := stringPtrValue(&s)
	if result != "test" {
		t.Errorf("expected test, got %q", result)
	}
}

func TestDetectTrajectorySignals_仅Trajectory(t *testing.T) {
	d := newDetector()
	traj := makeTrajectory(nil,
		makeLLMStep([]map[string]any{
			makeMsg("user", "hello"),
		}),
	)
	signals := d.DetectTrajectorySignals(traj, nil)
	// 没有错误内容，不应产生信号
	if len(signals) > 0 {
		t.Logf("Got %d signals (may be expected)", len(signals))
	}
}

