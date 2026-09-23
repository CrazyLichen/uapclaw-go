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
	if p.Reflector == nil {
		t.Error("Reflector 模板为 nil")
	}
	if p.ReflectorNoGT == nil {
		t.Error("ReflectorNoGT 模板为 nil")
	}
	if p.Curator == nil {
		t.Error("Curator 模板为 nil")
	}
	if p.ReflectorScaling == nil {
		t.Error("ReflectorScaling 模板为 nil")
	}
	if p.ReflectorScalingNoGT == nil {
		t.Error("ReflectorScalingNoGT 模板为 nil")
	}
	if p.CuratorScaling == nil {
		t.Error("CuratorScaling 模板为 nil")
	}
}

// TestDefaultACEPrompt 包级变量初始化
func TestDefaultACEPrompt(t *testing.T) {
	if defaultACEPrompt.Reflector == nil {
		t.Error("defaultACEPrompt.Reflector 为 nil")
	}
	if defaultACEPrompt.CuratorScaling == nil {
		t.Error("defaultACEPrompt.CuratorScaling 为 nil")
	}
}

// TestReflectorPrompt 执行 Reflector 模板
func TestReflectorPrompt(t *testing.T) {
	data := reflectorPromptData{
		GroundTruth: "print('hello')",
		Feedback:    "all tests passed",
		Playbook:    "use API X",
		Trajectory:  "agent tried Y",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.Reflector.Execute(&buf, data); err != nil {
		t.Fatalf("Reflector 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "print('hello')") {
		t.Error("Reflector 结果缺少 GroundTruth")
	}
	if !strings.Contains(result, "all tests passed") {
		t.Error("Reflector 结果缺少 Feedback")
	}
	if !strings.Contains(result, "use API X") {
		t.Error("Reflector 结果缺少 Playbook")
	}
	if !strings.Contains(result, "agent tried Y") {
		t.Error("Reflector 结果缺少 Trajectory")
	}
	if !strings.Contains(result, "GROUND_TRUTH_CODE_START") {
		t.Error("Reflector 结果缺少 GROUND_TRUTH_CODE_START 标记")
	}
	if !strings.Contains(result, "TEST_REPORT_START") {
		t.Error("Reflector 结果缺少 TEST_REPORT_START 标记")
	}
	if !strings.Contains(result, "PLAYBOOK_START") {
		t.Error("Reflector 结果缺少 PLAYBOOK_START 标记")
	}
}

// TestReflectorNoGTPrompt 执行 ReflectorNoGT 模板
func TestReflectorNoGTPrompt(t *testing.T) {
	data := reflectorPromptData{
		Playbook:   "use API X",
		Trajectory: "agent tried Y",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.ReflectorNoGT.Execute(&buf, data); err != nil {
		t.Fatalf("ReflectorNoGT 执行失败: %v", err)
	}
	result := buf.String()
	if strings.Contains(result, "GROUND_TRUTH_CODE_START") {
		t.Error("ReflectorNoGT 不应包含 GROUND_TRUTH_CODE_START")
	}
	if !strings.Contains(result, "PLAYBOOK_START") {
		t.Error("ReflectorNoGT 结果缺少 PLAYBOOK_START 标记")
	}
	if !strings.Contains(result, "agent tried Y") {
		t.Error("ReflectorNoGT 结果缺少 Trajectory")
	}
}

// TestCuratorPrompt 执行 Curator 模板
func TestCuratorPrompt(t *testing.T) {
	data := curatorPromptData{
		QuestionContext: "find all items",
		Playbook:       "existing playbook",
		Trajectory:     "current attempt",
		Reflection:     "reflection result",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.Curator.Execute(&buf, data); err != nil {
		t.Fatalf("Curator 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "find all items") {
		t.Error("Curator 结果缺少 QuestionContext")
	}
	if !strings.Contains(result, "existing playbook") {
		t.Error("Curator 结果缺少 Playbook")
	}
	if !strings.Contains(result, "current attempt") {
		t.Error("Curator 结果缺少 Trajectory")
	}
	if !strings.Contains(result, "reflection result") {
		t.Error("Curator 结果缺少 Reflection")
	}
	if !strings.Contains(result, "page_index") {
		t.Error("Curator 结果缺少 page_index（示例中的反引号内容）")
	}
}

// TestReflectorScalingPrompt 执行 ReflectorScaling 模板
func TestReflectorScalingPrompt(t *testing.T) {
	data := reflectorScalingPromptData{
		GroundTruth:  "gt code",
		Playbook:     "playbook content",
		Trajectories: "trajectory 1\ntrajectory 2",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.ReflectorScaling.Execute(&buf, data); err != nil {
		t.Fatalf("ReflectorScaling 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "gt code") {
		t.Error("ReflectorScaling 结果缺少 GroundTruth")
	}
	if !strings.Contains(result, "trajectory 1") {
		t.Error("ReflectorScaling 结果缺少 Trajectories")
	}
	if !strings.Contains(result, "GROUND_TRUTH_CODE_START") {
		t.Error("ReflectorScaling 结果缺少 GROUND_TRUTH_CODE_START")
	}
}

// TestReflectorScalingNoGTPrompt 执行 ReflectorScalingNoGT 模板
func TestReflectorScalingNoGTPrompt(t *testing.T) {
	data := reflectorScalingPromptData{
		Playbook:     "playbook content",
		Trajectories: "traj1\ntraj2",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.ReflectorScalingNoGT.Execute(&buf, data); err != nil {
		t.Fatalf("ReflectorScalingNoGT 执行失败: %v", err)
	}
	result := buf.String()
	if strings.Contains(result, "GROUND_TRUTH_CODE_START") {
		t.Error("ReflectorScalingNoGT 不应包含 GROUND_TRUTH_CODE_START")
	}
	if !strings.Contains(result, "traj1") {
		t.Error("ReflectorScalingNoGT 结果缺少 Trajectories")
	}
}

// TestCuratorScalingPrompt 执行 CuratorScaling 模板
func TestCuratorScalingPrompt(t *testing.T) {
	data := curatorScalingPromptData{
		QuestionContext: "find all items",
		Playbook:       "existing playbook",
		Trajectories:   "attempt1\nattempt2",
		Reflection:     "reflection result",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.CuratorScaling.Execute(&buf, data); err != nil {
		t.Fatalf("CuratorScaling 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "find all items") {
		t.Error("CuratorScaling 结果缺少 QuestionContext")
	}
	if !strings.Contains(result, "attempt1") {
		t.Error("CuratorScaling 结果缺少 Trajectories")
	}
	if !strings.Contains(result, "page_index") {
		t.Error("CuratorScaling 结果缺少 page_index（示例中的反引号内容）")
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
	if err := defaultACEPrompt.Reflector.Execute(&buf, data); err != nil {
		t.Fatalf("Reflector 执行失败: %v", err)
	}
	result := buf.String()
	// 验证 JSON 示例中的花括号被正确渲染（不被模板引擎解释）
	if !strings.Contains(result, `"reasoning":`) {
		t.Error("Reflector 结果缺少 JSON 示例的 reasoning 字段")
	}
	if !strings.Contains(result, `"error_identification":`) {
		t.Error("Reflector 结果缺少 JSON 示例的 error_identification 字段")
	}
	if !strings.Contains(result, `"root_cause_analysis":`) {
		t.Error("Reflector 结果缺少 JSON 示例的 root_cause_analysis 字段")
	}
	if !strings.Contains(result, `"correct_approach":`) {
		t.Error("Reflector 结果缺少 JSON 示例的 correct_approach 字段")
	}
	if !strings.Contains(result, `"key_insight":`) {
		t.Error("Reflector 结果缺少 JSON 示例的 key_insight 字段")
	}
}

// TestCuratorPromptBacktickEscaping 验证反引号转义正确
func TestCuratorPromptBacktickEscaping(t *testing.T) {
	data := curatorPromptData{
		QuestionContext: "ctx",
		Playbook:       "pb",
		Trajectory:     "tr",
		Reflection:     "ref",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.Curator.Execute(&buf, data); err != nil {
		t.Fatalf("Curator 执行失败: %v", err)
	}
	result := buf.String()
	// 验证 page_index 被反引号包围
	if !strings.Contains(result, "`page_index`") {
		t.Error("Curator 结果缺少反引号包围的 page_index")
	}
}

// TestCuratorScalingPromptBacktickEscaping 验证反引号转义正确
func TestCuratorScalingPromptBacktickEscaping(t *testing.T) {
	data := curatorScalingPromptData{
		QuestionContext: "ctx",
		Playbook:       "pb",
		Trajectories:   "trajs",
		Reflection:     "ref",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.CuratorScaling.Execute(&buf, data); err != nil {
		t.Fatalf("CuratorScaling 执行失败: %v", err)
	}
	result := buf.String()
	if !strings.Contains(result, "`page_index`") {
		t.Error("CuratorScaling 结果缺少反引号包围的 page_index")
	}
}

// TestCuratorPromptMetadataEscaping 验证 metadata 示例中花括号转义
func TestCuratorPromptMetadataEscaping(t *testing.T) {
	data := curatorPromptData{
		QuestionContext: "ctx",
		Playbook:       "pb",
		Trajectory:     "tr",
		Reflection:     "ref",
	}
	var buf bytes.Buffer
	if err := defaultACEPrompt.Curator.Execute(&buf, data); err != nil {
		t.Fatalf("Curator 执行失败: %v", err)
	}
	result := buf.String()
	// 验证 metadata 示例中的花括号正确渲染
	if !strings.Contains(result, `"helpful": 1`) {
		t.Error("Curator 结果缺少 metadata helpful 示例")
	}
	if !strings.Contains(result, `"harmful": 0`) {
		t.Error("Curator 结果缺少 metadata harmful 示例")
	}
}
