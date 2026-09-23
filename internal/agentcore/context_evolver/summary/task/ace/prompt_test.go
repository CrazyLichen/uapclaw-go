package ace

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewACEPrompt 构造 ACEPrompt 实例
func TestNewACEPrompt(t *testing.T) {
	p := NewACEPrompt()
	if p == nil {
		t.Fatal("NewACEPrompt 返回 nil")
	}
	if p.ACEReflectorPrompt == nil {
		t.Error("ACEReflectorPrompt 模板为 nil")
	}
	if p.ACEReflectorNoGTPrompt == nil {
		t.Error("ACEReflectorNoGTPrompt 模板为 nil")
	}
	if p.ACECuratorPrompt == nil {
		t.Error("ACECuratorPrompt 模板为 nil")
	}
	if p.ACEReflectorScalingPrompt == nil {
		t.Error("ACEReflectorScalingPrompt 模板为 nil")
	}
	if p.ACEReflectorScalingNoGTPrompt == nil {
		t.Error("ACEReflectorScalingNoGTPrompt 模板为 nil")
	}
	if p.ACECuratorScalingPrompt == nil {
		t.Error("ACECuratorScalingPrompt 模板为 nil")
	}
}

// TestACEPrompts 包级变量初始化
func TestACEPrompts(t *testing.T) {
	if ACEPrompts.ACEReflectorPrompt == nil {
		t.Error("ACEPrompts.ACEReflectorPrompt 为 nil")
	}
	if ACEPrompts.ACECuratorScalingPrompt == nil {
		t.Error("ACEPrompts.ACECuratorScalingPrompt 为 nil")
	}
}

// TestACEReflectorPrompt 执行 ACEReflectorPrompt 模板
func TestACEReflectorPrompt(t *testing.T) {
	data := reflectorPromptData{
		GroundTruth: "print('hello')",
		Feedback:    "all tests passed",
		Playbook:    "use API X",
		Trajectory:  "agent tried Y",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACEReflectorPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACEReflectorPrompt 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "print('hello')") {
		t.Error("ACEReflectorPrompt 结果缺少 GroundTruth")
	}
	if !strings.Contains(result, "all tests passed") {
		t.Error("ACEReflectorPrompt 结果缺少 Feedback")
	}
	if !strings.Contains(result, "use API X") {
		t.Error("ACEReflectorPrompt 结果缺少 Playbook")
	}
	if !strings.Contains(result, "agent tried Y") {
		t.Error("ACEReflectorPrompt 结果缺少 Trajectory")
	}
	if !strings.Contains(result, "GROUND_TRUTH_CODE_START") {
		t.Error("ACEReflectorPrompt 结果缺少 GROUND_TRUTH_CODE_START 标记")
	}
	if !strings.Contains(result, "TEST_REPORT_START") {
		t.Error("ACEReflectorPrompt 结果缺少 TEST_REPORT_START 标记")
	}
	if !strings.Contains(result, "PLAYBOOK_START") {
		t.Error("ACEReflectorPrompt 结果缺少 PLAYBOOK_START 标记")
	}
}

// TestACEReflectorNoGTPrompt 执行 ACEReflectorNoGTPrompt 模板
func TestACEReflectorNoGTPrompt(t *testing.T) {
	data := reflectorPromptData{
		Playbook:   "use API X",
		Trajectory: "agent tried Y",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACEReflectorNoGTPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACEReflectorNoGTPrompt 执行失败: %v", err)
	}
	result := buf.String()
	if strings.Contains(result, "GROUND_TRUTH_CODE_START") {
		t.Error("ACEReflectorNoGTPrompt 不应包含 GROUND_TRUTH_CODE_START")
	}
	if !strings.Contains(result, "PLAYBOOK_START") {
		t.Error("ACEReflectorNoGTPrompt 结果缺少 PLAYBOOK_START 标记")
	}
	if !strings.Contains(result, "agent tried Y") {
		t.Error("ACEReflectorNoGTPrompt 结果缺少 Trajectory")
	}
}

// TestACECuratorPrompt 执行 ACECuratorPrompt 模板
func TestACECuratorPrompt(t *testing.T) {
	data := curatorPromptData{
		QuestionContext: "find all items",
		Playbook:       "existing playbook",
		Trajectory:     "current attempt",
		Reflection:     "reflection result",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACECuratorPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACECuratorPrompt 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "find all items") {
		t.Error("ACECuratorPrompt 结果缺少 QuestionContext")
	}
	if !strings.Contains(result, "existing playbook") {
		t.Error("ACECuratorPrompt 结果缺少 Playbook")
	}
	if !strings.Contains(result, "current attempt") {
		t.Error("ACECuratorPrompt 结果缺少 Trajectory")
	}
	if !strings.Contains(result, "reflection result") {
		t.Error("ACECuratorPrompt 结果缺少 Reflection")
	}
	if !strings.Contains(result, "page_index") {
		t.Error("ACECuratorPrompt 结果缺少 page_index（示例中的反引号内容）")
	}
}

// TestACEReflectorScalingPrompt 执行 ACEReflectorScalingPrompt 模板
func TestACEReflectorScalingPrompt(t *testing.T) {
	data := reflectorScalingPromptData{
		GroundTruth:  "gt code",
		Playbook:     "playbook content",
		Trajectories: "trajectory 1\ntrajectory 2",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACEReflectorScalingPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACEReflectorScalingPrompt 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "gt code") {
		t.Error("ACEReflectorScalingPrompt 结果缺少 GroundTruth")
	}
	if !strings.Contains(result, "trajectory 1") {
		t.Error("ACEReflectorScalingPrompt 结果缺少 Trajectories")
	}
	if !strings.Contains(result, "GROUND_TRUTH_CODE_START") {
		t.Error("ACEReflectorScalingPrompt 结果缺少 GROUND_TRUTH_CODE_START")
	}
}

// TestACEReflectorScalingNoGTPrompt 执行 ACEReflectorScalingNoGTPrompt 模板
func TestACEReflectorScalingNoGTPrompt(t *testing.T) {
	data := reflectorScalingPromptData{
		Playbook:     "playbook content",
		Trajectories: "traj1\ntraj2",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACEReflectorScalingNoGTPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACEReflectorScalingNoGTPrompt 执行失败: %v", err)
	}
	result := buf.String()
	if strings.Contains(result, "GROUND_TRUTH_CODE_START") {
		t.Error("ACEReflectorScalingNoGTPrompt 不应包含 GROUND_TRUTH_CODE_START")
	}
	if !strings.Contains(result, "traj1") {
		t.Error("ACEReflectorScalingNoGTPrompt 结果缺少 Trajectories")
	}
}

// TestACECuratorScalingPrompt 执行 ACECuratorScalingPrompt 模板
func TestACECuratorScalingPrompt(t *testing.T) {
	data := curatorScalingPromptData{
		QuestionContext: "find all items",
		Playbook:       "existing playbook",
		Trajectories:   "attempt1\nattempt2",
		Reflection:     "reflection result",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACECuratorScalingPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACECuratorScalingPrompt 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "find all items") {
		t.Error("ACECuratorScalingPrompt 结果缺少 QuestionContext")
	}
	if !strings.Contains(result, "attempt1") {
		t.Error("ACECuratorScalingPrompt 结果缺少 Trajectories")
	}
	if !strings.Contains(result, "page_index") {
		t.Error("ACECuratorScalingPrompt 结果缺少 page_index（示例中的反引号内容）")
	}
}

// TestPromptJSONEscaping 验证 JSON 示例中的花括号转义正确
func TestPromptJSONEscaping(t *testing.T) {
	data := reflectorPromptData{
		GroundTruth: "gt",
		Feedback:    "fb",
		Playbook:    "pb",
		Trajectory:  "tr",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACEReflectorPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACEReflectorPrompt 执行失败: %v", err)
	}
	result := buf.String()
	// 验证 JSON 示例中的花括号被正确渲染（不被模板引擎解释）
	if !strings.Contains(result, `"reasoning":`) {
		t.Error("ACEReflectorPrompt 结果缺少 JSON 示例的 reasoning 字段")
	}
	if !strings.Contains(result, `"error_identification":`) {
		t.Error("ACEReflectorPrompt 结果缺少 JSON 示例的 error_identification 字段")
	}
	if !strings.Contains(result, `"root_cause_analysis":`) {
		t.Error("ACEReflectorPrompt 结果缺少 JSON 示例的 root_cause_analysis 字段")
	}
	if !strings.Contains(result, `"correct_approach":`) {
		t.Error("ACEReflectorPrompt 结果缺少 JSON 示例的 correct_approach 字段")
	}
	if !strings.Contains(result, `"key_insight":`) {
		t.Error("ACEReflectorPrompt 结果缺少 JSON 示例的 key_insight 字段")
	}
}

// TestACECuratorPromptBacktickEscaping 验证反引号转义正确
func TestACECuratorPromptBacktickEscaping(t *testing.T) {
	data := curatorPromptData{
		QuestionContext: "ctx",
		Playbook:       "pb",
		Trajectory:     "tr",
		Reflection:     "ref",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACECuratorPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACECuratorPrompt 执行失败: %v", err)
	}
	result := buf.String()
	// 验证 page_index 被反引号包围
	if !strings.Contains(result, "`page_index`") {
		t.Error("ACECuratorPrompt 结果缺少反引号包围的 page_index")
	}
}

// TestACECuratorScalingPromptBacktickEscaping 验证反引号转义正确
func TestACECuratorScalingPromptBacktickEscaping(t *testing.T) {
	data := curatorScalingPromptData{
		QuestionContext: "ctx",
		Playbook:       "pb",
		Trajectories:   "trajs",
		Reflection:     "ref",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACECuratorScalingPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACECuratorScalingPrompt 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "`page_index`") {
		t.Error("ACECuratorScalingPrompt 结果缺少反引号包围的 page_index")
	}
}

// TestACECuratorPromptMetadataEscaping 验证 metadata 示例中花括号转义
func TestACECuratorPromptMetadataEscaping(t *testing.T) {
	data := curatorPromptData{
		QuestionContext: "ctx",
		Playbook:       "pb",
		Trajectory:     "tr",
		Reflection:     "ref",
	}
	var buf bytes.Buffer
	if err := ACEPrompts.ACECuratorPrompt.Execute(&buf, data); err != nil {
		t.Fatalf("ACECuratorPrompt 执行失败: %v", err)
	}
	result := buf.String()
	// 验证 metadata 示例中的花括号正确渲染
	if !strings.Contains(result, `"helpful": 1`) {
		t.Error("ACECuratorPrompt 结果缺少 metadata helpful 示例")
	}
	if !strings.Contains(result, `"harmful": 0`) {
		t.Error("ACECuratorPrompt 结果缺少 metadata harmful 示例")
	}
}
