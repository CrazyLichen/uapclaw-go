package rsi

// models.go 单测：枚举值逐字、序列化往返、投影字段、失败原因择取。

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTaskStatus_值逐字对齐 Python models.py TaskStatus 成员集合必须完整。
func TestTaskStatus_值逐字对齐(t *testing.T) {
	if TaskStatusCreated != "CREATED" || TaskStatusQueued != "QUEUED" ||
		TaskStatusRunning != "RUNNING" || TaskStatusPaused != "PAUSED" ||
		TaskStatusCompleted != "COMPLETED" || TaskStatusFailed != "FAILED" ||
		TaskStatusTerminated != "TERMINATED" {
		t.Fatalf("TaskStatus 枚举漂移")
	}
	for _, s := range TerminalSet() {
		_ = s
	}
	if len(TerminalSet()) != 3 {
		t.Fatalf("终态集合漂移")
	}
}

// TestScenario_ArtifactType枚举成员完整 成员完整保留（规则 9.2.4）。
func TestScenario_ArtifactType枚举成员完整(t *testing.T) {
	if ScenarioHarness != "HARNESS" || ScenarioArtifact != "ARTIFACT" {
		t.Fatalf("Scenario 漂移")
	}
	if ArtifactTypePaper != "PAPER" || ArtifactTypeProgram != "PROGRAM" {
		t.Fatalf("ArtifactType 漂移")
	}
	if ArtifactKindHarnessPlugin != "harness_plugin" || ArtifactKindArtifactPackage != "artifact_package" {
		t.Fatalf("ArtifactKind 漂移")
	}
}

// TestRsiTask_JSON往返 序列化剔除 search_width、旧文件读入恢复。
func TestRsiTask_JSON往返(t *testing.T) {
	sw := "HARNESS"
	task := RsiTask{
		TaskID: "rsi-abc", Name: "任务", Scenario: sw,
		Status: string(TaskStatusCreated), CreatedAt: UtcnowISO(),
		ModelRefs: map[string]any{}, MaxIterations: 3, SearchWidth: 2,
		Config: map[string]any{}, StatusHistory: []map[string]any{},
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	// 新序列化不落盘 search_width（Python to_dict pop）。
	if strings.Contains(string(b), "search_width") || strings.Contains(string(b), "SearchWidth") {
		t.Fatalf("search_width 泄漏进新快照: %s", b)
	}
	var back RsiTask
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Status != string(TaskStatusCreated) || back.TaskID != "rsi-abc" || back.MaxIterations != 3 {
		t.Fatalf("往返漂移: %+v", back)
	}
	if back.SearchWidth != 0 {
		t.Fatalf("新快照不应恢复 search_width")
	}
	// 旧文件含 search_width 仍可读入（Python from_dict known 集合）。
	old := `{"task_id":"rsi-old","name":"旧","scenario":"HARNESS","status":"CREATED","created_at":"2026-01-01T00:00:00+00:00","search_width":5}`
	var legacy RsiTask
	if err := json.Unmarshal([]byte(old), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.SearchWidth != 5 {
		t.Fatalf("旧文件 search_width 丢失")
	}
}

// TestGetProjection_失败原因注入 web §6.3 字段与 failure_reason 注入。
func TestGetProjection_失败原因注入(t *testing.T) {
	task := RsiTask{
		TaskID: "rsi-x", Name: "n", Scenario: "HARNESS", Status: string(TaskStatusFailed),
		CreatedAt: UtcnowISO(), ModelRefs: map[string]any{}, Config: map[string]any{},
		StatusHistory: []map[string]any{{"to": "FAILED", "cause": "引擎崩溃"}},
	}
	payload := task.GetProjection(nil, nil, nil)
	if payload["failure_reason"] != "引擎崩溃" {
		t.Fatalf("failure_reason 缺失: %v", payload)
	}
	cfg := payload["config"].(map[string]any)
	if _, ok := cfg["web_proxy_configured"]; !ok {
		t.Fatalf("web_proxy_configured 缺失")
	}
}

// TestFailureReasonText_结果原因优先 对齐 Python 结果原因压过泛化历史原因。
func TestFailureReasonText_结果原因优先(t *testing.T) {
	task := RsiTask{
		Status: string(TaskStatusFailed), Config: map[string]any{},
		StatusHistory: []map[string]any{{"to": "FAILED", "cause": "provider.failed"}},
	}
	task.Config["results"] = map[string]any{"error_message": "数据集第 3 行非法"}
	if got := task.FailureReasonText(); got != "数据集第 3 行非法" {
		t.Fatalf("got %q", got)
	}
	// 非泛化历史原因保留。
	task.StatusHistory = []map[string]any{{"to": "FAILED", "cause": "引擎崩溃"}}
	if got := task.FailureReasonText(); got != "引擎崩溃" {
		t.Fatalf("got %q", got)
	}
	// 未失败返回空。
	task.Status = string(TaskStatusRunning)
	if got := task.FailureReasonText(); got != "" {
		t.Fatalf("got %q", got)
	}
}

// TestGenerateTaskID 前缀 rsi- + 8 位 hex。
func TestGenerateTaskID(t *testing.T) {
	id := GenerateTaskID()
	if !strings.HasPrefix(id, "rsi-") || len(id) != len("rsi-")+8 {
		t.Fatalf("got %q", id)
	}
}

// TestListProjection 六字段投影（web §6.2）。
func TestListProjection(t *testing.T) {
	task := RsiTask{TaskID: "rsi-1", Name: "n", Scenario: "HARNESS", Status: "CREATED", CreatedAt: "t"}
	p := task.ListProjection()
	for _, k := range []string{"task_id", "name", "scenario", "artifact_type", "status", "created_at"} {
		if _, ok := p[k]; !ok {
			t.Fatalf("缺字段 %s", k)
		}
	}
	if len(p) != 6 {
		t.Fatalf("投影字段数漂移: %d", len(p))
	}
}

// TestUsageMerge 对齐 Python Usage.merge。
func TestUsageMerge(t *testing.T) {
	u := Usage{Tokens: Tokens{Input: 1, Output: 2, CacheHit: 3}, CostEstimate: 1.5, CallCount: 1}
	u.Merge(Usage{Tokens: Tokens{Input: 10, Output: 20, CacheHit: 30}, CostEstimate: 2.5, CallCount: 2})
	if u.Tokens.Input != 11 || u.Tokens.Output != 22 || u.Tokens.CacheHit != 33 || u.CallCount != 3 {
		t.Fatalf("merge 漂移: %+v", u)
	}
}

// TestUtcnowISO 秒级 + UTC。
func TestUtcnowISO(t *testing.T) {
	s := UtcnowISO()
	if !strings.HasSuffix(s, "+00:00") || len(s) != 25 {
		t.Fatalf("got %q", s)
	}
}
