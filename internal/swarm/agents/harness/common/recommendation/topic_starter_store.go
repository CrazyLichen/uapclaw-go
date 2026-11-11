// Package recommendation 的 topic 推荐事件账本存储。
// 本文件对应上游 topic_starter.py 的前两段：
//   - InterestDrift + 漂移纯函数（:100-176）
//   - TopicStarterStore（:178-513）
//
// TopicStarterService 与画像函数（:515-1953）⤵️ 10.3.31d，另建 topic_starter_service.go。
//
// 存储层刻意使用 database/sql + mattn/go-sqlite3 裸 SQL，不引入 GORM：
// 上游是 import sqlite3 的裸 SQL，query_events 的 json_extract/IS 比较与 4 个表达式索引
// 均无法用 ORM 链式 API 表达，改用 GORM 会退化为一层 db.Raw 且破坏与上游逐行对拍的能力
// （详见 spec §2.1，勿"顺手优化"）。
package recommendation

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterestDrift 兴趣漂移三信号结果（对齐 topic_starter.py:100）。
type InterestDrift struct {
	// EntropyDelta 前后熵差的绝对值。
	EntropyDelta float64
	// JaccardDistance 1 - 交集/并集，完全重叠为 0、完全不重叠为 1。
	JaccardDistance float64
	// JSDivergence 两分布的 Jensen-Shannon 散度。
	JSDivergence float64
	// Score 三信号加权和（0.4/0.3/0.3）。
	Score float64
	// ShouldUpdate Score 超过阈值（严格大于 0.8）时为真。
	ShouldUpdate bool
}

// TopicStarterStore topic 推荐的本地单用户事件账本（对齐 topic_starter.py:178）。
// 两张表：profile_snapshot（画像快照，版本自增）与 recommendation_event（事件账本）。
type TopicStarterStore struct {
	// path 数据库文件路径。
	path string
}

// PayloadIntegerLTE payload 整数上界过滤条件。
type PayloadIntegerLTE struct {
	// Field payload 内字段名（须在白名单内）。
	Field string
	// Value 上界值（含）。
	Value int
}

// QueryEventsOptions query_events 的查询条件。
// Python: topic_starter.py:578-592 的 keyword-only 参数与其默认值。
type QueryEventsOptions struct {
	// EventType 事件类型，必填；空串直接返回 nil。
	EventType string
	// SlateID 迭代批次过滤，nil 表示不加条件。
	SlateID *string
	// TopicID 单个话题过滤，nil 表示不加条件。
	TopicID *string
	// PolicyID 策略过滤，nil 表示不加条件。
	PolicyID *string
	// PayloadEquals payload 等值过滤（用 SQL IS 语义，可匹配 JSON null）。
	PayloadEquals map[string]any
	// PayloadIntegerLTE payload 整数上界过滤。
	PayloadIntegerLTE *PayloadIntegerLTE
	// PayloadPresent 要求 payload 内该键非 SQL NULL。
	PayloadPresent *string
	// OrderByPayloadIntegerDesc 按该 payload 字段的整数降序作为主排序前缀。
	OrderByPayloadIntegerDesc *string
	// Limit 返回条数上限，默认 100；小于 1 返回 nil。
	Limit int
	// Offset 跳过条数，负数返回 nil。
	Offset int
	// NewestFirst 为真时按 created_at/event_key 降序。
	NewestFirst bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sqliteBusyTimeoutSeconds SQLite 写锁等待秒数（对齐 topic_starter.py:74）。
	sqliteBusyTimeoutSeconds = 5.0
	// supersededSummaryKeep 每个会话保留的最新 summary 条数（:95）。
	supersededSummaryKeep = 2
	// profileSnapshotRetentionDays 画像快照留存天数（:96）。
	profileSnapshotRetentionDays = 90
	// shortIntentRetentionDays 短期意图状态留存天数（:97）。
	shortIntentRetentionDays = 90
	// pruneMinIntervalSeconds 两次清理的最小间隔（:98，20 小时）。
	pruneMinIntervalSeconds = 20 * 60 * 60
	// pruneEventType 清理自身的事件类型（:99）。
	pruneEventType = "store_pruned"
	// v4SnapshotSchema 需要跳过的 v4 快照标记（:49）。
	v4SnapshotSchema = "topic-starter-v4"
	// driftThreshold 兴趣漂移判定阈值（:41）。
	driftThreshold = 0.8
	// daySeconds 一天的秒数。
	daySeconds = 24 * 60 * 60
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// driftWeights 三信号权重（对齐 topic_starter.py:40）。
	driftWeights = [3]float64{0.4, 0.3, 0.3}

	// retentionDays 分层留存 TTL（对齐 topic_starter.py:80-95，共 16 项）。
	// 未列出的 event_type 永不按龄删除（例如 session_summary 保最新 2 条由助手单独裁剪）。
	retentionDays = map[string]int{
		"dynamic_exposure":               30,
		"starter_slate_manifest":         45,
		"starter_card":                   45,
		"provider_invocation":            180,
		"external_invocation_attempt":    180,
		"rid_summary_pair_match_attempt": 180,
		"rid_summary_pair_match_receipt": 180,
		"rid_decision":                   180,
		"rid_abstention":                 180,
		"rid_user_context_snapshot":      180,
		"session_summary_coverage":       90,
		"state_maintenance_receipt":      90,
		"prompt_suggestion_generation":   90,
		"conversation_depth":             180,
		"active_expression_observation":  180,
		"store_pruned":                   30,
	}

	// eventPayloadQueryFields query_events 允许过滤的 payload 字段白名单（:57-71，13 项）。
	// 字段名会被直接拼进 SQL，必须先经此白名单校验。
	eventPayloadQueryFields = map[string]struct{}{
		"card_id":                              {},
		"context_scope":                        {},
		"generation_trigger_key":               {},
		"negative_preference_version":          {},
		"project_id":                           {},
		"serving_compatibility_key":            {},
		"short_intent_version":                 {},
		"source_starter_id":                    {},
		"model":                                {},
		"provider_response_id":                 {},
		"status":                               {},
		"generation_trigger_key.context_scope": {},
		"generation_trigger_key.model":         {},
	}
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// pythonTruthy 复刻 Python 真值判断（topic_starter.py:486 的 truthy 语义）。
// nil / 空串 / 零值 / false / 空容器为假，其余为真。
func pythonTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int8:
		return v != 0
	case int16:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case uint:
		return v != 0
	case uint8:
		return v != 0
	case uint16:
		return v != 0
	case uint32:
		return v != 0
	case uint64:
		return v != 0
	case float32:
		return v != 0
	case float64:
		return v != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

// pyStr 复刻 Python str() 语义（topic_starter.py:483 的 `str(x or "")`）。
// nil 与空串都得到空串；bool 得到 "True"/"False"；数值按 Python 排版。
func pyStr(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		// Python str(float) 用最短往返表示：整值补 ".0"，其余按 %g 风格
		if math.IsInf(v, 1) {
			return "inf"
		}
		if math.IsInf(v, -1) {
			return "-inf"
		}
		if math.IsNaN(v) {
			return "nan"
		}
		formatted := strconv.FormatFloat(v, 'g', -1, 64)
		// Go 把负零格式化为 "-0"，Python 给 "-0.0"
		if formatted == "-0" {
			formatted = "-0.0"
		}
		// 非指数形式且无小数点时补 ".0"（Python str(3.0) == "3.0"）
		if !strings.ContainsAny(formatted, ".eE") {
			formatted += ".0"
		}
		// Python 指数至少两位：1e-07 而非 1e-7
		if index := strings.IndexAny(formatted, "eE"); index >= 0 {
			mantissa, exponent := formatted[:index], formatted[index+1:]
			sign := ""
			if len(exponent) > 0 && (exponent[0] == '+' || exponent[0] == '-') {
				sign, exponent = exponent[:1], exponent[1:]
			}
			// 指数已带前导零时保持；否则补齐到两位
			if len(exponent) < 2 {
				exponent = "0" + exponent
			}
			formatted = mantissa + "e" + sign + exponent
		}
		return formatted
	default:
		return fmt.Sprintf("%v", v)
	}
}

// marshalNoEscape 序列化为紧凑 JSON，不转义 HTML 且不追加换行。
// Python: json.dumps(value, ensure_ascii=False, separators=(",", ":"))
func marshalNoEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	out := buffer.Bytes()
	// Encoder 会追加换行；上游 dumps 不带尾换行
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}

// round12 四舍五入到 12 位小数（对齐 Python round(x, 12)）。
func round12(value float64) float64 {
	return math.Round(value*1e12) / 1e12
}

// ensureParentDir 创建父目录（对齐 topic_starter.py:183 的 mkdir(parents=True)）。
func ensureParentDir(path string) error {
	parent := filepath.Dir(path)
	if parent == "" || parent == "." {
		return nil
	}
	return os.MkdirAll(parent, 0o755)
}

// sortedKeys 取 map 键并排序，用于稳定的错误消息。
func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedStringSet 取字符串集合的元素并排序，用于稳定的错误消息。
// 对齐 Python 侧 payload_fields 的 set 语义（只有键有意义）。
func sortedStringSet(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// nowSeconds 当前时间的 Unix 秒浮点（对齐 Python time.time()）。
func nowSeconds() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// interestDistribution 把标签列表归一化为概率分布。
// Python: topic_starter.py:113 _distribution（先 strip 再过滤空串）。
func interestDistribution(labels []string) map[string]float64 {
	counts := make(map[string]float64)
	total := 0.0
	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" {
			continue
		}
		counts[trimmed]++
		total++
	}
	if total == 0 {
		return map[string]float64{}
	}
	for label, count := range counts {
		counts[label] = count / total
	}
	return counts
}

// interestEntropy 计算香农熵（底 2）。
// Python: topic_starter.py:118 _entropy
func interestEntropy(distribution map[string]float64) float64 {
	entropy := 0.0
	for _, probability := range distribution {
		if probability <= 0 {
			continue
		}
		entropy -= probability * math.Log2(probability)
	}
	return entropy
}

// jsDivergence 计算两分布的 Jensen-Shannon 散度。
// Python: topic_starter.py:122 _js_divergence
func jsDivergence(previous, current map[string]float64) float64 {
	labels := make(map[string]struct{}, len(previous)+len(current))
	for label := range previous {
		labels[label] = struct{}{}
	}
	for label := range current {
		labels[label] = struct{}{}
	}
	if len(labels) == 0 {
		return 0.0
	}
	midpoint := make(map[string]float64, len(labels))
	for label := range labels {
		midpoint[label] = (previous[label] + current[label]) / 2
	}
	kl := func(source map[string]float64) float64 {
		sum := 0.0
		for label, probability := range source {
			if probability > 0 && midpoint[label] > 0 {
				sum += probability * math.Log2(probability/midpoint[label])
			}
		}
		return sum
	}
	return (kl(previous) + kl(current)) / 2
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTopicStarterStore 打开（或创建）事件账本库并确保表结构存在。
// Python: topic_starter.py:181 __init__
func NewTopicStarterStore(path string) (*TopicStarterStore, error) {
	if err := ensureParentDir(path); err != nil {
		return nil, fmt.Errorf("创建推荐库父目录失败: %w", err)
	}
	store := &TopicStarterStore{path: path}
	if err := store.createTables(); err != nil {
		return nil, fmt.Errorf("创建推荐库表结构失败: %w", err)
	}
	return store, nil
}

// Path 返回数据库文件路径（测试与诊断用）。
func (s *TopicStarterStore) Path() string {
	return s.path
}

// Close 关闭底层连接池。
// 每次 connect() 新建的 *sql.DB 由各方法自行关闭；本方法保留为对称 API，
// 与上游 `with self._connect()` 的用后即关语义一致。
func (s *TopicStarterStore) Close() error {
	return nil
}

// EventKeyOf 计算事件主键：session_id|slate_id|event_type|topic_id[|feedback_record_id]。
// Python: topic_starter.py:486 event_key_of
//
// 第五段按 Python 真值语义判定：feedback_record_id 为假（nil/空串/0/false）时不追加，
// 因为同一张卡上的反馈可以重复发生，需要用反馈记录 ID 区分。
func (s *TopicStarterStore) EventKeyOf(event map[string]any) string {
	parts := []string{
		pyStr(event["session_id"]),
		pyStr(event["slate_id"]),
		pyStr(event["event_type"]),
		pyStr(event["topic_id"]),
	}
	if pythonTruthy(event["feedback_record_id"]) {
		parts = append(parts, pyStr(event["feedback_record_id"]))
	}
	return strings.Join(parts, "|")
}

// RecordEvent 追加一条事件，幂等（同 key 已存在则返回 false）。
// Python: topic_starter.py:498 record_event
func (s *TopicStarterStore) RecordEvent(event map[string]any) (bool, error) {
	return s.insertEvent(s.EventKeyOf(event), event, true)
}

// RecordEventsAtomic 整组写入或整组不写。
// Python: topic_starter.py:527 record_events_atomic
//
// 三条短路：空列表 → false；组内 event_key 重复 → false（写库前拦截）；
// 落库撞唯一约束 → 回滚并返回 false（IntegrityError 不向上抛）。
func (s *TopicStarterStore) RecordEventsAtomic(events []map[string]any) (bool, error) {
	if len(events) == 0 {
		return false, nil
	}
	keys := make([]string, 0, len(events))
	seen := make(map[string]struct{}, len(events))
	for _, event := range events {
		key := s.EventKeyOf(event)
		if _, duplicate := seen[key]; duplicate {
			return false, nil
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	db, err := s.connect()
	if err != nil {
		return false, err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	statement := "INSERT INTO recommendation_event(" + insertEventKeyColumns + ") VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	for index, event := range events {
		args, argErr := eventArgs(keys[index], event)
		if argErr != nil {
			_ = tx.Rollback()
			return false, argErr
		}
		if _, execErr := tx.Exec(statement, args...); execErr != nil {
			_ = tx.Rollback()
			// Python 只捕 sqlite3.IntegrityError；Go 侧对应约束类错误
			var sqliteErr sqlite3.Error
			if errors.As(execErr, &sqliteErr) && sqliteErr.Code == sqlite3.ErrConstraint {
				return false, nil
			}
			return false, execErr
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// QueryEvents 按条件读取有界事件切片，不做全表扫描。
// Python: topic_starter.py:578 query_events
//
// 四条守卫按上游顺序提前返回：event_type 为空、limit<1、offset<0 均返回 nil；
// payload 查询字段不在白名单内返回 error（字段名会被拼进 SQL，必须先校验）。
func (s *TopicStarterStore) QueryEvents(opts QueryEventsOptions) ([]map[string]any, error) {
	if opts.EventType == "" {
		return nil, nil
	}
	if opts.Limit < 1 {
		return nil, nil
	}
	if opts.Offset < 0 {
		return nil, nil
	}

	// Python 侧 payload_fields 是 set（只有键有意义），这里用 struct{} 集合表达
	touched := make(map[string]struct{}, len(opts.PayloadEquals)+3)
	for field := range opts.PayloadEquals {
		touched[field] = struct{}{}
	}
	if opts.PayloadPresent != nil {
		touched[*opts.PayloadPresent] = struct{}{}
	}
	if opts.PayloadIntegerLTE != nil {
		touched[opts.PayloadIntegerLTE.Field] = struct{}{}
	}
	if opts.OrderByPayloadIntegerDesc != nil {
		touched[*opts.OrderByPayloadIntegerDesc] = struct{}{}
	}
	unknown := make([]string, 0)
	for _, field := range sortedStringSet(touched) {
		if _, ok := eventPayloadQueryFields[field]; !ok {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unsupported event payload query fields: %v", unknown)
	}

	clauses := []string{"event_type = ?"}
	values := []any{opts.EventType}
	if opts.SlateID != nil {
		clauses = append(clauses, "slate_id = ?")
		values = append(values, *opts.SlateID)
	}
	if opts.TopicID != nil {
		clauses = append(clauses, "topic_id = ?")
		values = append(values, *opts.TopicID)
	}
	if opts.PolicyID != nil {
		clauses = append(clauses, "policy_id = ?")
		values = append(values, *opts.PolicyID)
	}
	for _, field := range sortedKeys(opts.PayloadEquals) {
		clauses = append(clauses, "json_extract(payload_json, '$."+field+"') IS json_extract(?, '$')")
		encoded, err := marshalNoEscape(opts.PayloadEquals[field])
		if err != nil {
			return nil, fmt.Errorf("序列化 payload 等值条件失败: %w", err)
		}
		values = append(values, string(encoded))
	}
	if opts.PayloadIntegerLTE != nil {
		clauses = append(clauses, "CAST(json_extract(payload_json, '$."+opts.PayloadIntegerLTE.Field+"') AS INTEGER) <= ?")
		values = append(values, opts.PayloadIntegerLTE.Value)
	}
	if opts.PayloadPresent != nil {
		clauses = append(clauses, "json_extract(payload_json, '$."+*opts.PayloadPresent+"') IS NOT NULL")
	}

	direction := "ASC"
	if opts.NewestFirst {
		direction = "DESC"
	}
	ordering := ""
	if opts.OrderByPayloadIntegerDesc != nil {
		ordering = "CAST(json_extract(payload_json, '$." + *opts.OrderByPayloadIntegerDesc + "') AS INTEGER) DESC, "
	}

	values = append(values, opts.Limit, opts.Offset)
	statement := "SELECT * FROM recommendation_event WHERE " +
		strings.Join(clauses, " AND ") +
		" ORDER BY " + ordering + "created_at " + direction + ", event_key " + direction +
		" LIMIT ? OFFSET ?"

	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(statement, values...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// SaveSnapshot 保存一版画像快照，版本号在库内自增并回写到存储内容。
// Python: topic_starter.py:437 save_snapshot
func (s *TopicStarterStore) SaveSnapshot(snapshot map[string]any, evidenceUntil float64) (map[string]any, error) {
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var version int
	if err := db.QueryRow("SELECT COALESCE(MAX(version), 0) + 1 FROM profile_snapshot").Scan(&version); err != nil {
		return nil, err
	}

	createdAt := nowSeconds()
	stored := make(map[string]any, len(snapshot)+3)
	for key, value := range snapshot {
		stored[key] = value
	}
	stored["version"] = version
	stored["updated_at"] = createdAt
	stored["evidence_until"] = evidenceUntil

	encoded, err := marshalNoEscape(stored)
	if err != nil {
		return nil, fmt.Errorf("序列化画像快照失败: %w", err)
	}
	if _, err := db.Exec(
		"INSERT INTO profile_snapshot(version, created_at, evidence_until, snapshot_json) VALUES(?, ?, ?, ?)",
		version, createdAt, evidenceUntil, string(encoded),
	); err != nil {
		return nil, err
	}
	return stored, nil
}

// LoadLatestSnapshot 读取最新的 v3 可读快照，跳过显式 v4 行。
// Python: topic_starter.py:469 load_latest_snapshot
func (s *TopicStarterStore) LoadLatestSnapshot() (map[string]any, error) {
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT snapshot_json FROM profile_snapshot ORDER BY version DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var raw any
		if err := rows.Scan(&raw); err != nil {
			return nil, err
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
		if schema, ok := decoded["schema_version"]; ok && pyStr(schema) == v4SnapshotSchema {
			continue
		}
		return decoded, nil
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

// Prune 按分层 TTL 清理缓存与收据，返回各类删除条数。
// Python: topic_starter.py:233 prune
//
// 20 小时内只执行一次（由 store_pruned 事件的时间戳节流）。
// 点击/绑定/否定/会话摘要/同意记录/训练数据不在 retentionDays 内，永不按龄删除；
// 过期的会话摘要另由 pruneSupersededSummaries 按"每会话保最新 2 条"裁剪。
// 无论是否删到东西，末尾都会写一条 store_pruned 事件。
func (s *TopicStarterStore) Prune(now float64) (map[string]int, error) {
	recent, err := s.QueryEvents(QueryEventsOptions{
		EventType:   pruneEventType,
		Limit:       1,
		NewestFirst: true,
	})
	if err != nil {
		return nil, err
	}
	if len(recent) > 0 {
		createdAt, _ := recent[0]["created_at"].(float64)
		if createdAt > now-pruneMinIntervalSeconds {
			return map[string]int{}, nil
		}
	}

	removed := map[string]int{}
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	for eventType, days := range retentionDays {
		result, execErr := tx.Exec(
			"DELETE FROM recommendation_event WHERE event_type = ? AND created_at < ?",
			eventType, now-float64(days)*daySeconds,
		)
		if execErr != nil {
			_ = tx.Rollback()
			return nil, execErr
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected > 0 {
			removed[eventType] = int(affected)
		}
	}
	superseded, err := pruneSupersededSummaries(tx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if superseded > 0 {
		removed["session_summary_superseded"] = int(superseded)
	}
	snapshots, err := pruneProfileSnapshots(tx, now)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if snapshots > 0 {
		removed["profile_snapshot"] = int(snapshots)
	}
	shortIntent, err := pruneShortIntent(tx, now)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if shortIntent > 0 {
		removed["short_intent_state"] = int(shortIntent)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// VACUUM 不能在事务内执行：必须在 Commit 之后单独发出
	if len(removed) > 0 {
		if _, err := db.Exec("VACUUM"); err != nil {
			return nil, err
		}
	}

	if _, err := s.RecordEvent(map[string]any{
		"session_id":        "maintenance",
		"slate_id":          "maintenance",
		"event_type":        pruneEventType,
		"topic_id":          fmt.Sprintf("maintenance:%d", int(now)),
		"policy_id":         "recommendation-retention-v1",
		"selection_mode":    "usage_retention",
		"propensity_reason": "not_applicable",
		"payload":           removed,
		"created_at":        now,
	}); err != nil {
		return nil, err
	}
	return removed, nil
}

// DeleteRecommendationData 清空本地派生内容，只保留非个人化的用量事实。
// Python: topic_starter.py:186 delete_recommendation_data
//
// 步骤：先把 provider_invocation 压成 9 键收据 → PRAGMA secure_delete 后清两表
// （short_intent_state 存在才清）→ 以 session_id="usage" 回填收据 →
// 写 recommendation_data_deleted 记录 exposures/cards 计数 → VACUUM。
func (s *TopicStarterStore) DeleteRecommendationData() error {
	type receipt struct {
		createdAt float64
		payload   map[string]any
	}
	receipts := map[string]receipt{}
	exposuresRemoved := 0
	cardsRemoved := 0

	events, err := s.ListEvents()
	if err != nil {
		return err
	}
	keyItems := []string{
		"attempt_id", "provider_response_id", "usage", "status", "stage",
		"provider", "model", "latency_ms", "failure_reason",
	}
	for _, event := range events {
		payload, _ := event["payload"].(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		if pyStr(event["event_type"]) == "dynamic_exposure" {
			exposuresRemoved++
			if raw, ok := payload["card_revision_ids"].([]any); ok {
				cardsRemoved += len(raw)
			}
		}
		if pyStr(event["event_type"]) != "provider_invocation" {
			continue
		}
		attempt := pyStr(payload["attempt_id"])
		if !pythonTruthy(attempt) {
			continue
		}
		createdAt, _ := event["created_at"].(float64)
		trimmed := map[string]any{}
		for _, key := range keyItems {
			if value, ok := payload[key]; ok {
				trimmed[key] = value
			}
		}
		receipts[attempt] = receipt{createdAt: createdAt, payload: trimmed}
	}

	db, err := s.connect()
	if err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA secure_delete = ON"); err != nil {
		db.Close()
		return err
	}
	if _, err := db.Exec("DELETE FROM profile_snapshot"); err != nil {
		db.Close()
		return err
	}
	if _, err := db.Exec("DELETE FROM recommendation_event"); err != nil {
		db.Close()
		return err
	}
	var probe int
	probeErr := db.QueryRow(
		"SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'short_intent_state'",
	).Scan(&probe)
	if probeErr == nil {
		if _, err := db.Exec("DELETE FROM short_intent_state"); err != nil {
			db.Close()
			return err
		}
	} else if !errors.Is(probeErr, sql.ErrNoRows) {
		db.Close()
		return probeErr
	}
	db.Close()

	for attempt, item := range receipts {
		if _, err := s.RecordEvent(map[string]any{
			"session_id":        "usage",
			"slate_id":          "usage",
			"event_type":        "provider_invocation",
			"topic_id":          attempt,
			"policy_id":         "topic-starter-provider-invocation-v1",
			"selection_mode":    "usage_retention",
			"propensity_reason": "not_applicable",
			"payload":           item.payload,
			"created_at":        item.createdAt,
		}); err != nil {
			return err
		}
	}

	if _, err := s.RecordEvent(map[string]any{
		"session_id":        "deletion",
		"slate_id":          "deletion",
		"topic_id":          "deletion",
		"event_type":        "recommendation_data_deleted",
		"policy_id":         "recommendation-consent-v1",
		"selection_mode":    "exclusion",
		"propensity_reason": "user_deletion",
		"created_at":        nowSeconds(),
		"payload": map[string]any{
			"exposures_removed": exposuresRemoved,
			"cards_removed":     cardsRemoved,
		},
	}); err != nil {
		return err
	}

	// VACUUM 不能在事务内执行，这里连接已全部关闭
	vacuumDB, err := s.connect()
	if err != nil {
		return err
	}
	defer vacuumDB.Close()
	_, err = vacuumDB.Exec("VACUUM")
	return err
}

// IntervenedPrompts 返回"已绑定会话 + 有点击提示词"的 (session_id, prompt) 集合。
// Python: topic_starter.py:673 intervened_prompts
//
// 用于后续历史扫描时区分"入库卡片注入的提示词"与"用户自己说的话"。
func (s *TopicStarterStore) IntervenedPrompts() (map[[2]string]struct{}, error) {
	events, err := s.ListEvents()
	if err != nil {
		return nil, err
	}

	boundSessions := map[string]string{}
	for _, event := range events {
		if pyStr(event["event_type"]) == "session_bound" {
			boundSessions[pyStr(event["slate_id"])] = pyStr(event["session_id"])
		}
	}

	result := make(map[[2]string]struct{})
	for _, event := range events {
		if pyStr(event["event_type"]) != "topic_click" {
			continue
		}
		slateID := pyStr(event["slate_id"])
		payload, _ := event["payload"].(map[string]any)
		prompt := ""
		if payload != nil {
			prompt = strings.TrimSpace(pyStr(payload["prompt"]))
		}
		sessionID, bound := boundSessions[slateID]
		if bound && prompt != "" {
			result[[2]string{sessionID, prompt}] = struct{}{}
		}
	}
	return result, nil
}

// ListEvents 按写入顺序返回全部事件。
// Python: topic_starter.py:566 list_events
func (s *TopicStarterStore) ListEvents() ([]map[string]any, error) {
	db, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT * FROM recommendation_event ORDER BY created_at, event_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// ComputeInterestDrift 计算前后两批兴趣标签的漂移度。
// Python: topic_starter.py:142 compute_interest_drift
//
// 注意 ShouldUpdate 比较的是 round 之前的原始 score（:173 读局部变量），
// 而返回的 Score 字段是 round 到 12 位的值；两者不可混用。
func ComputeInterestDrift(previousLabels, currentLabels []string) InterestDrift {
	previous := interestDistribution(previousLabels)
	current := interestDistribution(currentLabels)

	entropyDelta := math.Abs(interestEntropy(current) - interestEntropy(previous))

	previousSet := make(map[string]struct{}, len(previous))
	for label := range previous {
		previousSet[label] = struct{}{}
	}
	currentSet := make(map[string]struct{}, len(current))
	for label := range current {
		currentSet[label] = struct{}{}
	}
	union := make(map[string]struct{}, len(previousSet)+len(currentSet))
	for label := range previousSet {
		union[label] = struct{}{}
	}
	for label := range currentSet {
		union[label] = struct{}{}
	}
	jaccardDistance := 0.0
	if len(union) > 0 {
		intersection := 0
		for label := range previousSet {
			if _, ok := currentSet[label]; ok {
				intersection++
			}
		}
		jaccardDistance = 1 - float64(intersection)/float64(len(union))
	}

	divergence := jsDivergence(previous, current)

	score := driftWeights[0]*entropyDelta +
		driftWeights[1]*jaccardDistance +
		driftWeights[2]*divergence

	return InterestDrift{
		EntropyDelta:    round12(entropyDelta),
		JaccardDistance: round12(jaccardDistance),
		JSDivergence:    round12(divergence),
		Score:           round12(score),
		ShouldUpdate:    score > driftThreshold,
	}
}

// connect 打开一条数据库连接。
// Python: topic_starter.py:355 _connect —— 只设 busy_timeout，不设 WAL，不加 MaxOpenConns 限制。
func (s *TopicStarterStore) connect() (*sql.DB, error) {
	dsn := s.path + "?_busy_timeout=" + strconv.Itoa(int(sqliteBusyTimeoutSeconds*1000))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	// Python 每次 with self._connect() 新开连接、用后即关；这里不限制池容量（spec D-2）
	db.SetMaxOpenConns(0)
	return db, nil
}

// createTables 建两表四索引（幂等）。
// Python: topic_starter.py:361 _create_tables
func (s *TopicStarterStore) createTables() error {
	db, err := s.connect()
	if err != nil {
		return err
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS profile_snapshot (
			version INTEGER PRIMARY KEY,
			created_at REAL NOT NULL,
			evidence_until REAL NOT NULL,
			snapshot_json TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS recommendation_event (
			event_key TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			slate_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			topic_id TEXT,
			position INTEGER,
			profile_version INTEGER,
			policy_id TEXT NOT NULL,
			selection_mode TEXT NOT NULL,
			propensity REAL,
			propensity_reason TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at REAL NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recommendation_event_lookup
			ON recommendation_event(event_type, slate_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_recommendation_event_topic_lookup
			ON recommendation_event(event_type, topic_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_recommendation_event_manifest_compat
			ON recommendation_event(
				event_type,
				policy_id,
				json_extract(payload_json, '$.serving_compatibility_key'),
				json_extract(payload_json, '$.context_scope'),
				json_extract(payload_json, '$.project_id'),
				CAST(json_extract(payload_json, '$.negative_preference_version') AS INTEGER) DESC,
				created_at DESC,
				event_key DESC
			)`,
		`CREATE INDEX IF NOT EXISTS idx_recommendation_event_manifest_trigger
			ON recommendation_event(
				event_type,
				policy_id,
				json_extract(payload_json, '$.generation_trigger_key'),
				created_at DESC,
				event_key DESC
			)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("执行 DDL 失败: %w", err)
		}
	}
	return nil
}

// insertEventKeyColumns recommendation_event 的写入列顺序。
const insertEventKeyColumns = `event_key, session_id, slate_id, event_type, topic_id,
	position, profile_version, policy_id, selection_mode,
	propensity, propensity_reason, payload_json, created_at`

// eventArgs 把事件 map 展开为与 insertEventKeyColumns 对应的 13 个参数。
// Python: topic_starter.py:501-512 的默认值兜底链。
func eventArgs(eventKey string, event map[string]any) ([]any, error) {
	payloadMap, ok := event["payload"].(map[string]any)
	if !ok || payloadMap == nil {
		payloadMap = map[string]any{}
	}
	payloadJSON, err := marshalNoEscape(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("序列化 payload 失败: %w", err)
	}

	policyID := pyStr(event["policy_id"])
	if policyID == "" {
		policyID = "topic-starter-mvp-v1"
	}
	selectionMode := pyStr(event["selection_mode"])
	if selectionMode == "" {
		selectionMode = "unknown"
	}
	propensityReason := pyStr(event["propensity_reason"])
	if propensityReason == "" {
		propensityReason = "not_estimated"
	}
	createdAt := 0.0
	switch raw := event["created_at"].(type) {
	case float64:
		createdAt = raw
	case int64:
		createdAt = float64(raw)
	case int:
		createdAt = float64(raw)
	}
	if !pythonTruthy(createdAt) {
		createdAt = nowSeconds()
	}

	return []any{
		eventKey,
		pyStr(event["session_id"]),
		pyStr(event["slate_id"]),
		pyStr(event["event_type"]),
		nullableString(event["topic_id"]),
		nullableInt(event["position"]),
		nullableInt(event["profile_version"]),
		policyID,
		selectionMode,
		nullableFloat(event["propensity"]),
		propensityReason,
		string(payloadJSON),
		createdAt,
	}, nil
}

// nullableString nil 转 SQL NULL，其余转字符串。
func nullableString(value any) any {
	if value == nil {
		return nil
	}
	return pyStr(value)
}

// nullableInt nil 转 SQL NULL，其余转 int64。
func nullableInt(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return nil
	}
}

// nullableFloat nil 转 SQL NULL，其余转 float64。
func nullableFloat(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return nil
	}
}

// insertEvent 单条写入。ignoreConflict 为 true 时用 INSERT OR IGNORE（record_event），
// 为 false 时用普通 INSERT（record_events_atomic 需要唯一约束错误传播）。
func (s *TopicStarterStore) insertEvent(eventKey string, event map[string]any, ignoreConflict bool) (bool, error) {
	args, err := eventArgs(eventKey, event)
	if err != nil {
		return false, err
	}
	db, err := s.connect()
	if err != nil {
		return false, err
	}
	defer db.Close()

	verb := "INSERT"
	if ignoreConflict {
		verb = "INSERT OR IGNORE"
	}
	result, err := db.Exec(verb+" INTO recommendation_event("+insertEventKeyColumns+") VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// scanEventRows 把结果集转为 []map[string]any，payload_json 解回 payload。
// Python: topic_starter.py:562-565 的 `item["payload"] = json.loads(item.pop("payload_json"))`
func scanEventRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(columns))
		for i, column := range columns {
			item[column] = values[i]
		}
		item["payload"] = map[string]any{}
		if raw, ok := item["payload_json"]; ok {
			text := ""
			switch value := raw.(type) {
			case []byte:
				text = string(value)
			case string:
				text = value
			}
			if text != "" {
				decoded := map[string]any{}
				if err := json.Unmarshal([]byte(text), &decoded); err != nil {
					return nil, fmt.Errorf("解析 payload_json 失败: %w", err)
				}
				item["payload"] = decoded
			}
		}
		delete(item, "payload_json")
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// pruneSupersededSummaries 每个会话只保留最新 supersededSummaryKeep 条会话摘要。
// Python: topic_starter.py:290 _prune_superseded_summaries
func pruneSupersededSummaries(tx *sql.Tx) (int64, error) {
	rows, err := tx.Query(
		`SELECT event_key, payload_json FROM recommendation_event
		 WHERE event_type = 'session_summary'
		 ORDER BY json_extract(payload_json, '$.session_id'), created_at DESC, event_key DESC`,
	)
	if err != nil {
		return 0, err
	}
	type doomedRow struct {
		sessionID string
		eventKey  string
	}
	ordered := make([]doomedRow, 0)
	for rows.Next() {
		var eventKey string
		var payloadJSON []byte
		if err := rows.Scan(&eventKey, &payloadJSON); err != nil {
			rows.Close()
			return 0, err
		}
		sessionID := ""
		decoded := map[string]any{}
		if len(payloadJSON) > 0 && json.Unmarshal(payloadJSON, &decoded) == nil {
			sessionID = pyStr(decoded["session_id"])
		}
		ordered = append(ordered, doomedRow{sessionID: sessionID, eventKey: eventKey})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	seen := map[string]int{}
	var removed int64
	for _, row := range ordered {
		if seen[row.sessionID] >= supersededSummaryKeep {
			if _, err := tx.Exec("DELETE FROM recommendation_event WHERE event_key = ?", row.eventKey); err != nil {
				return 0, err
			}
			removed++
		}
		seen[row.sessionID]++
	}
	return removed, nil
}

// pruneProfileSnapshots 删除除最新两版外、超过留存期的画像快照。
// Python: topic_starter.py:316 _prune_profile_snapshots
func pruneProfileSnapshots(tx *sql.Tx, now float64) (int64, error) {
	cutoff := now - float64(profileSnapshotRetentionDays)*daySeconds
	rows, err := tx.Query("SELECT version, created_at FROM profile_snapshot ORDER BY version DESC")
	if err != nil {
		return 0, err
	}
	type snapshotRow struct {
		version   int64
		createdAt float64
	}
	ordered := make([]snapshotRow, 0)
	for rows.Next() {
		var row snapshotRow
		if err := rows.Scan(&row.version, &row.createdAt); err != nil {
			rows.Close()
			return 0, err
		}
		ordered = append(ordered, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var removed int64
	for index, row := range ordered {
		// 最新两版无条件保留：读取总是从最新版本开始
		if index < 2 {
			continue
		}
		if row.createdAt >= cutoff {
			continue
		}
		if _, err := tx.Exec("DELETE FROM profile_snapshot WHERE version = ?", row.version); err != nil {
			return 0, err
		}
		removed++
	}
	return removed, nil
}

// pruneShortIntent 清理过期的短期意图状态；表不存在时返回 0。
// Python: topic_starter.py:339 _prune_short_intent
func pruneShortIntent(tx *sql.Tx, now float64) (int64, error) {
	var probe int
	err := tx.QueryRow(
		"SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'short_intent_state'",
	).Scan(&probe)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	result, err := tx.Exec(
		"DELETE FROM short_intent_state WHERE evaluated_at < ?",
		now-float64(shortIntentRetentionDays)*daySeconds,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
