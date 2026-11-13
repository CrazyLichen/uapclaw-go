package recommendation

import "testing"

// TestHistoryStateStore_接口契约 过渡接口 4 方法契约（⤴️ 31d 替换真实实现）。
func TestHistoryStateStore_接口契约(t *testing.T) {
	store := NewInMemoryHistoryStateStore()
	if _, ok := store.LoadLatestHistoryState(); ok {
		t.Fatal("空库应无快照")
	}
	if _, ok := store.LoadHistoryStateAt(100); ok {
		t.Fatal("空库按时间也应无快照")
	}
	state := map[string]any{"schema_version": "v4", "history_ledger": map[string]any{}}
	store.SaveHistoryState(state, nil, 10)
	store.SaveHistoryState(map[string]any{"schema_version": "v4b"}, nil, 20)
	got, ok := store.LoadLatestHistoryState()
	if !ok || got["schema_version"] != "v4b" {
		t.Fatalf("最新快照不符: %v", got)
	}
	got, ok = store.LoadHistoryStateAt(15)
	if !ok || got["schema_version"] != "v4" {
		t.Fatalf("时间点快照不符: %v", got)
	}
	if _, ok := store.LoadHistoryStateAt(5); ok {
		t.Fatal("早于全部快照应无结果")
	}
	empty := store.EmptyHistoryState()
	if _, has := empty["history_ledger"]; !has {
		t.Fatal("空白状态必须含 history_ledger（对齐 empty_history_state :2016-2028）")
	}
}
