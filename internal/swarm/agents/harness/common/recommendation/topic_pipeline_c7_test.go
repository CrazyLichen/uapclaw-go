package recommendation

// topic_pipeline_c7_test.go 对齐 topic_pipeline.py:3786-4130
// _default_context_snapshot 的单元用例。

import (
	"strings"
	"testing"
)

// snapshotRecords 会话记录：一条已完结用户请求（含完结行）。
func snapshotRecords() map[string][]map[string]any {
	return map[string][]map[string]any{
		"s1": {
			{"id": "evt-1", "request_id": "req-1", "role": "user",
				"content": "我想学习围棋的基本定式", "input_origin": "manual_free_text",
				"assistance_origin": "none", "timestamp": 10.0, "ordinal": 0},
			{"request_id": "req-1", "role": "assistant", "event_type": "chat.final",
				"content": "定式讲解", "ordinal": 1},
			{"request_id": "req-1", "role": "assistant", "event_type": "chat.request_completed",
				"id": "req-1:request_completed", "status": "success", "timestamp": 12.0,
				"feedback_only": true, "ordinal": 2},
		},
	}
}

// TestDefaultContextSnapshot_StateUnavailable 对齐 :3809-3813 无小票拒绝。
func TestDefaultContextSnapshot_StateUnavailable(t *testing.T) {
	store := newTestStore(t)
	_, err := DefaultContextSnapshot(store, GlobalNewChatScope, 100.0,
		func() map[string][]map[string]any { return map[string][]map[string]any{} })
	if err == nil {
		t.Fatalf("no state receipt should fail")
	}
	if stateErr, ok := err.(*StateUnavailableError); !ok || stateErr.Reason != "state_unavailable" {
		t.Fatalf("error type: %v", err)
	}
}

// TestDefaultContextSnapshot_GlobalFlow 对齐 :3786-4130 全局 scope 主链路。
func TestDefaultContextSnapshot_GlobalFlow(t *testing.T) {
	store := newTestStore(t)
	state := v4StateWithBehavior(1, "s1", 0)
	if _, err := HistorySaveV4State(store, state, nil, 40.0); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if _, err := PersistStateMaintenanceReceipt(store, state, "behavior_mapping", 50.0); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	snapshot, err := DefaultContextSnapshot(store, GlobalNewChatScope, 100.0, snapshotRecords)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.ContextScope != GlobalNewChatScope {
		t.Fatalf("scope: %s", snapshot.ContextScope)
	}
	if !strings.HasPrefix(snapshot.InputSnapshotID, "v6-input:global_new_chat:0:0:") {
		t.Fatalf("snapshot id: %s", snapshot.InputSnapshotID)
	}
	// 活跃行为经 scope 匹配（无会话元数据 → 空项目桶命中全局）与完结前缀过滤
	if len(snapshot.Messages) != 1 {
		t.Fatalf("messages: %d", len(snapshot.Messages))
	}
	message := snapshot.Messages[0]
	if message.IntentCluster != "gaming-interactive" || message.ActionType != "natural_question" {
		t.Fatalf("mapping: %s/%s", message.IntentCluster, message.ActionType)
	}
	if message.SessionID != "s1" || message.Ordinal != 0 {
		t.Fatalf("locator: %s/%d", message.SessionID, message.Ordinal)
	}
	// 完结回答已挂到消息上（answers_by_session）
	if answer, ok := message.AssistantAnswer.(string); !ok || answer != "定式讲解" {
		t.Fatalf("assistant answer: %v", message.AssistantAnswer)
	}
	// 完结前缀投影
	if len(snapshot.CompletedSessionPrefixes) != 1 ||
		snapshot.CompletedSessionPrefixes[0].Boundary.RequestID != "req-1" {
		t.Fatalf("prefixes: %+v", snapshot.CompletedSessionPrefixes)
	}
	// 画像投影
	projection := snapshot.ProfileProjection
	if clusters := pyListStrings(projection["abstract_interest_clusters"]); len(clusters) != 1 ||
		clusters[0] != "gaming-interactive" {
		t.Fatalf("abstract clusters: %v", projection["abstract_interest_clusters"])
	}
	// 状态小票带负偏好版本
	receipt := snapshot.StateMaintenanceReceipt
	if _, present := receipt["negative_preference_version"]; !present {
		t.Fatalf("receipt: %v", receipt)
	}
	// 无短意图状态 → 默认 v0
	if snapshot.ShortIntentState.ShortIntentVersion != 0 ||
		snapshot.ShortIntentState.InputSnapshotID != "v6-short:global_new_chat:0" {
		t.Fatalf("short state: %+v", snapshot.ShortIntentState)
	}
}

// TestDefaultContextSnapshot_ProjectScope 对齐项目 scope 分支。
func TestDefaultContextSnapshot_ProjectScope(t *testing.T) {
	store := newTestStore(t)
	state := v4StateWithBehavior(1, "s1", 0)
	if _, err := HistorySaveV4State(store, state, nil, 40.0); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if _, err := PersistStateMaintenanceReceipt(store, state, "behavior_mapping", 50.0); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	// 非法 scope 报错
	if _, err := DefaultContextSnapshot(store, "project:", 100.0, snapshotRecords); err == nil {
		t.Fatalf("invalid scope should fail")
	}
	snapshot, err := DefaultContextSnapshot(store, "project:p1", 100.0, snapshotRecords)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// 项目 scope 走 ProjectUnitGlobalDiscussion 分支；无单元 → 空覆盖
	if snapshot.OtherScopeDiscussionCoverage == nil {
		t.Fatalf("coverage should be empty list, not nil")
	}
	if len(snapshot.OtherScopeDiscussionCoverage) != 0 {
		t.Fatalf("coverage: %v", snapshot.OtherScopeDiscussionCoverage)
	}
}
