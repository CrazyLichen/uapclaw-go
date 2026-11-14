package recommendation

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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

// ──────────────────────────── 常量 ────────────────────────────

// v4 侧常量（topic_history.py:43/44/75/76）
const (
	// v4HistoryPolicyID v4 历史状态策略 ID
	v4HistoryPolicyID = "topic-starter-mvp-v4"
	// v4TaxonomyVersion 行为映射分类法版本
	v4TaxonomyVersion = "v4-conversation-taxonomy-1"
	// v4MappingPromptVersion 行为映射提示词版本
	v4MappingPromptVersion = "v4-mapping-prompt-3"
)

// TaxonomyTags 项目适配的冻结会话域分类法（topic_history.py:86-105）。
// 扩展只能通过 provisional 标签加显式协议版本升级。
var TaxonomyTags = map[string]string{
	"tech-ai-ml":                 "人工智能与机器学习",
	"tech-software-dev":          "软件开发与工程",
	"tech-hardware-devices":      "硬件与智能设备",
	"science-math-research":      "数学与基础科学研究",
	"science-life-medicine":      "生命科学与医学健康",
	"finance-investment":         "金融与投资理财",
	"business-career":            "商业与职业发展",
	"education-learning":         "学习方法与教育",
	"culture-history-humanities": "历史与人文",
	"arts-media-entertainment":   "文艺与影音娱乐",
	"gaming-interactive":         "游戏与互动娱乐",
	"sports-fitness":             "体育运动与健身",
	"travel-outdoor":             "旅行与户外",
	"food-cooking":               "美食与烹饪",
	"home-lifestyle":             "生活方式与家庭",
	"society-news-current":       "社会时事与新闻",
	"language-writing":           "语言与写作表达",
	"psychology-self":            "心理与自我成长",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// HistoryEmptyV4State 全新空白 v4 状态（⤴️ 31d：随 topic_history.py 整体落地后
// 归位 topic_history.go 并由真实引擎消费）。
// Python: topic_history.py:2016 empty_history_state + :253 _empty_ledger
func HistoryEmptyV4State() map[string]any {
	return map[string]any{
		"schema_version": v4SnapshotSchema,
		"policy_id":      v4HistoryPolicyID,
		"history_ledger": map[string]any{
			"history_revision":    0,
			"next_behavior_index": 1,
			"sessions":            map[string]any{},
		},
		"behavior_mapping": map[string]any{
			"taxonomy_version": v4TaxonomyVersion,
			"prompt_version":   v4MappingPromptVersion,
			"entries":          map[string]any{},
		},
		"memory": map[string]any{
			"initialized":      false,
			"semantic_version": 0,
			"unit_counter":     0,
			"units":            []any{},
		},
		"memory_sidecar": map[string]any{
			"negative_preferences": []any{},
			"explicit_corrections": []any{},
		},
		"memory_cursor": map[string]any{
			"last_success_at":                    nil,
			"consumed_through_behavior_index":    0,
			"pending_unmatched_behavior_indices": []any{},
			"origin":                             "",
		},
		"mobile_trigger": map[string]any{
			"reference_status":           "uninitialized_empty",
			"reference_tags":             []any{},
			"reference_locators":         []any{},
			"reference_behavior_indices": []any{},
		},
	}
}

// HistoryLoadLatestV4 从事件账本读最新 v4 快照（仅 schema_version=v4 的行）。
// Python: topic_history.py:2500 load_latest_history_state
func HistoryLoadLatestV4(store *TopicStarterStore) (map[string]any, bool) {
	db, err := store.connect()
	if err != nil {
		return nil, false
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT snapshot_json FROM profile_snapshot ORDER BY version DESC")
	if err != nil {
		return nil, false
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw any
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		text := ""
		switch value := raw.(type) {
		case []byte:
			text = string(value)
		case string:
			text = value
		default:
			continue
		}
		if text == "" {
			continue
		}
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			continue
		}
		if pyStr(decoded["schema_version"]) != v4SnapshotSchema {
			continue
		}
		return decoded, true
	}
	return nil, false
}

// HistorySaveV4State 单事务提交一次 v4 成功：新快照插入、被顶替快照的见证剥离、
// 成功事件写入同生共死。
// Python: topic_history.py:2520 save_history_state（事件 INSERT 镜像 record_event，
// 与 TopicStarterStore.event_key_of 共享去重键）
func HistorySaveV4State(store *TopicStarterStore, state map[string]any,
	events []map[string]any, now float64) (map[string]any, error) {
	db, err := store.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 步骤 1：previous = 最新 v4 快照（_latest_v4_row；逐行解析过滤 schema）
	var previousVersion int
	previous := map[string]any{}
	hasPrevious := false
	prevRows, err := tx.Query("SELECT snapshot_json, version FROM profile_snapshot ORDER BY version DESC")
	if err != nil {
		return nil, err
	}
	for prevRows.Next() {
		var rawJSON string
		var rowVersion int
		if err := prevRows.Scan(&rawJSON, &rowVersion); err != nil {
			continue
		}
		candidate := map[string]any{}
		if json.Unmarshal([]byte(rawJSON), &candidate) != nil {
			continue
		}
		if pyStr(candidate["schema_version"]) != v4SnapshotSchema {
			continue
		}
		previous = candidate
		previousVersion = rowVersion
		hasPrevious = true
		break
	}
	_ = prevRows.Close()
	_ = prevRows.Err()

	var version int
	if err := tx.QueryRow("SELECT COALESCE(MAX(version), 0) + 1 FROM profile_snapshot").Scan(&version); err != nil {
		return nil, err
	}
	stored := map[string]any{}
	for key, value := range state {
		stored[key] = value
	}
	stored["version"] = version
	stored["updated_at"] = now
	stored["evidence_until"] = 0.0
	encoded, err := marshalNoEscape(stored)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		"INSERT INTO profile_snapshot(version, created_at, evidence_until, snapshot_json) VALUES(?, ?, ?, ?)",
		version, now, 0.0, string(encoded),
	); err != nil {
		return nil, err
	}
	if hasPrevious {
		// D7：见证内容只随活跃注册保留，被顶替快照剥离 witness
		stripHistoryWitness(previous)
		encodedPrevious, err := marshalNoEscape(previous)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(
			"UPDATE profile_snapshot SET snapshot_json = ? WHERE version = ?",
			string(encodedPrevious), previousVersion,
		); err != nil {
			return nil, err
		}
	}
	for _, event := range events {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO recommendation_event(event_key, session_id, slate_id, event_type, topic_id, position, profile_version, policy_id, selection_mode, propensity, propensity_reason, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			store.EventKeyOf(event),
			pyStr(event["session_id"]),
			pyStr(event["slate_id"]),
			pyStr(event["event_type"]),
			event["topic_id"],
			event["position"],
			event["profile_version"],
			pyStrOr(event["policy_id"], v4HistoryPolicyID),
			pyStrOr(event["selection_mode"], "state_maintenance"),
			event["propensity"],
			pyStrOr(event["propensity_reason"], "not_estimated"),
			string(mustMarshalNoEscape(event["payload"])),
			toFloatOr(event["created_at"], now),
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stored, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stripHistoryWitness 剥离被顶替快照中全部 witness 字段
// （Python save_history_state 内联双层循环 :2567-2575）
func stripHistoryWitness(previous map[string]any) {
	ledger, ok := previous["history_ledger"].(map[string]any)
	if !ok {
		return
	}
	sessions, ok := ledger["sessions"].(map[string]any)
	if !ok {
		return
	}
	for _, sessionState := range sessions {
		sessionMap, ok := sessionState.(map[string]any)
		if !ok {
			continue
		}
		entries, ok := sessionMap["entries"].([]any)
		if !ok {
			continue
		}
		for _, entry := range entries {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			delete(entryMap, "witness")
		}
	}
}

func mustMarshalNoEscape(value any) []byte {
	data, err := marshalNoEscape(value)
	if err != nil {
		return []byte("{}")
	}
	return data
}

func pyStrOr(value any, fallback string) string {
	if text := pyStr(value); text != "" {
		return text
	}
	return fallback
}

func toFloatOr(value any, fallback float64) float64 {
	if f, ok := toFloat(value); ok {
		return f
	}
	return fallback
}

// ──────────────────────────── ⤴️ 31d 过渡：topic_history.py 纯函数切片（10.3.31b 依赖） ────────────────────────────

// 以下类型与纯函数来自上游 topic_history.py（3818 行，属 31d），被 10.3.31b 的
// topic_prepare 依赖。它们是确定性纯逻辑，31d 落地 topic_history.go 时整段**归位**
// （搬移非重写），故不属兼容壳；唯一真过渡件是 HistoryEngine 引擎缝接口与
// fakeHistoryEngine（TopicHistoryEngine 类 :2863-3818 属 31d，用户 2026-11-13
// 拍板：31d 计划文档标注待回填，用真实类型回填后删除接口与 fake，规则 8）。

// CanonicalBehavior 规范行为（topic_history.py:214-221）。
type CanonicalBehavior struct {
	// BehaviorIndex 行为全局序号
	BehaviorIndex int
	// SessionID 会话 ID
	SessionID string
	// Ordinal 会话内记录序号
	Ordinal int
	// Content 记录正文
	Content string
	// TimestampOr0 记录时间戳（非法回退 0）
	TimestampOr0 float64
	// RecordID 记录 ID（active_behaviors_from_records 视图才有）
	RecordID string
}

// AnchorEvidence 锚点证据定位（topic_history.py:929-934）。
type AnchorEvidence struct {
	// Origin 证据来源（repeated_literal / model_span）
	Origin string
	// SourceLocator 来源定位
	SourceLocator SourceLocator
	// Start 0-based 起始（Python 字符索引）
	Start int
	// End end-exclusive 结束
	End int
}

// AnchorBucket 单个记忆单元的冻结锚点面（topic_history.py:937-950）。
type AnchorBucket struct {
	// UnitID 记忆单元 ID
	UnitID string
	// Locators 来源定位元组
	Locators []SourceLocator
	// Anchors 允许的逐字锚点集合
	Anchors []string
	// ExplicitSingleEvidence D20 窄路径标记
	ExplicitSingleEvidence bool
	// IdentityAnchor 窄路径身份锚
	IdentityAnchor string
	// AnchorEvidence 锚点→证据定位
	AnchorEvidence map[string][]AnchorEvidence
	// PolicyId 策略 ID
	PolicyId string
	// ValidationStatus 校验状态
	ValidationStatus string
}

// SpanProposal I1 API 输出；文本刻意缺席且永不信任（topic_history.py:683-689）。
type SpanProposal struct {
	// BehaviorId 行为序号
	BehaviorId int
	// SourceLocator 来源定位
	SourceLocator SourceLocator
	// Start 起始
	Start int
	// End 结束
	End int
}

// ValidatedSpan 宿主回收的精确切片，与可替换的 I1 API 分离（topic_history.py:693-700）。
type ValidatedSpan struct {
	// BehaviorId 行为序号
	BehaviorId int
	// SourceLocator 来源定位
	SourceLocator SourceLocator
	// Start 起始
	Start int
	// End 结束
	End int
	// Text 切片原文
	Text string
}

// ContextLimitInfo 上下文限额信息（topic_history.py:541-544）。
type ContextLimitInfo struct {
	// Value 限额值（nil 表示未验证）
	Value *int
	// Source 限额来源
	Source string
}

// HistoryRefreshOutcome TopicHistoryEngine.refresh 的结果投影
// （Python HistoryRefreshOutcome；仅收录 topic_prepare :656-688 消费的字段）。
type HistoryRefreshOutcome struct {
	// Status ok / blocked
	Status string
	// State 提交后的最新状态（成功时非 nil）
	State map[string]any
	// MemoryMode 记忆阶段模式
	MemoryMode string
	// MemoryOutcome 记忆阶段结果
	MemoryOutcome string
	// RewriteDetected 历史重写是否检出
	RewriteDetected bool
	// MappingBatches 尝试的映射批次数
	MappingBatches int
	// Reason 失败原因
	Reason string
}

// HistoryRefreshRequest RefreshV5State 的入参（对齐 TopicHistoryEngine.__init__ :2866-2886）。
type HistoryRefreshRequest struct {
	// ModelName 模型名
	ModelName string
	// ContextLimit 上下文限额
	ContextLimit ContextLimitInfo
	// ModelProvenance 模型溯源
	ModelProvenance map[string]any
	// RecordsLoader 会话记录加载器（nil 用引擎默认）
	RecordsLoader func() map[string][]map[string]any
	// InvokeStage Ark 阶段调用器
	InvokeStage StageInvokerFunc
	// Now 时钟
	Now func() float64
}

// HistoryEngine TopicHistoryEngine（topic_history.py:2863）的过渡缝接口。
//
// ⤴️ 31d：真实引擎落地后改为注入 *TopicHistoryEngine 并删除本接口与
// fakeHistoryEngine（规则 8，锚点 HistoryEngine 于 internal/ 0 命中验收）。
type HistoryEngine interface {
	// RefreshV5State 执行 state_only_for_v5=True 的状态刷新
	// （Python refresh :3473，v5 分支停在 Memory Update Gate，不进 Planner/Expert）。
	RefreshV5State(ctx context.Context, req HistoryRefreshRequest) HistoryRefreshOutcome
}

// v5HistoryPolicyID v5 策略 ID（topic_history.py:65 _V5_POLICY_ID）。
const v5HistoryPolicyID = "topic-starter-mvp-v5"

// v5ValidationStatus v5 校验状态（:66 _V5_VALIDATION_STATUS）。
const v5ValidationStatus = "unvalidated-v5"

// v5MinCJKChars / v5MaxBucketDF Issue-01 校准常量（:70-72）。
const (
	v5MinCJKChars    = 2
	v5MaxBucketDF    = 0.5
	maxEvidenceChars = 2000
)

// cjkRE / asciiTokenRE / provisionalTagRE 正则（:80-82 与 topic_starter.py:45）。
var (
	cjkRE            = regexp.MustCompile(`[㐀-鿿]`)
	asciiTokenRE     = regexp.MustCompile(`[A-Za-z0-9_-]+`)
	provisionalTagRE = regexp.MustCompile(`^provisional:[a-z0-9][a-z0-9-]{0,63}$`)
	tagLikeRE        = regexp.MustCompile(`<([A-Za-z0-9_-]+)>`)
	upperSnakeRE     = regexp.MustCompile(`[A-Z0-9]+(?:_[A-Z0-9]+)+`)
	cjkRunRE         = regexp.MustCompile(`[㐀-鿿]+`)
	markdownLinkRE   = regexp.MustCompile(`\]\([^)]*\)`)
	slashPathRE      = regexp.MustCompile(`(^|[^:\w])/\S+`)
)

// HistoryTimestampOrZero 记录时间戳安全读取（topic_history.py:224-230）。
func HistoryTimestampOrZero(record map[string]any) float64 {
	value, ok := toFloat(record["timestamp"])
	if !ok {
		return 0.0
	}
	return value
}

// HistoryActiveLedgerBehaviors 活跃注册的索引/定位视图，见证正文仅作本地等值伴随，
// 禁止进模型载荷（topic_history.py:473-498，D10）。
func HistoryActiveLedgerBehaviors(ledger map[string]any) []CanonicalBehavior {
	var result []CanonicalBehavior
	sessions, _ := ledger["sessions"].(map[string]any)
	for sessionID, stateValue := range sessions {
		state, _ := stateValue.(map[string]any)
		entries, _ := state["entries"].([]any)
		for _, entryValue := range entries {
			entry, _ := entryValue.(map[string]any)
			if truthy(entry["retracted"]) {
				continue
			}
			witness, _ := entry["witness"].(map[string]any)
			result = append(result, CanonicalBehavior{
				BehaviorIndex: intOf(entry["behavior_index"]),
				SessionID:     sessionID,
				Ordinal:       intOf(entry["ordinal"]),
				Content:       pyStr(witness["content"]),
				TimestampOr0:  toFloatOr(witness["timestamp_or_0"], 0.0),
			})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].BehaviorIndex < result[j].BehaviorIndex
	})
	return result
}

// HistoryActiveBehaviorsFromRecords 从会话历史本体读证据正文的活跃行为视图
// （topic_history.py:500-534，D10）。
func HistoryActiveBehaviorsFromRecords(ledger map[string]any, recordsBySession map[string][]map[string]any) []CanonicalBehavior {
	var result []CanonicalBehavior
	sessions, _ := ledger["sessions"].(map[string]any)
	for sessionID, stateValue := range sessions {
		records := recordsBySession[sessionID]
		state, _ := stateValue.(map[string]any)
		entries, _ := state["entries"].([]any)
		for _, entryValue := range entries {
			entry, _ := entryValue.(map[string]any)
			if truthy(entry["retracted"]) {
				continue
			}
			ordinal := intOf(entry["ordinal"])
			if ordinal >= len(records) {
				continue
			}
			record := records[ordinal]
			result = append(result, CanonicalBehavior{
				BehaviorIndex: intOf(entry["behavior_index"]),
				SessionID:     sessionID,
				Ordinal:       ordinal,
				Content:       pyStr(record["content"]),
				TimestampOr0:  HistoryTimestampOrZero(record),
				RecordID:      pyStr(orValue(record["id"], record["request_id"])),
			})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].BehaviorIndex < result[j].BehaviorIndex
	})
	return result
}

// HistoryStateMappingEntries 行为映射条目按序号索引（topic_history.py:2048-2051）。
func HistoryStateMappingEntries(state map[string]any) map[int]map[string]any {
	entries := mapAnyOf(mapAnyOf(state["behavior_mapping"])["entries"])
	result := make(map[int]map[string]any, len(entries))
	for key, value := range entries {
		result[pyIntOf(key)] = mapAnyOf(value)
	}
	return result
}

// HistoryUncuratedMappedBehaviors 已映射且未被成功策展消费的行为
// （topic_history.py:2053-2077）。
func HistoryUncuratedMappedBehaviors(state map[string]any, active []CanonicalBehavior) []CanonicalBehavior {
	cursor := intOf(mapAnyOf(state["memory_cursor"])["consumed_through_behavior_index"])
	pending := map[int]bool{}
	for _, item := range anyListOf(mapAnyOf(state["memory_cursor"])["pending_unmatched_behavior_indices"]) {
		if plainInt(item) {
			pending[intOf(item)] = true
		}
	}
	mapping := HistoryStateMappingEntries(state)
	var result []CanonicalBehavior
	for _, behavior := range active {
		if _, ok := mapping[behavior.BehaviorIndex]; !ok {
			continue
		}
		if behavior.BehaviorIndex > cursor {
			result = append(result, behavior)
			continue
		}
		if pending[behavior.BehaviorIndex] {
			result = append(result, behavior)
		}
	}
	return result
}

// HistoryExtractVerbatimSpans I1 项目适配 API 适配器；宿主在其后校验位置
// （topic_history.py:760-788）。invoke 失败按上游告警并返回空（静默降级属上游语义）。
func HistoryExtractVerbatimSpans(ctx context.Context, sourceMessages []map[string]any, behaviorRecords []CanonicalBehavior, invokeStage StageInvokerFunc) []SpanProposal {
	behaviorPayload := make([]any, 0, len(behaviorRecords))
	for _, behavior := range behaviorRecords {
		behaviorPayload = append(behaviorPayload, map[string]any{
			"behavior_id":    behavior.BehaviorIndex,
			"source_locator": []any{behavior.SessionID, behavior.Ordinal},
		})
	}
	payload := map[string]any{
		"source_messages":  mapsToAny(sourceMessages),
		"behavior_records": behaviorPayload,
	}
	response, err := invokeStage(ctx, "verbatim_span_extractor", verbatimSpanSystemPrompt, marshalCompactPayload(payload))
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TopicPrepare] verbatim span invoke failed")
		return nil
	}
	return parseSpanProposals(response)
}

// HistoryValidateVerbatimSpans I1 位置的宿主准入；仅丢弃非法兄弟项
// （topic_history.py:799-868）。
func HistoryValidateVerbatimSpans(sourceMessages []map[string]any, behaviorRecords []CanonicalBehavior, proposals []SpanProposal) []ValidatedSpan {
	sourceByLocator := map[SourceLocator]string{}
	ambiguous := map[SourceLocator]bool{}
	for _, source := range sourceMessages {
		locatorValue, ok := source["source_locator"].([]any)
		if !ok || len(locatorValue) != 2 {
			continue
		}
		sessionID, ok := locatorValue[0].(string)
		if !ok || !plainInt(locatorValue[1]) {
			continue
		}
		content, ok := source["content"].(string)
		if !ok {
			continue
		}
		key := SourceLocator{SessionID: sessionID, Ordinal: intOf(locatorValue[1])}
		if existing, seen := sourceByLocator[key]; seen && existing != content {
			ambiguous[key] = true
		}
		sourceByLocator[key] = content
	}
	behaviorByID := map[int]CanonicalBehavior{}
	for _, behavior := range behaviorRecords {
		behaviorByID[behavior.BehaviorIndex] = behavior
	}
	var validated []ValidatedSpan
	seen := map[string]bool{}
	for _, proposal := range proposals {
		behavior, ok := behaviorByID[proposal.BehaviorId]
		if !ok {
			continue
		}
		expected := SourceLocator{SessionID: behavior.SessionID, Ordinal: behavior.Ordinal}
		if proposal.SourceLocator != expected || ambiguous[expected] {
			continue
		}
		content := sourceByLocator[expected]
		if content == "" || content != behavior.Content {
			continue
		}
		if proposal.Start < 0 || proposal.End <= proposal.Start || proposal.End > utf8.RuneCountInString(content) {
			continue
		}
		text := pyRuneSlice(content, proposal.Start, proposal.End)
		if text == "" || !asciiSpanHasExactWordBoundaries(content, proposal.Start, proposal.End) {
			continue
		}
		// 去重键对齐 Python 四元组 (behavior_id, locator, start, end)。
		proposalKey := fmt.Sprintf("%d|%s|%d|%d|%d",
			proposal.BehaviorId, proposal.SourceLocator.SessionID, proposal.SourceLocator.Ordinal,
			proposal.Start, proposal.End)
		if seen[proposalKey] {
			continue
		}
		seen[proposalKey] = true
		validated = append(validated, ValidatedSpan{
			BehaviorId:    proposal.BehaviorId,
			SourceLocator: proposal.SourceLocator,
			Start:         proposal.Start,
			End:           proposal.End,
			Text:          text,
		})
	}
	return validated
}

// HistoryBuildAllowedGroundedAnchors 在 Planner 运行前冻结每桶的可用锚点
// （topic_history.py:1271-1417；v5 分支 :1286-1327，v4 分支 :1330-1417）。
func HistoryBuildAllowedGroundedAnchors(units []map[string]any, behaviors []CanonicalBehavior, mappingEntries map[int]map[string]any, spanProposals []SpanProposal, policyID string) map[string]AnchorBucket {
	byIndex := map[int]CanonicalBehavior{}
	for _, behavior := range behaviors {
		byIndex[behavior.BehaviorIndex] = behavior
	}

	if policyID == v5HistoryPolicyID {
		sourceMessages := make([]map[string]any, 0, len(behaviors))
		for _, behavior := range behaviors {
			sourceMessages = append(sourceMessages, map[string]any{
				"behavior_id":    behavior.BehaviorIndex,
				"source_locator": []any{behavior.SessionID, behavior.Ordinal},
				"content":        behavior.Content,
			})
		}
		validated := HistoryValidateVerbatimSpans(sourceMessages, behaviors, spanProposals)
		spansByBehavior := map[int][]ValidatedSpan{}
		for _, span := range validated {
			spansByBehavior[span.BehaviorId] = append(spansByBehavior[span.BehaviorId], span)
		}

		v5Buckets := map[string]AnchorBucket{}
		v5Covered := map[int]bool{}
		for _, unit := range units {
			unitID := pyStr(mapAnyOf(unit["companion"])["unit_id"])
			if unitID == "" {
				continue
			}
			indices := []int{}
			for _, item := range anyListOf(mapAnyOf(unit["core"])["representative_behavior_indices"]) {
				index := intOf(item)
				if _, ok := byIndex[index]; ok {
					indices = append(indices, index)
				}
			}
			sort.Ints(indices)
			for _, index := range indices {
				v5Covered[index] = true
			}
			members := make([]CanonicalBehavior, 0, len(indices))
			for _, index := range indices {
				members = append(members, byIndex[index])
			}
			v5Buckets[unitID] = buildV5AnchorBucket(unitID, members, spansByBehavior)
		}

		v5Uncurated := map[string][]CanonicalBehavior{}
		for _, behavior := range behaviors {
			if v5Covered[behavior.BehaviorIndex] {
				continue
			}
			tag := pyStr(mapAnyOf(mappingEntries[behavior.BehaviorIndex])["tag"])
			if provisionalTagRE.MatchString(tag) {
				v5Uncurated[tag] = append(v5Uncurated[tag], behavior)
			}
		}
		for tag, members := range v5Uncurated {
			if len(members) == 1 {
				if _, exists := v5Buckets[tag]; !exists {
					v5Buckets[tag] = buildV5AnchorBucket(tag, members, spansByBehavior)
				}
			}
		}
		return applyV5MaxBucketDF(v5Buckets)
	}

	buckets := map[string]AnchorBucket{}
	covered := map[int]bool{}
	for _, unit := range units {
		unitID := pyStr(mapAnyOf(unit["companion"])["unit_id"])
		if unitID == "" {
			continue
		}
		indices := []int{}
		for _, item := range anyListOf(mapAnyOf(unit["core"])["representative_behavior_indices"]) {
			index := intOf(item)
			if _, ok := byIndex[index]; ok {
				indices = append(indices, index)
			}
		}
		sort.Ints(indices)
		for _, index := range indices {
			covered[index] = true
		}
		members := make([]CanonicalBehavior, 0, len(indices))
		for _, index := range indices {
			members = append(members, byIndex[index])
		}
		locators := make([]SourceLocator, 0, len(members))
		for _, member := range members {
			locators = append(locators, SourceLocator{SessionID: member.SessionID, Ordinal: member.Ordinal})
		}

		if len(members) == 1 {
			span := narrowPathAnchor(members[0], mappingEntries)
			if span != "" {
				buckets[unitID] = AnchorBucket{
					UnitID:                 unitID,
					Locators:               locators,
					Anchors:                []string{span},
					ExplicitSingleEvidence: true,
					IdentityAnchor:         span,
				}
				continue
			}
		}

		cleaned := make([]locatorContent, 0, len(members))
		for _, member := range members {
			cleaned = append(cleaned, locatorContent{
				locator: SourceLocator{SessionID: member.SessionID, Ordinal: member.Ordinal},
				content: cleanEvidenceContent(member.Content),
			})
		}
		buckets[unitID] = AnchorBucket{UnitID: unitID, Locators: locators, Anchors: bucketAnchors(cleaned)}
	}

	// D20 窄路径：策展尚未吸收的 provisional 标签自成桶。
	uncurated := map[string][]CanonicalBehavior{}
	for _, behavior := range behaviors {
		if covered[behavior.BehaviorIndex] {
			continue
		}
		tag := pyStr(mapAnyOf(mappingEntries[behavior.BehaviorIndex])["tag"])
		if provisionalTagRE.MatchString(tag) {
			uncurated[tag] = append(uncurated[tag], behavior)
		}
	}
	for tag, members := range uncurated {
		if len(members) != 1 {
			continue
		}
		if _, exists := buckets[tag]; exists {
			continue
		}
		span := narrowPathAnchor(members[0], mappingEntries)
		if span == "" {
			continue
		}
		buckets[tag] = AnchorBucket{
			UnitID:                 tag,
			Locators:               []SourceLocator{{SessionID: members[0].SessionID, Ordinal: members[0].Ordinal}},
			Anchors:                []string{span},
			ExplicitSingleEvidence: true,
			IdentityAnchor:         span,
		}
	}
	return buckets
}

// verbatimSpanSystemPrompt I1 逐字锚点提议器系统提示词（topic_history.py:147-152，逐字复刻）。
const verbatimSpanSystemPrompt = "你是逐字锚点位置提议器。对每条 behavior 可提议零个或多个原文片段。" +
	"只输出 JSON {\"proposals\": [...]}；每项必须且只能包含 behavior_id、" +
	"source_locator、start、end。source_locator 必须逐字复制输入，start/end 是" +
	"0-based、end-exclusive 的 Python 字符索引。不要返回片段文本。"

// ──────────────────────────── ⤴️ 31d 过渡：切片私有助手 ────────────────────────────

// pyRuneSlice Python 字符索引切片 content[start:end]。
func pyRuneSlice(content string, start, end int) string {
	runes := []rune(content)
	if start < 0 || end > len(runes) || start >= end {
		return ""
	}
	return string(runes[start:end])
}

// pyIntOf Python int() 宽转换（int/float64/数字字符串）。
func pyIntOf(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// mapAnyOf 断言 map[string]any，失败回空表（payload 往返双形态由调用方处理）。
func mapAnyOf(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// anyListOf 断言 []any，失败回 nil。
func anyListOf(value any) []any {
	if s, ok := value.([]any); ok {
		return s
	}
	return nil
}

// orValue 前值空（nil）取后值，对齐 Python `a or b` 的 None 分支。
func orValue(a, b any) any {
	if a == nil {
		return b
	}
	return a
}

// truthy 布尔真值判定（仅接受 bool）。
func truthy(value any) bool {
	b, _ := value.(bool)
	return b
}

// parseSpanProposals 仅校验 I1 可替换 API 的 schema（topic_history.py:703-758）。
func parseSpanProposals(payload any) []SpanProposal {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if !mapKeysEqual(root, []string{"proposals"}) {
		return nil
	}
	items, ok := root["proposals"].([]any)
	if !ok {
		return nil
	}
	var proposals []SpanProposal
	seen := map[string]bool{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || !mapKeysEqual(item, []string{"behavior_id", "source_locator", "start", "end"}) {
			continue
		}
		if !plainInt(item["behavior_id"]) || !plainInt(item["start"]) || !plainInt(item["end"]) {
			continue
		}
		locator, ok := item["source_locator"].([]any)
		if !ok || len(locator) != 2 {
			continue
		}
		sessionID, ok := locator[0].(string)
		if !ok || !plainInt(locator[1]) {
			continue
		}
		key := fmt.Sprintf("%d|%s|%d|%d|%d",
			intOf(item["behavior_id"]), sessionID, intOf(locator[1]),
			intOf(item["start"]), intOf(item["end"]))
		if seen[key] {
			continue
		}
		seen[key] = true
		proposals = append(proposals, SpanProposal{
			BehaviorId:    intOf(item["behavior_id"]),
			SourceLocator: SourceLocator{SessionID: sessionID, Ordinal: intOf(locator[1])},
			Start:         intOf(item["start"]),
			End:           intOf(item["end"]),
		})
	}
	return proposals
}

// mapKeysEqual 键集合恰好相等（对齐 Python set(payload) == {...}）。
func mapKeysEqual(m map[string]any, keys []string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			return false
		}
	}
	return true
}

// asciiSpanHasExactWordBoundaries ASCII 词边界校验（topic_history.py:790-797）。
// 非 ASCII 切片直接放行（对齐 Python not text.isascii() → True）。
func asciiSpanHasExactWordBoundaries(content string, start, end int) bool {
	text := pyRuneSlice(content, start, end)
	if !isASCII(text) {
		return true
	}
	runes := []rune(content)
	leftOK := start == 0 || !isWordByte(runes[start-1])
	rightOK := end == len(runes) || !isWordByte(runes[end])
	return leftOK && rightOK && keywordMatches(content, text)
}

// isWordByte ASCII 词字符判定（[A-Za-z0-9_]）。
func isWordByte(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_'
}

// isGroundingTerm 用户证据词面是否足够独特可准入（topic_starter.py:853-884）。
func isGroundingTerm(value any) bool {
	term := strings.TrimSpace(pyStr(value))
	if term == "" {
		return false
	}
	folded := strings.ToLower(term)
	if strings.Contains(folded, "openjiuwen") {
		return false
	}
	if strings.Contains(term, "话题卡") {
		return false
	}
	if tagLikeRE.MatchString(term) {
		return false
	}
	if upperSnakeRE.MatchString(term) {
		return false
	}
	cjkCount := len(cjkRE.FindAllString(term, -1))
	if cjkCount > 0 {
		return cjkCount >= 2
	}
	if !asciiTokenRE.MatchString(term) {
		return false
	}
	hasAlpha := false
	hasUpperBeyondFirst := false
	hasDigit := false
	upperTotal := 0
	runes := []rune(term)
	for i, r := range runes {
		if unicode.IsLetter(r) {
			hasAlpha = true
		}
		if unicode.IsUpper(r) {
			upperTotal++
			if i > 0 {
				hasUpperBeyondFirst = true
			}
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasAlpha {
		return false
	}
	return hasUpperBeyondFirst || hasDigit || strings.Contains(term, "-") || upperTotal >= 2
}

// cleanEvidenceContent v3 的证据清洗，逐字复用（topic_history.py:955-961）。
func cleanEvidenceContent(content string) string {
	cleaned := pyStr(content)
	runes := []rune(cleaned)
	if len(runes) > maxEvidenceChars {
		cleaned = string(runes[:maxEvidenceChars])
	}
	cleaned = markdownLinkRE.ReplaceAllString(cleaned, "]")
	cleaned = slashPathRE.ReplaceAllString(cleaned, "$1 ")
	cleaned = tagLikeRE.ReplaceAllString(cleaned, " ")
	return cleaned
}

// locatorContent 清洗证据条目（locator, content）二元组。
type locatorContent struct {
	locator SourceLocator
	content string
}

// asciiRepeatedAnchors v3 的重复标识符路径，重键到规范定位
// （topic_history.py:968-1013）。
func asciiRepeatedAnchors(cleaned []locatorContent) []positionedTerm {
	occurrences := map[string]*occurrenceEntry{}
	var order []string
	for recordIndex, item := range cleaned {
		// finditer 位置为字符偏移：先收集字节匹配再换算 rune 偏移
		for _, match := range asciiTokenRE.FindAllStringIndex(item.content, -1) {
			token := item.content[match[0]:match[1]]
			position := [2]int{recordIndex, utf8.RuneCountInString(item.content[:match[0]])}
			folded := strings.ToLower(token)
			entry, ok := occurrences[folded]
			if !ok {
				entry = &occurrenceEntry{locators: map[SourceLocator]bool{}, forms: map[string]int{}, formOrder: []string{}}
				occurrences[folded] = entry
				order = append(order, folded)
			}
			entry.locators[item.locator] = true
			if _, seen := entry.forms[token]; !seen {
				entry.formOrder = append(entry.formOrder, token)
			}
			entry.forms[token]++
			if _, seen := entry.firstFormIndex[token]; !seen {
				entry.firstFormIndex[token] = position
			}
		}
	}
	var results []positionedTerm
	for _, folded := range order {
		entry := occurrences[folded]
		if len(entry.locators) < 2 {
			continue
		}
		var strict []string
		for _, form := range entry.formOrder {
			if isGroundingTerm(form) {
				strict = append(strict, form)
			}
		}
		if len(strict) == 0 {
			continue
		}
		canonical := strict[0]
		for _, form := range strict[1:] {
			// min(key=(-count, first_form_index))
			if entry.forms[form] > entry.forms[canonical] ||
				(entry.forms[form] == entry.forms[canonical] && positionLess(entry.firstFormIndex[form], entry.firstFormIndex[canonical])) {
				canonical = form
			}
		}
		results = append(results, positionedTerm{position: entry.firstFormIndex[canonical], term: canonical})
	}
	return results
}

// occurrenceEntry 折叠词的出现登记。
type occurrenceEntry struct {
	locators       map[SourceLocator]bool
	forms          map[string]int
	formOrder      []string
	firstFormIndex map[string][2]int
}

// positionedTerm 带首现位置的词元。
type positionedTerm struct {
	position [2]int
	term     string
}

// chineseRepeatedAnchors CJK 路径：极大重复字面子串（topic_history.py:1017-1114）。
func chineseRepeatedAnchors(cleaned []locatorContent) []positionedTerm {
	type run struct {
		locator SourceLocator
		text    []rune
	}
	var runs []run
	runOrigin := map[[2]int][2]int{} // (runID, offsetInRun) → (recordIndex, charOffset)
	for recordIndex, item := range cleaned {
		for _, match := range cjkRunRE.FindAllStringIndex(item.content, -1) {
			runes := []rune(item.content[match[0]:match[1]])
			startRune := utf8.RuneCountInString(item.content[:match[0]])
			runs = append(runs, run{locator: item.locator, text: runes})
			for offset := range runes {
				runOrigin[[2]int{len(runs) - 1, offset}] = [2]int{recordIndex, startRune + offset}
			}
		}
	}
	supportOf := func(occurrences [][2]int) map[SourceLocator]bool {
		support := map[SourceLocator]bool{}
		for _, occ := range occurrences {
			support[runs[occ[0]].locator] = true
		}
		return support
	}
	seeds := map[rune][][2]int{}
	for runID, r := range runs {
		for offset, character := range r.text {
			seeds[character] = append(seeds[character], [2]int{runID, offset})
		}
	}
	var results []positionedTerm
	type frame struct {
		length      int
		occurrences [][2]int
	}
	var stack []frame
	for _, occurrences := range seeds {
		if len(supportOf(occurrences)) >= 2 {
			stack = append(stack, frame{length: 1, occurrences: occurrences})
		}
	}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		length, occurrences := current.length, current.occurrences
		support := supportOf(occurrences)

		// 折叠唯一右延伸链：中间长度支持相同，均非极大。
		for {
			nextCharacters := map[rune]bool{}
			extendable := true
			for _, occ := range occurrences {
				text := runs[occ[0]].text
				if occ[1]+length >= len(text) {
					extendable = false
					break
				}
				nextCharacters[text[occ[1]+length]] = true
				if len(nextCharacters) > 1 {
					break
				}
			}
			if !extendable || len(nextCharacters) != 1 {
				break
			}
			length++
		}

		children := map[rune][][2]int{}
		for _, occ := range occurrences {
			text := runs[occ[0]].text
			if occ[1]+length < len(text) {
				children[text[occ[1]+length]] = append(children[text[occ[1]+length]], occ)
			}
		}
		preceding := map[rune][][2]int{}
		for _, occ := range occurrences {
			if occ[1] > 0 {
				preceding[runs[occ[0]].text[occ[1]-1]] = append(preceding[runs[occ[0]].text[occ[1]-1]], occ)
			}
		}
		extendsWithSameSupport := false
		for _, group := range children {
			if sameSupport(supportOf(group), support) {
				extendsWithSameSupport = true
				break
			}
		}
		if !extendsWithSameSupport {
			for _, group := range preceding {
				if sameSupport(supportOf(group), support) {
					extendsWithSameSupport = true
					break
				}
			}
		}
		if length >= 2 && !extendsWithSameSupport {
			first := occurrences[0]
			piece := string(runs[first[0]].text[first[1] : first[1]+length])
			position := [2]int{-1, -1}
			for _, occ := range occurrences {
				origin := runOrigin[occ]
				if position[0] == -1 || origin[0] < position[0] ||
					(origin[0] == position[0] && origin[1] < position[1]) {
					position = origin
				}
			}
			results = append(results, positionedTerm{position: position, term: piece})
		}
		for _, group := range children {
			if len(supportOf(group)) >= 2 {
				stack = append(stack, frame{length: length + 1, occurrences: group})
			}
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if a.position != b.position {
			return a.position[0] < b.position[0] || (a.position[0] == b.position[0] && a.position[1] < b.position[1])
		}
		if len(a.term) != len(b.term) {
			return len(a.term) > len(b.term)
		}
		return a.term < b.term
	})
	return results
}

// sameSupport 支持集相等判定。
func sameSupport(a, b map[SourceLocator]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if !b[key] {
			return false
		}
	}
	return true
}

// bucketAnchors 合并 ASCII/CJK 重复词并按准入过滤保序（topic_history.py:1117-1126）。
func bucketAnchors(cleaned []locatorContent) []string {
	merged := append(asciiRepeatedAnchors(cleaned), chineseRepeatedAnchors(cleaned)...)
	sort.SliceStable(merged, func(i, j int) bool {
		a, b := merged[i], merged[j]
		if a.position != b.position {
			return a.position[0] < b.position[0] || (a.position[0] == b.position[0] && a.position[1] < b.position[1])
		}
		return len([]rune(a.term)) > len([]rune(b.term))
	})
	var ordered []string
	for _, item := range merged {
		if !containsString(ordered, item.term) && isGroundingTerm(item.term) {
			ordered = append(ordered, item.term)
		}
	}
	return ordered
}

// validLiteralFocusSpan 窄路径字面焦点切片校验（topic_history.py:664-673）。
func validLiteralFocusSpan(span any, content string) bool {
	value, ok := span.(string)
	if !ok {
		return false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.Contains(content, trimmed) {
		return false
	}
	if len(cjkRE.FindAllString(trimmed, -1)) < 2 && !asciiTokenRE.MatchString(trimmed) {
		return false
	}
	return isGroundingTerm(trimmed)
}

// narrowPathAnchor D20 explicit-single-evidence 窄路径（topic_history.py:1128-1141）。
func narrowPathAnchor(member CanonicalBehavior, mappingEntries map[int]map[string]any) string {
	entry := mappingEntries[member.BehaviorIndex]
	if pyStr(entry["action_type"]) != "explicit_goal_preference_task" {
		return ""
	}
	if !validLiteralFocusSpan(entry["literal_focus_span"], member.Content) {
		return ""
	}
	return strings.TrimSpace(pyStr(entry["literal_focus_span"]))
}

// v5AnchorTextAllowed v5 锚点文本准入（topic_history.py:1144-1150）。
func v5AnchorTextAllowed(text string) bool {
	if text == "" || text != strings.TrimSpace(text) {
		return false
	}
	cjkCount := len(cjkRE.FindAllString(text, -1))
	if cjkCount > 0 {
		return cjkCount >= v5MinCJKChars
	}
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// firstExactOccurrence 锚点在正文中的首个词边界精确出现（topic_history.py:1153-1161）。
func firstExactOccurrence(content, anchor string) (int, int, bool) {
	runes := []rune(content)
	anchorRunes := []rune(anchor)
	for start := runesIndex(runes, anchorRunes, 0); start >= 0; start = runesIndex(runes, anchorRunes, start+1) {
		end := start + len(anchorRunes)
		if asciiSpanHasExactWordBoundaries(content, start, end) {
			return start, end, true
		}
	}
	return 0, 0, false
}

// runesIndex 从 from 起在 rune 切片中找子串首现位置。
func runesIndex(haystack, needle []rune, from int) int {
	if len(needle) == 0 || from+len(needle) > len(haystack) {
		return -1
	}
	for i := from; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// repeatedAnchorEvidence 锚点在成员正文的重复字面证据（topic_history.py:1164-1179）。
func repeatedAnchorEvidence(anchor string, members []CanonicalBehavior) []AnchorEvidence {
	var evidence []AnchorEvidence
	for _, member := range members {
		start, end, ok := firstExactOccurrence(member.Content, anchor)
		if !ok {
			continue
		}
		evidence = append(evidence, AnchorEvidence{
			Origin:        "repeated_literal",
			SourceLocator: SourceLocator{SessionID: member.SessionID, Ordinal: member.Ordinal},
			Start:         start,
			End:           end,
		})
	}
	return evidence
}

// buildV5AnchorBucket 组装 v5 锚点桶（topic_history.py:1182-1236）。
func buildV5AnchorBucket(unitID string, members []CanonicalBehavior, spansByBehavior map[int][]ValidatedSpan) AnchorBucket {
	cleaned := make([]locatorContent, 0, len(members))
	for _, member := range members {
		cleaned = append(cleaned, locatorContent{
			locator: SourceLocator{SessionID: member.SessionID, Ordinal: member.Ordinal},
			content: cleanEvidenceContent(member.Content),
		})
	}
	repeated := bucketAnchors(cleaned)
	var anchors []string
	evidenceByAnchor := map[string][]AnchorEvidence{}

	for _, member := range members {
		for _, span := range spansByBehavior[member.BehaviorIndex] {
			if !v5AnchorTextAllowed(span.Text) {
				continue
			}
			if !containsString(anchors, span.Text) {
				anchors = append(anchors, span.Text)
			}
			evidenceByAnchor[span.Text] = append(evidenceByAnchor[span.Text], AnchorEvidence{
				Origin:        "model_span",
				SourceLocator: span.SourceLocator,
				Start:         span.Start,
				End:           span.End,
			})
		}
	}

	for _, anchor := range repeated {
		if !v5AnchorTextAllowed(anchor) {
			continue
		}
		occurrences := repeatedAnchorEvidence(anchor, members)
		locators := map[SourceLocator]bool{}
		for _, item := range occurrences {
			locators[item.SourceLocator] = true
		}
		if len(locators) < 2 {
			continue
		}
		if !containsString(anchors, anchor) {
			anchors = append(anchors, anchor)
		}
		evidenceByAnchor[anchor] = append(evidenceByAnchor[anchor], occurrences...)
	}

	frozenEvidence := map[string][]AnchorEvidence{}
	for _, anchor := range anchors {
		// 对齐 Python dict.fromkeys 去重保序
		seen := map[AnchorEvidence]bool{}
		var unique []AnchorEvidence
		for _, item := range evidenceByAnchor[anchor] {
			if !seen[item] {
				seen[item] = true
				unique = append(unique, item)
			}
		}
		frozenEvidence[anchor] = unique
	}
	single := len(members) == 1 && len(anchors) == 1
	identity := ""
	if single {
		identity = anchors[0]
	}
	return AnchorBucket{
		UnitID:                 unitID,
		Locators:               locatorsOf(members),
		Anchors:                anchors,
		ExplicitSingleEvidence: single,
		IdentityAnchor:         identity,
		AnchorEvidence:         frozenEvidence,
		PolicyId:               v5HistoryPolicyID,
		ValidationStatus:       v5ValidationStatus,
	}
}

// locatorsOf 成员定位元组。
func locatorsOf(members []CanonicalBehavior) []SourceLocator {
	locators := make([]SourceLocator, 0, len(members))
	for _, member := range members {
		locators = append(locators, SourceLocator{SessionID: member.SessionID, Ordinal: member.Ordinal})
	}
	return locators
}

// applyV5MaxBucketDF 跨桶文档频率上限过滤（topic_history.py:1239-1266）。
func applyV5MaxBucketDF(buckets map[string]AnchorBucket) map[string]AnchorBucket {
	bucketCount := len(buckets)
	if bucketCount <= 1 {
		return buckets
	}
	documentFrequency := map[string]int{}
	for _, bucket := range buckets {
		unique := map[string]bool{}
		for _, anchor := range bucket.Anchors {
			unique[anchor] = true
		}
		for anchor := range unique {
			documentFrequency[anchor]++
		}
	}
	filtered := make(map[string]AnchorBucket, len(buckets))
	for unitID, bucket := range buckets {
		var anchors []string
		for _, anchor := range bucket.Anchors {
			if float64(documentFrequency[anchor])/float64(bucketCount) <= v5MaxBucketDF {
				anchors = append(anchors, anchor)
			}
		}
		single := len(bucket.Locators) == 1 && len(anchors) == 1
		identity := ""
		if single {
			identity = anchors[0]
		}
		evidence := map[string][]AnchorEvidence{}
		for _, anchor := range anchors {
			if item, ok := bucket.AnchorEvidence[anchor]; ok {
				evidence[anchor] = item
			}
		}
		filtered[unitID] = AnchorBucket{
			UnitID:                 bucket.UnitID,
			Locators:               bucket.Locators,
			Anchors:                anchors,
			ExplicitSingleEvidence: single,
			IdentityAnchor:         identity,
			AnchorEvidence:         evidence,
			PolicyId:               v5HistoryPolicyID,
			ValidationStatus:       v5ValidationStatus,
		}
	}
	return filtered
}

// ──────────────────────────── ⤴️ 31d 过渡：引擎缝 fake ────────────────────────────

// fakeHistoryEngine 过渡期引擎：仅实现空历史的确定性路径（无记录 → 重扫描无新行为
// → 映射跳过 → 门不触发 → ok）。任何真实历史刷新都必须等 31d 的
// TopicHistoryEngine（用户 2026-11-13 拍板，31d 计划文档须标注待回填）。
type fakeHistoryEngine struct {
	store *TopicStarterStore
}

// NewFakeHistoryEngine 构造过渡引擎 fake（⤴️ 31d 删除）。
func NewFakeHistoryEngine(store *TopicStarterStore) HistoryEngine {
	return &fakeHistoryEngine{store: store}
}

// RefreshV5State 空历史确定性路径（对齐 refresh :3473-3620 的零记录分支语义）。
func (e *fakeHistoryEngine) RefreshV5State(_ context.Context, req HistoryRefreshRequest) HistoryRefreshOutcome {
	previous, ok := HistoryLoadLatestV4(e.store)
	var state map[string]any
	if ok {
		state = deepCopyMap(previous)
	} else {
		state = HistoryEmptyV4State()
	}
	state["schema_version"] = v4SnapshotSchema
	state["policy_id"] = v4HistoryPolicyID
	records := map[string][]map[string]any{}
	if req.RecordsLoader != nil {
		records = req.RecordsLoader()
	}
	if len(records) > 0 {
		return HistoryRefreshOutcome{Status: "blocked", Reason: "history_engine_transitional"}
	}
	return HistoryRefreshOutcome{Status: "ok", State: state}
}

// positionLess 首现位置二元组比较。
func positionLess(a, b [2]int) bool {
	return a[0] < b[0] || (a[0] == b[0] && a[1] < b[1])
}
