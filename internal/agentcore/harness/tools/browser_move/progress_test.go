package browser_move

import (
	"testing"
)

// TestNewBrowserTaskProgressStateFromDict_正常数据 测试从包含 last_page 子对象的完整字典构造
func TestNewBrowserTaskProgressStateFromDict_正常数据(t *testing.T) {
	data := map[string]any{
		"request_id":   "req-001",
		"status":       "in_progress",
		"completed_steps":    []any{"step1", "step2"},
		"remaining_steps":    []any{"step3"},
		"next_step":          "step3",
		"completion_evidence": []any{"ev1"},
		"missing_requirements": []any{"req1"},
		"recent_tool_steps":  []any{"tool1", "tool2"},
		"last_page": map[string]any{
			"url":   "https://example.com",
			"title": "Example",
		},
		"last_screenshot":   "base64data",
		"last_worker_final": "result text",
	}

	state := NewBrowserTaskProgressStateFromDict(data)

	if state.RequestID != "req-001" {
		t.Errorf("RequestID = %q, want %q", state.RequestID, "req-001")
	}
	if state.Status != "in_progress" {
		t.Errorf("Status = %q, want %q", state.Status, "in_progress")
	}
	if len(state.CompletedSteps) != 2 || state.CompletedSteps[0] != "step1" {
		t.Errorf("CompletedSteps = %v, want [step1 step2]", state.CompletedSteps)
	}
	if len(state.RemainingSteps) != 1 || state.RemainingSteps[0] != "step3" {
		t.Errorf("RemainingSteps = %v, want [step3]", state.RemainingSteps)
	}
	if state.NextStep != "step3" {
		t.Errorf("NextStep = %q, want %q", state.NextStep, "step3")
	}
	if len(state.CompletionEvidence) != 1 || state.CompletionEvidence[0] != "ev1" {
		t.Errorf("CompletionEvidence = %v, want [ev1]", state.CompletionEvidence)
	}
	if len(state.MissingRequirements) != 1 || state.MissingRequirements[0] != "req1" {
		t.Errorf("MissingRequirements = %v, want [req1]", state.MissingRequirements)
	}
	if len(state.RecentToolSteps) != 2 {
		t.Errorf("RecentToolSteps = %v, want 2 items", state.RecentToolSteps)
	}
	if state.LastPageURL != "https://example.com" {
		t.Errorf("LastPageURL = %q, want %q", state.LastPageURL, "https://example.com")
	}
	if state.LastPageTitle != "Example" {
		t.Errorf("LastPageTitle = %q, want %q", state.LastPageTitle, "Example")
	}
	if state.LastScreenshot != "base64data" {
		t.Errorf("LastScreenshot = %v, want base64data", state.LastScreenshot)
	}
	if state.LastWorkerFinal != "result text" {
		t.Errorf("LastWorkerFinal = %q, want %q", state.LastWorkerFinal, "result text")
	}
}

// TestNewBrowserTaskProgressStateFromDict_空数据 测试空字典输入，所有字段应为零值
func TestNewBrowserTaskProgressStateFromDict_空数据(t *testing.T) {
	data := map[string]any{}
	state := NewBrowserTaskProgressStateFromDict(data)

	if state.Status != "unknown" {
		t.Errorf("Status = %q, want %q", state.Status, "unknown")
	}
	if state.RequestID != "" {
		t.Errorf("RequestID = %q, want empty", state.RequestID)
	}
	if len(state.CompletedSteps) != 0 {
		t.Errorf("CompletedSteps = %v, want empty", state.CompletedSteps)
	}
	if len(state.RemainingSteps) != 0 {
		t.Errorf("RemainingSteps = %v, want empty", state.RemainingSteps)
	}
	if state.NextStep != "" {
		t.Errorf("NextStep = %q, want empty", state.NextStep)
	}
	if len(state.CompletionEvidence) != 0 {
		t.Errorf("CompletionEvidence = %v, want empty", state.CompletionEvidence)
	}
	if len(state.MissingRequirements) != 0 {
		t.Errorf("MissingRequirements = %v, want empty", state.MissingRequirements)
	}
	if len(state.RecentToolSteps) != 0 {
		t.Errorf("RecentToolSteps = %v, want empty", state.RecentToolSteps)
	}
	if state.LastPageURL != "" {
		t.Errorf("LastPageURL = %q, want empty", state.LastPageURL)
	}
	if state.LastPageTitle != "" {
		t.Errorf("LastPageTitle = %q, want empty", state.LastPageTitle)
	}
	if state.LastScreenshot != nil {
		t.Errorf("LastScreenshot = %v, want nil", state.LastScreenshot)
	}
	if state.LastWorkerFinal != "" {
		t.Errorf("LastWorkerFinal = %q, want empty", state.LastWorkerFinal)
	}
}

// TestNewBrowserTaskProgressStateFromDict_nil 测试 nil 输入
func TestNewBrowserTaskProgressStateFromDict_nil(t *testing.T) {
	state := NewBrowserTaskProgressStateFromDict(nil)

	if state.Status != "unknown" {
		t.Errorf("Status = %q, want %q", state.Status, "unknown")
	}
	if len(state.CompletedSteps) != 0 {
		t.Errorf("CompletedSteps = %v, want empty", state.CompletedSteps)
	}
	if state.LastScreenshot != nil {
		t.Errorf("LastScreenshot = %v, want nil", state.LastScreenshot)
	}
}

// TestBrowserTaskProgressState_IsEmpty_初始状态 测试零值状态为空
func TestBrowserTaskProgressState_IsEmpty_初始状态(t *testing.T) {
	state := NewBrowserTaskProgressStateFromDict(nil)
	if !state.IsEmpty() {
		t.Error("初始状态 IsEmpty() 应为 true")
	}
}

// TestBrowserTaskProgressState_IsEmpty_有完成步骤 测试有 completed_steps 时非空
func TestBrowserTaskProgressState_IsEmpty_有完成步骤(t *testing.T) {
	state := &BrowserTaskProgressState{
		Status:          "unknown",
		CompletedSteps:  []string{"step1"},
		RemainingSteps:  []string{},
		CompletionEvidence: []string{},
		MissingRequirements: []string{},
		RecentToolSteps: []string{},
	}
	if state.IsEmpty() {
		t.Error("有 completed_steps 时 IsEmpty() 应为 false")
	}
}

// TestBrowserTaskProgressState_ToDict_往返一致性 测试 ToDict 输出可通过 FromDict 还原
func TestBrowserTaskProgressState_ToDict_往返一致性(t *testing.T) {
	original := &BrowserTaskProgressState{
		RequestID:           "req-002",
		Status:              "completed",
		CompletedSteps:      []string{"s1", "s2"},
		RemainingSteps:      []string{},
		NextStep:            "",
		CompletionEvidence:  []string{"done"},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{"t1"},
		LastPageURL:         "https://test.com",
		LastPageTitle:       "Test",
		LastScreenshot:      nil,
		LastWorkerFinal:     "",
	}

	dict := original.ToDict()
	restored := NewBrowserTaskProgressStateFromDict(dict)

	if restored.Status != original.Status {
		t.Errorf("Status: got %q, want %q", restored.Status, original.Status)
	}
	if restored.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", restored.RequestID, original.RequestID)
	}
	if len(restored.CompletedSteps) != len(original.CompletedSteps) {
		t.Errorf("CompletedSteps: got %v, want %v", restored.CompletedSteps, original.CompletedSteps)
	}
	if restored.LastPageURL != original.LastPageURL {
		t.Errorf("LastPageURL: got %q, want %q", restored.LastPageURL, original.LastPageURL)
	}
	if restored.LastPageTitle != original.LastPageTitle {
		t.Errorf("LastPageTitle: got %q, want %q", restored.LastPageTitle, original.LastPageTitle)
	}
	if len(restored.RemainingSteps) != 0 {
		t.Errorf("RemainingSteps: got %v, want empty", restored.RemainingSteps)
	}

	// ToDict 中空字符串字段输出 nil
	if dict["next_step"] != nil {
		t.Errorf("next_step 应为 nil，got %v", dict["next_step"])
	}
	if dict["last_worker_final"] != nil {
		t.Errorf("last_worker_final 应为 nil，got %v", dict["last_worker_final"])
	}
	if dict["request_id"] != "req-002" {
		t.Errorf("request_id 应为 req-002，got %v", dict["request_id"])
	}

	// last_page 子对象
	lp, ok := dict["last_page"].(map[string]any)
	if !ok {
		t.Fatal("last_page 应为 map[string]any")
	}
	if lp["url"] != "https://test.com" {
		t.Errorf("last_page.url = %q, want %q", lp["url"], "https://test.com")
	}
	if lp["title"] != "Test" {
		t.Errorf("last_page.title = %q, want %q", lp["title"], "Test")
	}
}
