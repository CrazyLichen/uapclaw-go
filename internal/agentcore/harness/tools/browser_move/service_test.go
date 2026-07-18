package browser_move

import (
	"context"
	"testing"

	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// newTestBrowserService 创建测试用 BrowserService 实例
func newTestBrowserService() *BrowserService {
	return NewBrowserService(
		"openai",
		"test-key",
		"https://api.openai.com/v1",
		"gpt-4",
		&mcptypes.McpServerConfig{ServerName: "test", ServerPath: "stdio://test", ClientType: "stdio"},
		&BrowserRunGuardrails{
			MaxSteps:              10,
			MaxFailures:           3,
			TimeoutS:              60,
			RetryOnce:             true,
			ResumeOnMaxIterations: true,
		},
		kv.NewInMemoryKVStore(),
	)
}

// ──────────────────────────── NewBrowserService ────────────────────────────

// TestNewBrowserService 测试 BrowserService 构造
func TestNewBrowserService(t *testing.T) {
	svc := newTestBrowserService()
	if svc.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", svc.Provider, "openai")
	}
	if svc.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", svc.APIKey, "test-key")
	}
	if svc.APIBase != "https://api.openai.com/v1" {
		t.Errorf("APIBase = %q, want %q", svc.APIBase, "https://api.openai.com/v1")
	}
	if svc.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", svc.ModelName, "gpt-4")
	}
	if svc.Guardrails.MaxSteps != 10 {
		t.Errorf("MaxSteps = %d, want %d", svc.Guardrails.MaxSteps, 10)
	}
	if !svc.Guardrails.RetryOnce {
		t.Error("RetryOnce = false, want true")
	}
	if !svc.Guardrails.ResumeOnMaxIterations {
		t.Error("ResumeOnMaxIterations = false, want true")
	}
	if svc.started {
		t.Error("started = true, want false")
	}
	if svc.cancelStore == nil {
		t.Error("cancelStore is nil")
	}
}

// TestNewBrowserService_NilCancelStore 测试 nil cancelStore 自动创建 InMemoryKVStore
func TestNewBrowserService_NilCancelStore(t *testing.T) {
	svc := NewBrowserService(
		"openai", "key", "base", "model",
		&mcptypes.McpServerConfig{ServerName: "test"},
		&BrowserRunGuardrails{},
		nil,
	)
	if svc.cancelStore == nil {
		t.Error("cancelStore should be auto-created when nil")
	}
}

// ──────────────────────────── SessionNew ────────────────────────────

// TestSessionNew 自动生成会话标识
func TestSessionNew(t *testing.T) {
	svc := newTestBrowserService()
	sid := svc.SessionNew("")
	if sid == "" {
		t.Error("SessionNew('') should return non-empty session ID")
	}
	if len(sid) < 8 {
		t.Errorf("SessionNew('') returned too short ID: %q", sid)
	}
}

// TestSessionNew_指定会话标识
func TestSessionNew_指定会话标识(t *testing.T) {
	svc := newTestBrowserService()
	sid := svc.SessionNew("my-session")
	if sid != "my-session" {
		t.Errorf("SessionNew('my-session') = %q, want %q", sid, "my-session")
	}
}

// TestSessionNew_空白字符串自动生成
func TestSessionNew_空白字符串自动生成(t *testing.T) {
	svc := newTestBrowserService()
	sid := svc.SessionNew("   ")
	if sid == "" {
		t.Error("SessionNew with whitespace should auto-generate ID")
	}
}

// TestSessionNew_重复注册幂等
func TestSessionNew_重复注册幂等(t *testing.T) {
	svc := newTestBrowserService()
	sid1 := svc.SessionNew("dup-session")
	sid2 := svc.SessionNew("dup-session")
	if sid1 != sid2 {
		t.Errorf("Duplicate session registration should be idempotent, got %q and %q", sid1, sid2)
	}
}

// ──────────────────────────── RequestCancel / ClearCancel / IsCancelled ────────────────────────────

// TestRequestCancel_IsCancelled 测试取消请求和检查
func TestRequestCancel_IsCancelled(t *testing.T) {
	svc := newTestBrowserService()
	ctx := context.Background()

	cancelled, err := svc.IsCancelled(ctx, "sess1", "req1")
	if err != nil {
		t.Fatalf("IsCancelled error: %v", err)
	}
	if cancelled {
		t.Error("Should not be cancelled initially")
	}

	err = svc.RequestCancel(ctx, "sess1", "req1")
	if err != nil {
		t.Fatalf("RequestCancel error: %v", err)
	}

	cancelled, err = svc.IsCancelled(ctx, "sess1", "req1")
	if err != nil {
		t.Fatalf("IsCancelled error: %v", err)
	}
	if !cancelled {
		t.Error("Should be cancelled after RequestCancel")
	}
}

// TestRequestCancel_空会话ID报错
func TestRequestCancel_空会话ID报错(t *testing.T) {
	svc := newTestBrowserService()
	ctx := context.Background()
	err := svc.RequestCancel(ctx, "", "req1")
	if err == nil {
		t.Error("RequestCancel with empty session_id should return error")
	}
}

// TestClearCancel 清除取消标记
func TestClearCancel(t *testing.T) {
	svc := newTestBrowserService()
	ctx := context.Background()

	_ = svc.RequestCancel(ctx, "sess1", "req1")
	cancelled, _ := svc.IsCancelled(ctx, "sess1", "req1")
	if !cancelled {
		t.Fatal("Should be cancelled")
	}

	_ = svc.ClearCancel(ctx, "sess1", "req1")
	cancelled, _ = svc.IsCancelled(ctx, "sess1", "req1")
	if cancelled {
		t.Error("Should not be cancelled after ClearCancel")
	}
}

// TestClearCancel_通配符清除删除通配符标记
func TestClearCancel_通配符清除删除通配符标记(t *testing.T) {
	svc := newTestBrowserService()
	ctx := context.Background()

	// 设置通配符取消
	_ = svc.RequestCancel(ctx, "sess1", "")

	// 清除通配符
	_ = svc.ClearCancel(ctx, "sess1", "")

	// 通配符应被清除
	cancelled, _ := svc.IsCancelled(ctx, "sess1", "any-request")
	if cancelled {
		t.Error("Wildcard cancellation should be cleared")
	}
}

// TestIsCancelled_通配符匹配
func TestIsCancelled_通配符匹配(t *testing.T) {
	svc := newTestBrowserService()
	ctx := context.Background()

	// 设置通配符取消
	_ = svc.RequestCancel(ctx, "sess1", "")

	// 任意 requestID 都应被取消
	cancelled, err := svc.IsCancelled(ctx, "sess1", "any-request")
	if err != nil {
		t.Fatalf("IsCancelled error: %v", err)
	}
	if !cancelled {
		t.Error("Wildcard cancellation should match any request_id")
	}
}

// TestIsCancelled_空会话ID返回false
func TestIsCancelled_空会话ID返回false(t *testing.T) {
	svc := newTestBrowserService()
	ctx := context.Background()
	cancelled, err := svc.IsCancelled(ctx, "", "req1")
	if err != nil {
		t.Fatalf("IsCancelled error: %v", err)
	}
	if cancelled {
		t.Error("Empty session_id should return false")
	}
}

// ──────────────────────────── RecordToolProgress ────────────────────────────

// TestRecordToolProgress 测试工具进度记录
func TestRecordToolProgress(t *testing.T) {
	svc := newTestBrowserService()
	svc.SessionNew("sess1")

	svc.RecordToolProgress("sess1", "req1", "browser_click", map[string]any{
		"ok":   true,
		"page": map[string]any{"url": "https://example.com", "title": "Example"},
	})

	state := svc.GetProgressState("sess1")
	if state == nil {
		t.Fatal("progress state should not be nil after recording")
	}
	if state.RequestID != "req1" {
		t.Errorf("RequestID = %q, want %q", state.RequestID, "req1")
	}
	if state.LastPageURL != "https://example.com" {
		t.Errorf("LastPageURL = %q, want %q", state.LastPageURL, "https://example.com")
	}
	if state.LastPageTitle != "Example" {
		t.Errorf("LastPageTitle = %q, want %q", state.LastPageTitle, "Example")
	}
	if len(state.RecentToolSteps) == 0 {
		t.Error("RecentToolSteps should not be empty")
	}
	if state.Status != "partial" {
		t.Errorf("Status = %q, want %q", state.Status, "partial")
	}
}

// TestRecordToolProgress_空会话ID忽略
func TestRecordToolProgress_空会话ID忽略(t *testing.T) {
	svc := newTestBrowserService()
	svc.RecordToolProgress("", "req1", "click", nil)
	state := svc.GetProgressState("")
	if state != nil {
		t.Error("Empty session_id should be ignored")
	}
}

// ──────────────────────────── RecordWorkerProgress ────────────────────────────

// TestRecordWorkerProgress 测试 Worker 进度记录
func TestRecordWorkerProgress(t *testing.T) {
	svc := newTestBrowserService()
	svc.SessionNew("sess1")

	svc.RecordWorkerProgress("sess1", "req1", map[string]any{
		"ok":     true,
		"status": "completed",
		"final":  "Task completed successfully",
		"page":   map[string]any{"url": "https://result.com", "title": "Result"},
		"progress": map[string]any{
			"completed_steps":     []any{"Step 1", "Step 2"},
			"remaining_steps":     []any{"Step 3"},
			"completion_evidence": []any{"Found the target element"},
		},
	})

	state := svc.GetProgressState("sess1")
	if state == nil {
		t.Fatal("progress state should not be nil")
	}
	if state.Status != "completed" {
		t.Errorf("Status = %q, want %q", state.Status, "completed")
	}
	if len(state.CompletedSteps) != 2 {
		t.Errorf("CompletedSteps length = %d, want %d", len(state.CompletedSteps), 2)
	}
	if len(state.CompletionEvidence) != 1 {
		t.Errorf("CompletionEvidence length = %d, want %d", len(state.CompletionEvidence), 1)
	}
	if state.LastPageURL != "https://result.com" {
		t.Errorf("LastPageURL = %q, want %q", state.LastPageURL, "https://result.com")
	}
}

// TestRecordWorkerProgress_无status字段推断
func TestRecordWorkerProgress_无status字段推断(t *testing.T) {
	svc := newTestBrowserService()
	svc.SessionNew("sess2")

	// ok=true → completed
	svc.RecordWorkerProgress("sess2", "req1", map[string]any{
		"ok": true,
	})
	state := svc.GetProgressState("sess2")
	if state.Status != "completed" {
		t.Errorf("Status = %q, want %q (inferred from ok=true)", state.Status, "completed")
	}

	// error非空 → failed
	svc.ClearProgressState("sess2")
	svc.RecordWorkerProgress("sess2", "req2", map[string]any{
		"error": "something went wrong",
	})
	state = svc.GetProgressState("sess2")
	if state.Status != "failed" {
		t.Errorf("Status = %q, want %q (inferred from error)", state.Status, "failed")
	}
}

// ──────────────────────────── GetProgressState / SetProgressState / ClearProgressState ────────────────────────────

// TestGetProgressState_不存在返回nil
func TestGetProgressState_不存在返回nil(t *testing.T) {
	svc := newTestBrowserService()
	state := svc.GetProgressState("nonexistent")
	if state != nil {
		t.Error("GetProgressState for nonexistent session should return nil")
	}
}

// TestSetProgressState 设置和获取进度状态
func TestSetProgressState(t *testing.T) {
	svc := newTestBrowserService()
	state := &BrowserTaskProgressState{
		Status:              "partial",
		CompletedSteps:      []string{"Step 1"},
		RemainingSteps:      []string{},
		CompletionEvidence:  []string{},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{},
	}
	svc.SetProgressState("sess1", state)

	got := svc.GetProgressState("sess1")
	if got == nil {
		t.Fatal("GetProgressState should return the set state")
	}
	if got.Status != "partial" {
		t.Errorf("Status = %q, want %q", got.Status, "partial")
	}
	if len(got.CompletedSteps) != 1 || got.CompletedSteps[0] != "Step 1" {
		t.Errorf("CompletedSteps = %v, want [Step 1]", got.CompletedSteps)
	}
}

// TestSetProgressState_空状态则移除
func TestSetProgressState_空状态则移除(t *testing.T) {
	svc := newTestBrowserService()
	state := &BrowserTaskProgressState{
		Status:              "partial",
		CompletedSteps:      []string{"Step 1"},
		RemainingSteps:      []string{},
		CompletionEvidence:  []string{},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{},
	}
	svc.SetProgressState("sess1", state)

	emptyState := &BrowserTaskProgressState{
		Status:              "unknown",
		CompletedSteps:      []string{},
		RemainingSteps:      []string{},
		CompletionEvidence:  []string{},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{},
	}
	svc.SetProgressState("sess1", emptyState)

	got := svc.GetProgressState("sess1")
	if got != nil {
		t.Error("SetProgressState with empty state should remove the entry")
	}
}

// TestSetProgressState_空会话ID忽略
func TestSetProgressState_空会话ID忽略(t *testing.T) {
	svc := newTestBrowserService()
	state := &BrowserTaskProgressState{Status: "partial"}
	svc.SetProgressState("", state)
	got := svc.GetProgressState("")
	if got != nil {
		t.Error("Empty session_id should be ignored")
	}
}

// TestClearProgressState 清除进度状态
func TestClearProgressState(t *testing.T) {
	svc := newTestBrowserService()
	state := &BrowserTaskProgressState{
		Status:              "partial",
		CompletedSteps:      []string{"Step 1"},
		RemainingSteps:      []string{},
		CompletionEvidence:  []string{},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{},
	}
	svc.SetProgressState("sess1", state)
	svc.ClearProgressState("sess1")

	got := svc.GetProgressState("sess1")
	if got != nil {
		t.Error("ClearProgressState should remove the state")
	}
}

// ──────────────────────────── ExportProgressState ────────────────────────────

// TestExportProgressState 导出进度状态
func TestExportProgressState(t *testing.T) {
	svc := newTestBrowserService()
	state := &BrowserTaskProgressState{
		Status:              "completed",
		CompletedSteps:      []string{"Step 1", "Step 2"},
		RemainingSteps:      []string{},
		NextStep:            "",
		CompletionEvidence:  []string{"Evidence 1"},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{"click: ok"},
		LastPageURL:         "https://example.com",
		LastPageTitle:       "Example",
		LastScreenshot:      nil,
		LastWorkerFinal:     "Done",
	}
	svc.SetProgressState("sess1", state)

	exported := svc.ExportProgressState("sess1")
	if exported == nil {
		t.Fatal("ExportProgressState should return non-nil for non-empty state")
	}
	if exported["status"] != "completed" {
		t.Errorf("status = %v, want completed", exported["status"])
	}
	page, ok := exported["last_page"].(map[string]any)
	if !ok {
		t.Fatal("last_page should be a map")
	}
	if page["url"] != "https://example.com" {
		t.Errorf("last_page.url = %v, want https://example.com", page["url"])
	}
}

// TestExportProgressState_空状态返回nil
func TestExportProgressState_空状态返回nil(t *testing.T) {
	svc := newTestBrowserService()
	// 未设置任何状态
	exported := svc.ExportProgressState("nonexistent")
	if exported != nil {
		t.Error("ExportProgressState for nonexistent session should return nil")
	}
}

// TestExportProgressState_未知状态返回nil
func TestExportProgressState_未知状态返回nil(t *testing.T) {
	svc := newTestBrowserService()
	// 设置全空状态（status=unknown，所有列表为空）
	state := &BrowserTaskProgressState{
		Status:              "unknown",
		CompletedSteps:      []string{},
		RemainingSteps:      []string{},
		CompletionEvidence:  []string{},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{},
	}
	svc.SetProgressState("sess1", state)
	exported := svc.ExportProgressState("sess1")
	if exported != nil {
		t.Error("ExportProgressState for empty state should return nil")
	}
}

// ──────────────────────────── BuildProgressContext ────────────────────────────

// TestBuildProgressContext_空状态
func TestBuildProgressContext_空状态(t *testing.T) {
	result := BuildProgressContext(nil)
	if result != "" {
		t.Errorf("BuildProgressContext(nil) = %q, want empty", result)
	}
}

// TestBuildProgressContext_未知状态
func TestBuildProgressContext_未知状态(t *testing.T) {
	state := &BrowserTaskProgressState{
		Status:              "unknown",
		CompletedSteps:      []string{},
		RemainingSteps:      []string{},
		CompletionEvidence:  []string{},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{},
	}
	result := BuildProgressContext(state)
	if result != "" {
		t.Errorf("BuildProgressContext with unknown empty state = %q, want empty", result)
	}
}

// TestBuildProgressContext_有数据状态
func TestBuildProgressContext_有数据状态(t *testing.T) {
	state := &BrowserTaskProgressState{
		Status:              "partial",
		CompletedSteps:      []string{"Step 1", "Step 2"},
		RemainingSteps:      []string{"Step 3"},
		NextStep:            "Click submit button",
		CompletionEvidence:  []string{"Form filled"},
		MissingRequirements: []string{"Captcha not solved"},
		RecentToolSteps:     []string{"click: ok", "type: entered"},
	}
	result := BuildProgressContext(state)
	if result == "" {
		t.Error("BuildProgressContext with data should return non-empty")
	}
	if !contains(result, "Known progress for continuation:") {
		t.Error("Should contain header line")
	}
	if !contains(result, "Known progress status: partial") {
		t.Error("Should contain status line")
	}
	if !contains(result, "Completed steps:") {
		t.Error("Should contain completed steps")
	}
	if !contains(result, "Remaining steps:") {
		t.Error("Should contain remaining steps")
	}
	if !contains(result, "Next step to try:") {
		t.Error("Should contain next step")
	}
	if !contains(result, "Completion evidence observed:") {
		t.Error("Should contain completion evidence")
	}
	if !contains(result, "Missing requirements / blockers:") {
		t.Error("Should contain missing requirements")
	}
	if !contains(result, "Recent browser tool activity:") {
		t.Error("Should contain recent tool activity")
	}
}

// ──────────────────────────── BuildFailureSummary ────────────────────────────

// TestBuildFailureSummary 完整摘要
func TestBuildFailureSummary(t *testing.T) {
	svc := newTestBrowserService()
	progressState := &BrowserTaskProgressState{
		Status:              "partial",
		CompletedSteps:      []string{"Opened page"},
		CompletionEvidence:  []string{},
		MissingRequirements: []string{},
		RecentToolSteps:     []string{},
		RemainingSteps:      []string{},
	}
	summary := svc.BuildFailureSummary(
		"Search for cats",
		"element not found",
		"https://example.com",
		"Example",
		"Partial output",
		"screenshot_data",
		1,
		progressState,
	)
	if summary == "" {
		t.Fatal("BuildFailureSummary should return non-empty")
	}
	if !contains(summary, "Failure summary for continuation:") {
		t.Error("Should contain header")
	}
	if !contains(summary, "Original task:") {
		t.Error("Should contain original task")
	}
	if !contains(summary, "Search for cats") {
		t.Error("Should contain task text")
	}
	if !contains(summary, "element not found") {
		t.Error("Should contain error")
	}
	if !contains(summary, "https://example.com") {
		t.Error("Should contain page URL")
	}
	if !contains(summary, "Failed attempt: 1") {
		t.Error("Should contain attempt number")
	}
	if !contains(summary, "Partial output excerpt:") {
		t.Error("Should contain partial output")
	}
}

// TestBuildFailureSummary_空页面信息
func TestBuildFailureSummary_空页面信息(t *testing.T) {
	svc := newTestBrowserService()
	summary := svc.BuildFailureSummary(
		"Task",
		"error",
		"",
		"",
		"",
		nil,
		1,
		nil,
	)
	if contains(summary, "Last page:") {
		t.Error("Should not contain Last page when both URL and title are empty")
	}
}

// ──────────────────────────── ShouldTreatAsCompleted ────────────────────────────

// TestShouldTreatAsCompleted_已完成有证据
func TestShouldTreatAsCompleted_已完成有证据(t *testing.T) {
	parsed := map[string]any{
		"status":              "completed",
		"completion_evidence": []any{"Found the element"},
	}
	if !ShouldTreatAsCompleted(parsed) {
		t.Error("Should treat as completed when status=completed and evidence present")
	}
}

// TestShouldTreatAsCompleted_已完成有final文本
func TestShouldTreatAsCompleted_已完成有final文本(t *testing.T) {
	parsed := map[string]any{
		"status": "completed",
		"final":  "Task is done",
	}
	if !ShouldTreatAsCompleted(parsed) {
		t.Error("Should treat as completed when status=completed and final text present")
	}
}

// TestShouldTreatAsCompleted_有缺失需求
func TestShouldTreatAsCompleted_有缺失需求(t *testing.T) {
	parsed := map[string]any{
		"status":               "completed",
		"completion_evidence":  []any{"Found the element"},
		"missing_requirements": []any{"Captcha not solved"},
	}
	if ShouldTreatAsCompleted(parsed) {
		t.Error("Should not treat as completed when missing requirements present")
	}
}

// TestShouldTreatAsCompleted_非完成状态
func TestShouldTreatAsCompleted_非完成状态(t *testing.T) {
	parsed := map[string]any{
		"status":              "partial",
		"completion_evidence": []any{"Something"},
	}
	if ShouldTreatAsCompleted(parsed) {
		t.Error("Should not treat as completed when status is not completed")
	}
}

// TestShouldTreatAsCompleted_无证据无final
func TestShouldTreatAsCompleted_无证据无final(t *testing.T) {
	parsed := map[string]any{
		"status": "completed",
	}
	if ShouldTreatAsCompleted(parsed) {
		t.Error("Should not treat as completed when no evidence and no final text")
	}
}

// TestShouldTreatAsCompleted_task_status字段
func TestShouldTreatAsCompleted_task_status字段(t *testing.T) {
	parsed := map[string]any{
		"task_status":         "completed",
		"completion_evidence": []any{"Done"},
	}
	if !ShouldTreatAsCompleted(parsed) {
		t.Error("Should treat as completed when task_status=completed")
	}
}

// ──────────────────────────── NormalizeProgressStatus ────────────────────────────

// TestNormalizeProgressStatus 规范化进度状态
func TestNormalizeProgressStatus(t *testing.T) {
	tests := []struct {
		input  any
		expect string
	}{
		{"complete", "completed"},
		{"completed", "completed"},
		{"done", "completed"},
		{"Complete", "completed"},
		{"COMPLETED", "completed"},
		{"Done", "completed"},
		{"partial", "partial"},
		{"in_progress", "partial"},
		{"in-progress", "partial"},
		{"blocked", "blocked"},
		{"failed", "failed"},
		{"unknown_status", ""},
		{"", ""},
		{nil, ""},
		{123, ""},
	}
	for _, tt := range tests {
		got := NormalizeProgressStatus(tt.input)
		if got != tt.expect {
			t.Errorf("NormalizeProgressStatus(%v) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

// ──────────────────────────── IsRetryableTransportMessage ────────────────────────────

// TestIsRetryableTransportMessage 可重试传输错误判断
func TestIsRetryableTransportMessage(t *testing.T) {
	tests := []struct {
		input  string
		expect bool
	}{
		{"Session terminated unexpectedly", true},
		{"not connected to server", true},
		{"EndOfStream error", true},
		{"ClosedResourceError: connection lost", true},
		{"BrokenResourceError in stream", true},
		{"stream closed by remote", true},
		{"connection closed unexpectedly", true},
		{"broken pipe error", true},
		{"RemoteProtocolError: invalid", true},
		{"ReadError from socket", true},
		{"WriteError on socket", true},
		{"normal error message", false},
		{"invalid argument", false},
		{"", false},
	}
	for _, tt := range tests {
		got := IsRetryableTransportMessage(tt.input)
		if got != tt.expect {
			t.Errorf("IsRetryableTransportMessage(%q) = %v, want %v", tt.input, got, tt.expect)
		}
	}
}

// ──────────────────────────── IsRetryableRuntimeResult ────────────────────────────

// TestIsRetryableRuntimeResult 可重试运行时结果判断
func TestIsRetryableRuntimeResult(t *testing.T) {
	tests := []struct {
		name   string
		parsed map[string]any
		expect bool
	}{
		{
			name:   "frame detached",
			parsed: map[string]any{"ok": false, "error": "frame has been detached"},
			expect: true,
		},
		{
			name:   "target closed",
			parsed: map[string]any{"ok": false, "final": "target closed"},
			expect: true,
		},
		{
			name:   "page crashed",
			parsed: map[string]any{"ok": false, "error": "page crashed"},
			expect: true,
		},
		{
			name:   "ok=true not retryable",
			parsed: map[string]any{"ok": true, "error": "frame has been detached"},
			expect: false,
		},
		{
			name:   "normal error not retryable",
			parsed: map[string]any{"ok": false, "error": "invalid selector"},
			expect: false,
		},
		{
			name:   "nil parsed",
			parsed: nil,
			expect: false,
		},
		{
			name:   "context closed",
			parsed: map[string]any{"ok": false, "error": "context closed"},
			expect: true,
		},
		{
			name:   "net::err_network_changed",
			parsed: map[string]any{"ok": false, "error": "net::err_network_changed"},
			expect: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryableRuntimeResult(tt.parsed)
			if got != tt.expect {
				t.Errorf("IsRetryableRuntimeResult() = %v, want %v", got, tt.expect)
			}
		})
	}
}

// ──────────────────────────── NormalizeScreenshotValue ────────────────────────────

// TestNormalizeScreenshotValue 规范化截图值
func TestNormalizeScreenshotValue(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		expect any
	}{
		{"nil returns nil", nil, nil},
		{"non-string passthrough", 123, 123},
		{"empty string returns nil", "", nil},
		{"http URL kept", "http://example.com/img.png", "http://example.com/img.png"},
		{"https URL kept", "https://example.com/img.png", "https://example.com/img.png"},
		{"data URL kept", "data:image/png;base64,abc123", "data:image/png;base64,abc123"},
		{"whitespace empty returns nil", "   ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeScreenshotValue(tt.input)
			if got != tt.expect {
				t.Errorf("NormalizeScreenshotValue(%v) = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}

// ──────────────────────────── Shutdown ────────────────────────────

// TestShutdown 关闭服务
func TestShutdown(t *testing.T) {
	svc := newTestBrowserService()
	svc.started = true
	svc.connectionHealthy = true
	ctx := context.Background()
	err := svc.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown error: %v", err)
	}
	if svc.started {
		t.Error("started should be false after shutdown")
	}
	if svc.connectionHealthy {
		t.Error("connectionHealthy should be false after shutdown")
	}
}

// ──────────────────────────── cancelKey ────────────────────────────

// TestCancelKey 取消键格式
func TestCancelKey(t *testing.T) {
	key := cancelKey("sess1", "req1")
	expected := "playwright_runtime:cancel:sess1:req1"
	if key != expected {
		t.Errorf("cancelKey('sess1', 'req1') = %q, want %q", key, expected)
	}

	keyWildcard := cancelKey("sess1", "")
	expectedWildcard := "playwright_runtime:cancel:sess1:*"
	if keyWildcard != expectedWildcard {
		t.Errorf("cancelKey('sess1', '') = %q, want %q", keyWildcard, expectedWildcard)
	}
}

// ──────────────────────────── cleanProgressItems ────────────────────────────

// TestCleanProgressItems 清理进度项
func TestCleanProgressItems(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		limit  int
		expect []string
	}{
		{"nil returns empty", nil, 4, []string{}},
		{"string slice", []string{"  a  ", "b", "c"}, 4, []string{"a", "b", "c"}},
		{"any slice", []any{"x", "  y  "}, 4, []string{"x", "y"}},
		{"dedup", []string{"a", "A", "b"}, 4, []string{"a", "b"}},
		{"limit", []string{"a", "b", "c", "d", "e"}, 3, []string{"a", "b", "c"}},
		{"empty strings filtered", []string{"", "  ", "a"}, 4, []string{"a"}},
		{"single value", "hello", 4, []string{"hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanProgressItems(tt.input, tt.limit)
			if len(got) != len(tt.expect) {
				t.Errorf("cleanProgressItems() = %v, want %v", got, tt.expect)
				return
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Errorf("cleanProgressItems()[%d] = %q, want %q", i, got[i], tt.expect[i])
				}
			}
		})
	}
}

// ──────────────────────────── buildResumeTask ────────────────────────────

// TestBuildResumeTask 构建续行任务
func TestBuildResumeTask(t *testing.T) {
	result := buildResumeTask("Search for cats", "Partial results found", "Progress: step1 done")
	if !contains(result, "Search for cats") {
		t.Error("Should contain original task")
	}
	if !contains(result, "Continuation context:") {
		t.Error("Should contain continuation context header")
	}
	if !contains(result, "max iterations before completion") {
		t.Error("Should mention max iterations")
	}
	if !contains(result, "Partial results found") {
		t.Error("Should contain previous partial output")
	}
	if !contains(result, "Progress: step1 done") {
		t.Error("Should contain progress context")
	}
}

// TestBuildResumeTask_截断长输出
func TestBuildResumeTask_截断长输出(t *testing.T) {
	longFinal := make([]byte, 1500)
	for i := range longFinal {
		longFinal[i] = 'a'
	}
	result := buildResumeTask("Task", string(longFinal), "")
	if !contains(result, "...[truncated]") {
		t.Error("Should truncate long previous final output")
	}
}

// ──────────────────────────── buildTaskWithFailureContext ────────────────────────────

// TestBuildTaskWithFailureContext 构建带失败上下文的任务
func TestBuildTaskWithFailureContext(t *testing.T) {
	result := buildTaskWithFailureContext("Original task", "Previous failure summary")
	if !contains(result, "Original task") {
		t.Error("Should contain original task")
	}
	if !contains(result, "Previous failed attempt context:") {
		t.Error("Should contain failure context header")
	}
	if !contains(result, "Previous failure summary") {
		t.Error("Should contain failure summary")
	}
	if !contains(result, "Continuation instructions:") {
		t.Error("Should contain continuation instructions")
	}
}

// TestBuildTaskWithFailureContext_空摘要
func TestBuildTaskWithFailureContext_空摘要(t *testing.T) {
	result := buildTaskWithFailureContext("Task", "")
	if result != "Task" {
		t.Errorf("Empty summary should return base task, got %q", result)
	}
}

// ──────────────────────────── isMaxIterationResult ────────────────────────────

// TestIsMaxIterationResult 最大迭代次数判断
func TestIsMaxIterationResult(t *testing.T) {
	tests := []struct {
		name   string
		parsed map[string]any
		expect bool
	}{
		{"error max_iterations_reached", map[string]any{"error": "max_iterations_reached"}, true},
		{"final contains marker", map[string]any{"final": MaxIterationMessage}, true},
		{"normal error", map[string]any{"error": "some other error"}, false},
		{"nil parsed", nil, false},
		{"ok result", map[string]any{"ok": true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMaxIterationResult(tt.parsed)
			if got != tt.expect {
				t.Errorf("isMaxIterationResult() = %v, want %v", got, tt.expect)
			}
		})
	}
}

// ──────────────────────────── trimText ────────────────────────────

// TestTrimText 文本截断
func TestTrimText(t *testing.T) {
	result := trimText("hello", 10)
	if result != "hello" {
		t.Errorf("trimText('hello', 10) = %q, want %q", result, "hello")
	}

	longStr := make([]byte, 300)
	for i := range longStr {
		longStr[i] = 'a'
	}
	result = trimText(string(longStr), 100)
	if !contains(result, "...[truncated]") {
		t.Error("Should truncate and add suffix")
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
