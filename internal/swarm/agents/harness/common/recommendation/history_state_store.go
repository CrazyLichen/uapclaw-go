package recommendation

// HistoryStateStore topic 历史状态快照读写窄接口。
//
// ⤴️ 10.3.31d 过渡接口（spec 2026-11-12 D-c-5）：10.3.31c 的
// topic_outcomes / topic_feedback / topic_product 引用上游
// topic_history.py（3818 行，属 31d）的 4 个函数，本接口先行过渡；
// 31d 落地 topic_history.go 后改为真实类型适配注入并删除 fake（规则 8）。
//
// 对齐 Python topic_history.py:
//   - load_latest_history_state(store)       (:2500)
//   - load_history_state_at(store, at)       (:2506)
//   - save_history_state(store, state, ...)  (:2520)
//   - empty_history_state()                  (:2016)
type HistoryStateStore interface {
	// LoadLatestHistoryState 取最新 v4 历史状态快照；无快照返回 (nil,false)
	LoadLatestHistoryState() (map[string]any, bool)
	// LoadHistoryStateAt 取 at 时间点之前最近的快照
	LoadHistoryStateAt(at float64) (map[string]any, bool)
	// SaveHistoryState 单事务提交一次 v4 成功（快照 + 见证清除 + 成功事件同生共死）
	SaveHistoryState(state map[string]any, events []map[string]any, now float64) map[string]any
	// EmptyHistoryState 全新空白状态（schema/policy 版本随 31d 落地对齐）
	EmptyHistoryState() map[string]any
}

// ──────────────────────────── 结构体 ────────────────────────────

// inMemoryHistoryStateStore 测试用内存实现（⤴️ 31d 落地后删除）。
type inMemoryHistoryStateStore struct {
	snapshots []historySnapshot
	now       float64
}

// historySnapshot 一条带时间戳的快照。
type historySnapshot struct {
	at    float64
	state map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryHistoryStateStore 构造内存 fake（clock 可为 0，Save 时用传入 now）。
func NewInMemoryHistoryStateStore() HistoryStateStore {
	return &inMemoryHistoryStateStore{}
}

// LoadLatestHistoryState 返回最后一条快照。
func (s *inMemoryHistoryStateStore) LoadLatestHistoryState() (map[string]any, bool) {
	if len(s.snapshots) == 0 {
		return nil, false
	}
	return s.snapshots[len(s.snapshots)-1].state, true
}

// LoadHistoryStateAt 返回 at 之前（含）最近的快照。
func (s *inMemoryHistoryStateStore) LoadHistoryStateAt(at float64) (map[string]any, bool) {
	for i := len(s.snapshots) - 1; i >= 0; i-- {
		if s.snapshots[i].at <= at {
			return s.snapshots[i].state, true
		}
	}
	return nil, false
}

// SaveHistoryState 追加快照并回传提交后的状态。
func (s *inMemoryHistoryStateStore) SaveHistoryState(state map[string]any, _ []map[string]any, now float64) map[string]any {
	s.snapshots = append(s.snapshots, historySnapshot{at: now, state: state})
	return state
}

// EmptyHistoryState 空白状态骨架（真实常量 ⤴️ 31d 对齐 _V4_SCHEMA_VERSION）。
func (s *inMemoryHistoryStateStore) EmptyHistoryState() map[string]any {
	return map[string]any{
		"schema_version": "",
		"policy_id":      "",
		"history_ledger": map[string]any{},
	}
}
