package recommendation

// topic_pipeline_d1_test.go 对齐 topic_pipeline.py:4481-4622 与
// session_history.py:286-301 契约的单元用例。

import (
	"context"
	"strings"
	"testing"
)

// pendingRecord 一条 request_pending 记录。
func pendingRecord(requestID string) map[string]any {
	return map[string]any{"role": "request_pending", "request_id": requestID,
		"content": "问", "id": "evt-p"}
}

// TestProjectCompletedSessionRecords 对齐 session_history.py:286-301。
func TestProjectCompletedSessionRecords(t *testing.T) {
	records := []map[string]any{
		completedRecord("r1", "user", "", "问", 0),
		completedRecord("r1", "assistant", "chat.final", "答", 1),
		{"request_id": "r1", "role": "assistant", "event_type": "chat.request_completed",
			"id": "r1:request_completed", "status": "success", "timestamp": 10.0,
			"feedback_only": true},
		completedRecord("r2", "user", "", "未完结", 3),
		completedRecord("r2", "assistant", "chat.final", "半截", 4),
	}
	projected := ProjectCompletedSessionRecords("s1", records, true)
	if len(projected) != 5 {
		t.Fatalf("count: %d", len(projected))
	}
	if projected[0]["role"] != "user" {
		t.Fatalf("completed user kept: %v", projected[0]["role"])
	}
	if projected[3]["role"] != "request_pending" {
		t.Fatalf("incomplete user hidden: %v", projected[3]["role"])
	}
	if projected[4]["role"] != "assistant" {
		t.Fatalf("assistant rows untouched: %v", projected[4]["role"])
	}
}

// TestNewTopicPipeline_LoadCompletedRecords 对齐 :4543-4553。
func TestNewTopicPipeline_LoadCompletedRecords(t *testing.T) {
	store := newTestStore(t)
	pipeline := NewTopicPipeline(store, nil,
		func() map[string][]map[string]any {
			return map[string][]map[string]any{"s1": {
				completedRecord("r1", "user", "", "问", 0),
				completedRecord("r2", "user", "", "未完结", 1),
			}}
		}, nil, nil, "", nil, nil, nil, nil, nil, nil)
	records := pipeline.CompletedRecordsLoader()
	if len(records) != 1 || len(records["s1"]) != 2 {
		t.Fatalf("records: %v", records)
	}
	if records["s1"][1]["role"] != "request_pending" {
		t.Fatalf("projection: %v", records["s1"][1]["role"])
	}
}

// TestTopicPipeline_SearchProviderID 对齐 :4555-4567 三级取值。
func TestTopicPipeline_SearchProviderID(t *testing.T) {
	store := newTestStore(t)
	// 显式 ID
	explicit := NewTopicPipeline(store, nil, nil, nil, nil, "ark-x", nil, nil, nil, nil, nil, nil)
	if explicit.SearchProviderID() != "ark-x" {
		t.Fatalf("explicit: %s", explicit.SearchProviderID())
	}
	// 注入搜索 → 审计厂商 ID
	injected := NewTopicPipeline(store, nil, nil, nil,
		func(ctx context.Context, query string, maxResults, timeoutSeconds int) (string, []map[string]string, error) {
			return "", nil, nil
		}, "", nil, nil, nil, nil, nil, nil)
	if injected.SearchProviderID() != INJECTEDSearchAuditProviderID {
		t.Fatalf("injected: %s", injected.SearchProviderID())
	}
	_ = strings.TrimSpace("")
}

// TestTopicPipeline_ScopeHasPendingRequest 对齐 :4577-4621。
func TestTopicPipeline_ScopeHasPendingRequest(t *testing.T) {
	store := newTestStore(t)
	records := func() map[string][]map[string]any {
		return map[string][]map[string]any{
			"s1": {pendingRecord("req-live"), completedRecord("r1", "assistant", "", "答", 1)},
			"s2": {pendingRecord("req-dead")},
		}
	}
	// 无活跃请求 → false
	none := NewTopicPipeline(store, nil, records, nil, nil, "", nil, nil, nil, nil,
		func() []string { return nil }, nil)
	if none.ScopeHasPendingRequest(GlobalNewChatScope) {
		t.Fatalf("no live requests")
	}
	// 活跃请求命中 s1 的尾部 pending
	live := NewTopicPipeline(store, nil, records, nil, nil, "", nil, nil, nil, nil,
		func() []string { return []string{"req-live"} }, nil)
	if !live.ScopeHasPendingRequest(GlobalNewChatScope) {
		t.Fatalf("live pending should match")
	}
	// 倒序扫描跳过非 user/human/pending 行：s1 尾部 assistant 被跳过，
	// 命中其 pending(req-live)（在 live）→ true
	deadOnly := NewTopicPipeline(store, nil, func() map[string][]map[string]any {
		return map[string][]map[string]any{
			"s1": {pendingRecord("req-live"), completedRecord("r1", "assistant", "", "答", 1)},
			"s2": {pendingRecord("req-dead")},
		}
	}, nil, nil, "", nil, nil, nil, nil, func() []string { return []string{"req-live", "req-dead"} }, nil)
	if !deadOnly.ScopeHasPendingRequest(GlobalNewChatScope) {
		t.Fatalf("live pending behind assistant should match")
	}
	// 活跃 ID 不含任何尾部 pending → false
	idle := NewTopicPipeline(store, nil, records, nil, nil, "", nil, nil, nil, nil,
		func() []string { return []string{"req-other"} }, nil)
	if idle.ScopeHasPendingRequest(GlobalNewChatScope) {
		t.Fatalf("unrelated live request should not hold")
	}
	// 非法 scope → false
	if live.ScopeHasPendingRequest("project:") {
		t.Fatalf("invalid scope")
	}
}

// TestSelectedTopicSearchProviderID 对齐 topic_search.py:304-326。
func TestSelectedTopicSearchProviderID(t *testing.T) {
	// 未设变量 → 原生 lane（两个自由引擎开关需为 true）
	env := map[string]string{
		"FREE_SEARCH_DDG_ENABLED": "true", "FREE_SEARCH_BING_ENABLED": "true",
	}
	id, err := SelectedTopicSearchProviderID(env)
	if err != nil || id != OpenjiuwenNativeSearchProviderId {
		t.Fatalf("native: %s %v", id, err)
	}
	// 显式 openjiuwen-native 同上
	env["TOPIC_STARTER_SEARCH_PROVIDER"] = "openjiuwen-native"
	id2, err2 := SelectedTopicSearchProviderID(env)
	if err2 != nil || id2 != OpenjiuwenNativeSearchProviderId {
		t.Fatalf("explicit native: %s %v", id2, err2)
	}
	// 开关缺失 → 契约错误
	delete(env, "FREE_SEARCH_DDG_ENABLED")
	if _, err3 := SelectedTopicSearchProviderID(env); err3 == nil ||
		err3.(*OpenJiuwenNativeSearchContractError).Reason != "openjiuwen_native_search_requires_free_engines" {
		t.Fatalf("engines required: %v", err3)
	}
	// 未知名 → 大声失败
	env["FREE_SEARCH_DDG_ENABLED"] = "true"
	env["TOPIC_STARTER_SEARCH_PROVIDER"] = "unknown-lane"
	if _, err4 := SelectedTopicSearchProviderID(env); err4 == nil ||
		err4.(*OpenJiuwenNativeSearchContractError).Reason != "search_provider_not_configured" {
		t.Fatalf("unknown lane: %v", err4)
	}
}
