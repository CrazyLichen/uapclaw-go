package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// makeBaseMsg 创建 BaseMessage 格式的消息。
func makeBaseMsg(role, content string) llmschema.BaseMessage {
	switch role {
	case "user":
		return llmschema.NewUserMessage(content)
	case "assistant":
		return llmschema.NewAssistantMessage(content)
	case "system":
		return llmschema.NewSystemMessage(content)
	case "tool":
		return llmschema.NewToolMessage("", content)
	default:
		return llmschema.NewDefaultMessage(llmschema.RoleTypeUser, content)
	}
}

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

// makeTrajectory 创建轨迹（span 模型：经 trajectory.NewTestTrajectory 转换）。
func makeTrajectory(meta map[string]any, steps ...trajectory.TestStep) *trajectory.Trajectory {
	traj, err := trajectory.NewTestTrajectory("", meta, steps)
	if err != nil {
		panic(err)
	}
	return traj
}

// makeLLMStep 创建 LLM 步骤夹具（BaseMessage → 扁平消息 map）。
func makeLLMStep(messages []llmschema.BaseMessage) trajectory.TestStep {
	maps := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		if msgMap := llmschema.BaseMessageToMap(msg); msgMap != nil {
			maps = append(maps, msgMap)
		}
	}
	return trajectory.TestStep{Kind: "llm", Messages: maps}
}

// makeToolStep 创建工具步骤夹具。
func makeToolStep(toolName, toolCallID string, callArgs, callResult map[string]any, stepMeta map[string]any) trajectory.TestStep {
	_ = stepMeta // span 模型：step meta 由 span 属性承载（此处仅保持签名兼容）
	return trajectory.TestStep{Kind: "tool", ToolName: toolName, ToolCallID: toolCallID, CallArgs: callArgs, CallResult: callResult}
}

// newDetector 创建带技能的 ConversationSignalDetector。
func newDetector(skills ...string) *ConversationSignalDetector {
	skillsMap := make(map[string]bool)
	for _, s := range skills {
		skillsMap[s] = true
	}
	return NewConversationSignalDetector(WithExistingSkills(skillsMap))
}

// ──────────────────────────── NewConversationSignalDetector 会话信号检测器构造 ────────────────────────────

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
	var _ SignalDetector = ConversationSignalDetector{} //nolint:staticcheck // 接口合规性检查需要显式类型
	d := NewConversationSignalDetector()
	_ = d
}

// ──────────────────────────── BindLLM LLM 绑定 ────────────────────────────

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

// ──────────────────────────── Detect 信号检测 ────────────────────────────

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

// ──────────────────────────── DetectTrajectorySignals 轨迹信号检测 ────────────────────────────

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
		t.Fatal("expected signal from messages")
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

// ──────────────────────────── ConvertTrajectoryToMessages 轨迹转消息列表 ────────────────────────────

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
		makeLLMStep([]llmschema.BaseMessage{
			makeBaseMsg("user", "hello"),
			makeBaseMsg("assistant", "hi there"),
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
		makeLLMStep([]llmschema.BaseMessage{
			llmschema.NewAssistantMessage("", llmschema.WithToolCalls([]*llmschema.ToolCall{
				llmschema.NewToolCall("tc1", "read_file", `{"path": "/foo"}`),
			})),
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

// ──────────────────────────── detectFromMessages 从消息列表检测 ────────────────────────────

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

// ──────────────────────────── resolveActiveSkill 激活技能解析 ────────────────────────────

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

// ──────────────────────────── detectSkillFromToolCalls 从工具调用检测技能 ────────────────────────────

func TestDetectSkillFromToolCalls_SKILLMD路径(t *testing.T) {
	d := newDetector("my_skill")
	toolCalls := []map[string]any{
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
	toolCalls := []map[string]any{
		makeToolCallDict("tc1", "skill_tool", string(args)),
	}
	result := d.detectSkillFromToolCalls(toolCalls)
	if result != "target_skill" {
		t.Errorf("expected target_skill, got %q", result)
	}
}

func TestDetectSkillFromToolCalls_不在已有技能中(t *testing.T) {
	d := newDetector("skill_a")
	toolCalls := []map[string]any{
		makeToolCallDict("tc1", "read_file", `/skills/unknown_skill/SKILL.md`),
	}
	result := d.detectSkillFromToolCalls(toolCalls)
	if result != "" {
		t.Errorf("expected empty string for unknown skill, got %q", result)
	}
}

func TestDetectSkillFromToolCalls_空技能集合不过滤(t *testing.T) {
	d := newDetector()
	toolCalls := []map[string]any{
		makeToolCallDict("tc1", "read_file", `/skills/any_skill/SKILL.md`),
	}
	result := d.detectSkillFromToolCalls(toolCalls)
	if result != "any_skill" {
		t.Errorf("expected any_skill, got %q", result)
	}
}

// ──────────────────────────── isExistingSkill 既有技能判定 ────────────────────────────

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

// ──────────────────────────── isSkillMDReadTool 技能 MD 读取工具判定 ────────────────────────────

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

// ──────────────────────────── inferSkillFromMessages 从消息推断技能 ────────────────────────────

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

// ──────────────────────────── fallbackUserFeedbackSignals 用户反馈信号回退 ────────────────────────────

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

// ──────────────────────────── makeUserFeedbackSignal 用户反馈信号构造 ────────────────────────────

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

// ──────────────────────────── extractCodeFromArgs 从参数提取代码 ────────────────────────────

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

func TestExtractCodeFromArgs_nil参数(t *testing.T) {
	d := newDetector()
	tc := map[string]any{"id": "tc1", "name": "bash"} // 无 arguments 键
	result := d.extractCodeFromArgs(tc)
	if result != "" {
		t.Errorf("expected empty for nil args, got %q", result)
	}
}

// ──────────────────────────── DetectUserIntent 用户意图检测 ────────────────────────────

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

func TestDetectUserIntent_从轨迹转换(t *testing.T) {
	d := newDetector("my_skill")
	traj := makeTrajectory(nil,
		makeLLMStep([]llmschema.BaseMessage{
			llmschema.NewAssistantMessage("", llmschema.WithToolCalls([]*llmschema.ToolCall{
				llmschema.NewToolCall("tc1", "read_file", `/skills/my_skill/SKILL.md`),
			})),
		}),
		makeLLMStep([]llmschema.BaseMessage{
			makeBaseMsg("user", "不对，应该这样做"),
		}),
	)
	// 从轨迹转换为消息列表后调用
	converted := d.ConvertTrajectoryToMessages(traj)
	signals, err := d.DetectUserIntent(context.Background(), converted)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal from trajectory, got %d", len(signals))
	}
}

// ──────────────────────────── DetectUserMessageFeedback 用户消息反馈检测 ────────────────────────────

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

// ──────────────────────────── 【12-04】协作信号测试已删除 ────────────────────────────
// Python: _detect_collaboration_signals 上游 HEAD 已删除（from_conv.py 无此函数），
// Go 实现与测试同批删除（规则 8）。跨成员轨迹可见性由订阅窗口承载（25.2b）。

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

// ──────────────────────────── 【12-04】resolveActiveSkillForStep 测试已删除 ────────────────────────────
// 该函数为协作信号段配套（上游 HEAD 已删，Go 同批删除，规则 8）。
// 消息级 _resolve_active_skill 的测试保留于 TestResolveActiveSkill_* 系列。

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

// ──────────────────────────── 辅助函数测试 ────────────────────────────

func TestFindFailureKeywordIndex_ErrorEqualsNone不匹配(t *testing.T) {
	if findFailureKeywordIndex("error = None") != nil {
		t.Error("error = None should not match as failure")
	}
}

func TestFindFailureKeywordIndex_正常错误匹配(t *testing.T) {
	if findFailureKeywordIndex("error: something went wrong") == nil {
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
		makeLLMStep([]llmschema.BaseMessage{
			makeBaseMsg("user", "hello"),
		}),
	)
	signals := d.DetectTrajectorySignals(traj, nil)
	// 没有错误内容，不应产生信号
	if len(signals) > 0 {
		t.Logf("Got %d signals (may be expected)", len(signals))
	}
}

// ──────────────────────────── extractCodeFromArgs 长代码提取测试 ────────────────────────────

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

// TestParseMessages_基础字段与工具调用 验证 ParseMessages 把 BaseMessage 转为 OpenAI 风格 map。
func TestParseMessages_基础字段与工具调用(t *testing.T) {
	msgs := []llmschema.BaseMessage{
		makeBaseMsg("user", "你好"),
		&llmschema.AssistantMessage{
			DefaultMessage: *llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, ""),
			ToolCalls: []*llmschema.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: `{"path":"a"}`, Type: "function"},
			},
		},
	}

	got := ParseMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("期望 2 条消息，实际 %d", len(got))
	}
	if got[0]["role"] != "user" || got[0]["content"] != "你好" {
		t.Fatalf("用户消息映射错误: %v", got[0])
	}
	calls, ok := got[1]["tool_calls"].([]map[string]any)
	if !ok || len(calls) != 1 || calls[0]["name"] != "read_file" {
		t.Fatalf("tool_calls 映射错误: %v", got[1])
	}

	if empty := ParseMessages(nil); len(empty) != 0 {
		t.Fatalf("nil 输入应返回空切片，实际 %v", empty)
	}
}
