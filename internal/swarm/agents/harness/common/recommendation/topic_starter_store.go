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
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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
		if v == math.Trunc(v) && !math.IsInf(v, 0) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'g', -1, 64)
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
