package recommendation

import (
	"strings"
)

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

// ──────────────────────────── ⤴️ 31d 过渡函数（topic_history.py 切片） ────────────────────────────

// 以下三个函数来自上游 topic_history.py / topic_starter.py，被 topic_pool（10.3.31b）
// 依赖；31d 落地 topic_history.go 后改为真实实现并删除本段（规则 8）。

// ApplyNegativePreferences D20 检索前负偏好比较器（对齐 topic_history.py:1904-1955）。
// 逐字复用 v3 的 _keyword_matches（ASCII 词边界、CJK 子串），作用于被否定方向、
// 冻结别名/锚点与来源 ID 对候选的来源、意图/人设、查询与锚点。只拒绝被否定的
// 具体方向，同领域其他方向不受影响（D20，无通用语义裁判）。
func ApplyNegativePreferences(candidates []map[string]any, negativePreferences []map[string]any) ([]map[string]any, []map[string]any) {
	survivors := []map[string]any{}
	rejected := []map[string]any{}
	for _, candidate := range candidates {
		surfaces := []string{
			str(candidate["intent"]),
			str(candidate["persona"]),
			str(candidate["primary_query"]),
			str(candidate["fallback_query"]),
			str(candidate["identity_anchor"]),
		}
		for _, anchor := range anySliceToStrings(candidate["context_anchors"]) {
			surfaces = append(surfaces, anchor)
		}
		sourceIDs := map[string]struct{}{}
		for _, unitID := range anySliceToStrings(candidate["source_memory_unit_ids"]) {
			sourceIDs[unitID] = struct{}{}
		}
		hit := false
		for _, preference := range negativePreferences {
			sourceID := strings.TrimSpace(str(preference["source_id"]))
			if sourceID != "" {
				if _, known := sourceIDs[sourceID]; known {
					hit = true
					break
				}
			}
			terms := negativePreferenceTerms(preference)
			for _, surface := range surfaces {
				if surface == "" {
					continue
				}
				for _, term := range terms {
					if keywordMatches(surface, term) {
						hit = true
						break
					}
				}
				if hit {
					break
				}
			}
			if hit {
				break
			}
		}
		if hit {
			rejected = append(rejected, map[string]any{
				"candidate_id": candidate["candidate_id"],
				"reason":       "negative_preference_contract_failed",
			})
		} else {
			survivors = append(survivors, candidate)
		}
	}
	return survivors, rejected
}

// negativePreferenceTerms 负偏好词条（对齐 topic_history.py:1895-1902 _negative_preference_terms）。
func negativePreferenceTerms(preference map[string]any) []string {
	terms := []string{strings.TrimSpace(str(preference["direction"]))}
	for _, key := range []string{"aliases", "anchors"} {
		for _, term := range anySliceToStrings(preference[key]) {
			if strings.TrimSpace(term) != "" {
				terms = append(terms, strings.TrimSpace(term))
			}
		}
	}
	out := []string{}
	for _, term := range terms {
		if term != "" {
			out = append(out, term)
		}
	}
	return out
}

// keywordMatches v3 关键词匹配（对齐 topic_starter.py:844-851 _keyword_matches）。
// ASCII 词边界，CJK 子串。RE2 无 lookaround，以手工边界检查等价实现
// （Python: (?<![A-Za-z0-9_])kw(?![A-Za-z0-9_])）。
func keywordMatches(text, keyword string) bool {
	if !isASCII(keyword) {
		return strings.Contains(text, keyword)
	}
	isWordByte := func(b byte) bool {
		return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
	}
	for start := 0; ; {
		idx := strings.Index(text[start:], keyword)
		if idx < 0 {
			return false
		}
		pos := start + idx
		prevOK := pos == 0 || !isWordByte(text[pos-1])
		end := pos + len(keyword)
		nextOK := end >= len(text) || !isWordByte(text[end])
		if prevOK && nextOK {
			return true
		}
		start = pos + 1
	}
}

// isASCII 全 ASCII 判定（对齐 Python str.isascii）。
func isASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] > 0x7f {
			return false
		}
	}
	return true
}

// FilterDistinctPersonalizedCandidates R4 两条硬冲突下去重保序（对齐 topic_history.py:1809-1838）。
// 这是终局清单助手而非专家池过滤器：过采样合法地含同桶多个替代；泛化/趋势卡
// 既无来源桶也无身份锚，不占个性化去重槽位。
func FilterDistinctPersonalizedCandidates(candidates []map[string]any, targetCount int) []map[string]any {
	kept := []map[string]any{}
	usedBuckets := map[string]struct{}{}
	usedAnchors := map[string]struct{}{}
	for _, candidate := range candidates {
		if len(kept) >= targetCount {
			break
		}
		buckets := map[string]struct{}{}
		for _, unitID := range anySliceToStrings(candidate["source_memory_unit_ids"]) {
			if unitID != "" {
				buckets[unitID] = struct{}{}
			}
		}
		anchor := str(candidate["identity_anchor"])
		personalized := len(buckets) > 0 || anchor != ""
		bucketClash := false
		for bucket := range buckets {
			if _, used := usedBuckets[bucket]; used {
				bucketClash = true
				break
			}
		}
		_, anchorClash := usedAnchors[anchor]
		if personalized && (bucketClash || anchorClash) {
			continue
		}
		kept = append(kept, candidate)
		if personalized {
			for bucket := range buckets {
				usedBuckets[bucket] = struct{}{}
			}
			if anchor != "" {
				usedAnchors[anchor] = struct{}{}
			}
		}
	}
	return kept
}
