package recommendation

import (
	"path/filepath"
	"testing"
)

func newAuditTestStore(t *testing.T) *TopicStarterStore {
	t.Helper()
	store, err := NewTopicStarterStore(filepath.Join(t.TempDir(), "recommendation.db"))
	if err != nil {
		t.Fatalf("建库: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newTestAudit(t *testing.T, store *TopicStarterStore) *ExternalInvocationAudit {
	t.Helper()
	audit, err := NewExternalInvocationAudit(store, "policy-1", "snapshot-1", "global_new_chat", "visit-1")
	if err != nil {
		t.Fatalf("构造审计器: %v", err)
	}
	return audit
}

func TestExternalInvocationAudit_构造校验(t *testing.T) {
	store := newAuditTestStore(t)
	if _, err := NewExternalInvocationAudit(nil, "p", "s", "global_new_chat", "v"); err == nil {
		t.Fatal("nil store 必须报错")
	}
	if _, err := NewExternalInvocationAudit(store, "", "s", "global_new_chat", "v"); err == nil {
		t.Fatal("空 policy_id 必须报错（对齐 :75-76）")
	}
	if _, err := NewExternalInvocationAudit(store, "p", "s", "bad_scope", "v"); err == nil {
		t.Fatal("非法 scope 必须报错（对齐 :77-78）")
	}
	if _, err := NewExternalInvocationAudit(store, "p", "s", "project:abc", "v"); err != nil {
		t.Fatalf("project scope 合法: %v", err)
	}
}

func TestExternalInvocationAudit_尝试与终局闭环(t *testing.T) {
	audit := newTestAudit(t, newAuditTestStore(t))
	if err := audit.RecordAttempt("search", "op-1", "req-1", "comp-1", "prov-1", "cand-1"); err != nil {
		t.Fatalf("record_attempt: %v", err)
	}
	if err := audit.RecordSuccess("search", "op-1", "prov-1", "req-1", "result-1", 12.5); err != nil {
		t.Fatalf("record_success: %v", err)
	}
	receipt := audit.AsManifestReceipt()
	counters := receipt["counters"].(map[string]any)
	if counters["total"] != 1 || counters["success"] != 1 || counters["failure"] != 0 {
		t.Fatalf("计数器不符: %v", counters)
	}
	byOp := counters["by_operation"].(map[string]any)["search"].(map[string]any)
	if byOp["total"] != 1 || byOp["success"] != 1 {
		t.Fatalf("按操作计数不符: %v", byOp)
	}
	invocations := receipt["invocations"].([]map[string]any)
	if invocations[0]["sequence"] != 1 {
		t.Fatal("invocations 必须按序编号（对齐 :329-332）")
	}
	if invocations[0]["candidate_id"] != "cand-1" {
		t.Fatal("candidate_id 必须来自 attempt 记录")
	}
}

func TestExternalInvocationAudit_重复完结拒绝(t *testing.T) {
	audit := newTestAudit(t, newAuditTestStore(t))
	if err := audit.RecordAttempt("search", "op-1", "req-1", "c", "p", "cand-1"); err != nil {
		t.Fatal(err)
	}
	if err := audit.RecordSuccess("search", "op-1", "p", "req-1", "r1", 1); err != nil {
		t.Fatal(err)
	}
	if err := audit.RecordSuccess("search", "op-1", "p", "req-1", "r1", 1); err == nil {
		t.Fatal("重复完结必须拒绝（对齐 :234-235 already completed）")
	}
	audit2 := newTestAudit(t, newAuditTestStore(t))
	if err := audit2.RecordAttempt("search", "op-1", "req-1", "c", "p", "cand-1"); err != nil {
		t.Fatal(err)
	}
	if err := audit2.RecordAttempt("search", "op-1", "req-1", "c", "p", "cand-1"); err == nil {
		t.Fatal("重复尝试必须拒绝（对齐 :144-145 already recorded）")
	}
}

func TestExternalInvocationAudit_终局与尝试不匹配拒绝(t *testing.T) {
	audit := newTestAudit(t, newAuditTestStore(t))
	if err := audit.RecordAttempt("search", "op-1", "req-1", "c", "p", "cand-1"); err != nil {
		t.Fatal(err)
	}
	// request_id 不匹配
	if err := audit.RecordSuccess("search", "op-1", "p", "req-9", "r1", 1); err == nil {
		t.Fatal("终局小票必须匹配精确的尝试（对齐 :228-232）")
	}
	// operation 不匹配
	if err := audit.RecordSuccess("fetch", "op-1", "p", "req-1", "r1", 1); err == nil {
		t.Fatal("operation 不匹配必须拒绝")
	}
}

func TestExternalInvocationAudit_参数校验矩阵(t *testing.T) {
	audit := newTestAudit(t, newAuditTestStore(t))
	if err := audit.RecordSuccess("search", "", "p", "r", "res", 1); err == nil {
		t.Fatal("空 operation_id 拒绝")
	}
	if err := audit.RecordSuccess("search", "o", "p", "r", "", 1); err == nil {
		t.Fatal("success 缺 result_id 拒绝（对齐 :215-216）")
	}
	if err := audit.RecordFailure("search", "o", "p", "r", "服务器炸了", 1); err == nil {
		t.Fatal("failure_reason 非机器可读码拒绝（对齐 :217-221）")
	}
	if err := audit.RecordFailure("search", "o", "p", "r", "timeout", -1); err == nil {
		t.Fatal("负 latency 拒绝（对齐 :222-227）")
	}
	if err := audit.RecordSuccess("bogus", "o", "p", "r", "res", 1); err == nil {
		t.Fatal("非法 operation 拒绝（对齐 :207-208）")
	}
	if err := audit.RecordFailure("search", "o", "p", "r", "timeout", 1); err == nil {
		t.Fatal("无 attempt 的终局拒绝")
	}
}

func TestExternalInvocationAudit_快照持久化与加载(t *testing.T) {
	// ⤴️ 31b：注入生成尝试 ID 后快照才会落库（对齐 :259-261 attempt 为 None 时跳过）
	defer func(orig func() string) { currentBackgroundGenerationAttemptID = orig }(currentBackgroundGenerationAttemptID)
	currentBackgroundGenerationAttemptID = func() string { return "attempt-42" }

	store := newAuditTestStore(t)
	audit := newTestAudit(t, store)
	if err := audit.RecordAttempt("search", "op-1", "req-1", "c", "p", "cand-1"); err != nil {
		t.Fatal(err)
	}
	if err := audit.RecordSuccess("search", "op-1", "p", "req-1", "r1", 2); err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadExternalInvocationAuditSnapshot(store, "policy-1", "attempt-42")
	if err != nil {
		t.Fatalf("加载快照: %v", err)
	}
	if snapshot == nil {
		t.Fatal("快照必须落库")
	}
	counters := snapshot["counters"].(map[string]any)
	if n, _ := auditPlainInt(counters["total"]); n != 1 {
		// 落库走 JSON 往返，int 解出 float64；用整值 float 兼容比较（D-6 先例）
		t.Fatalf("快照计数不符: %v", counters)
	}
	// 空 attempt 时无快照
	currentBackgroundGenerationAttemptID = func() string { return "" }
	audit2 := newTestAudit(t, newAuditTestStore(t))
	if err := audit2.RecordAttempt("search", "op-1", "req-1", "c", "p", "cand-1"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = LoadExternalInvocationAuditSnapshot(newAuditTestStore(t), "policy-1", "attempt-42")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot != nil {
		t.Fatal("空 attempt_id（无快照落库）必须查无")
	}
}

func TestExternalInvocationAudit_加载快照过滤矩阵(t *testing.T) {
	store := newAuditTestStore(t)
	defer func(orig func() string) { currentBackgroundGenerationAttemptID = orig }(currentBackgroundGenerationAttemptID)
	currentBackgroundGenerationAttemptID = func() string { return "attempt-1" }
	audit := newTestAudit(t, store)
	if err := audit.RecordAttempt("fetch", "op-1", "req-1", "c", "p", "cand-1"); err != nil {
		t.Fatal(err)
	}
	if err := audit.RecordFailure("fetch", "op-1", "p", "req-1", "timeout", 3); err != nil {
		t.Fatal(err)
	}
	// 错误 policy / attempt 查不到
	if s, _ := LoadExternalInvocationAuditSnapshot(store, "policy-9", "attempt-1"); s != nil {
		t.Fatal("policy 不匹配应查无")
	}
	if s, _ := LoadExternalInvocationAuditSnapshot(store, "policy-1", "attempt-9"); s != nil {
		t.Fatal("attempt 不匹配应查无")
	}
	s, err := LoadExternalInvocationAuditSnapshot(store, "policy-1", "attempt-1")
	if err != nil || s == nil {
		t.Fatalf("正常加载: %v %v", s, err)
	}
	// failure 归因进 counters（JSON 往返后为 float64，整值比较）
	if n, _ := auditPlainInt(s["counters"].(map[string]any)["failure"]); n != 1 {
		t.Fatal("failure 计数不符")
	}
}
