package recommendation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPythonTruthy_Python真值语义
// Python: topic_starter.py:486 event_key_of 的 `if event.get("feedback_record_id"):`
func TestPythonTruthy_Python真值语义(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  bool
	}{
		{"nil", nil, false},
		{"空串", "", false},
		{"非空串", "x", true},
		{"零整数", 0, false},
		{"非零整数", 1, true},
		{"零浮点", 0.0, false},
		{"非零浮点", 0.5, true},
		{"false", false, false},
		{"true", true, true},
		{"空切片", []any{}, false},
		{"非空切片", []any{1}, true},
		{"空 map", map[string]any{}, false},
		{"非空 map", map[string]any{"a": 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, pythonTruthy(c.value))
		})
	}
}

// TestPyStr_Python的str语义
// Python: topic_starter.py:483 `str(event.get("session_id") or "")`
func TestPyStr_Python的str语义(t *testing.T) {
	assert.Equal(t, "", pyStr(nil))
	assert.Equal(t, "", pyStr(""))
	assert.Equal(t, "abc", pyStr("abc"))
	assert.Equal(t, "1", pyStr(1))
	assert.Equal(t, "1", pyStr(int64(1)))
	assert.Equal(t, "1", pyStr(float64(1)))
	assert.Equal(t, "1.5", pyStr(1.5))
	assert.Equal(t, "True", pyStr(true))
	assert.Equal(t, "False", pyStr(false))
}

// TestMarshalNoEscape_不转义HTML且无尾换行
// Python: json.dumps(value, ensure_ascii=False, separators=(",", ":"))
func TestMarshalNoEscape_不转义HTML且无尾换行(t *testing.T) {
	out, err := marshalNoEscape(map[string]any{"k": "<中文&>"})
	assert.NoError(t, err)
	assert.Equal(t, `{"k":"<中文&>"}`, string(out))
	assert.NotContains(t, string(out), "\n")
}

// TestRetentionDays_16项与上游一致
// Python: topic_starter.py:80-95 _RETENTION_DAYS
func TestRetentionDays_16项与上游一致(t *testing.T) {
	assert.Len(t, retentionDays, 16)
	assert.Equal(t, 30, retentionDays["dynamic_exposure"])
	assert.Equal(t, 45, retentionDays["starter_slate_manifest"])
	assert.Equal(t, 45, retentionDays["starter_card"])
	assert.Equal(t, 180, retentionDays["provider_invocation"])
	assert.Equal(t, 90, retentionDays["session_summary_coverage"])
	assert.Equal(t, 30, retentionDays["store_pruned"])
	// session_summary 不在表内，永不按龄删除
	_, has := retentionDays["session_summary"]
	assert.False(t, has)
}

// TestEventPayloadQueryFields_13项与上游一致
// Python: topic_starter.py:57-71 _EVENT_PAYLOAD_QUERY_FIELDS
func TestEventPayloadQueryFields_13项与上游一致(t *testing.T) {
	assert.Len(t, eventPayloadQueryFields, 13)
	for _, field := range []string{
		"card_id", "context_scope", "generation_trigger_key", "negative_preference_version",
		"project_id", "serving_compatibility_key", "short_intent_version", "source_starter_id",
		"model", "provider_response_id", "status",
		"generation_trigger_key.context_scope", "generation_trigger_key.model",
	} {
		_, ok := eventPayloadQueryFields[field]
		assert.True(t, ok, "缺少字段 %s", field)
	}
}

// TestComputeInterestDrift_空输入全零
// Python: topic_starter.py:142 compute_interest_drift
func TestComputeInterestDrift_空输入全零(t *testing.T) {
	drift := ComputeInterestDrift(nil, nil)
	assert.Equal(t, 0.0, drift.EntropyDelta)
	assert.Equal(t, 0.0, drift.JaccardDistance)
	assert.Equal(t, 0.0, drift.JSDivergence)
	assert.Equal(t, 0.0, drift.Score)
	assert.False(t, drift.ShouldUpdate)
}

// TestComputeInterestDrift_完全一致全零
func TestComputeInterestDrift_完全一致全零(t *testing.T) {
	labels := []string{"考研", "数学", "英语"}
	drift := ComputeInterestDrift(labels, labels)
	assert.Equal(t, 0.0, drift.EntropyDelta)
	assert.Equal(t, 0.0, drift.JaccardDistance)
	assert.Equal(t, 0.0, drift.JSDivergence)
	assert.Equal(t, 0.0, drift.Score)
	assert.False(t, drift.ShouldUpdate)
}

// TestComputeInterestDrift_完全不重叠Jaccard为一
func TestComputeInterestDrift_完全不重叠Jaccard为一(t *testing.T) {
	drift := ComputeInterestDrift([]string{"a", "b"}, []string{"c", "d"})
	assert.Equal(t, 1.0, drift.JaccardDistance)
	// 0.4*熵差 + 0.3*1 + 0.3*JS；两分布都是均匀二点，熵相等 → 熵差 0
	assert.Equal(t, 0.0, drift.EntropyDelta)
	assert.InDelta(t, 0.6, drift.Score, 1e-9)
	assert.False(t, drift.ShouldUpdate, "0.6 未超过阈值 0.8")
}

// TestComputeInterestDrift_标签先strip再判空
// Python: topic_starter.py:113 `str(label).strip() for label in labels if str(label).strip()`
func TestComputeInterestDrift_标签先strip再判空(t *testing.T) {
	withSpace := ComputeInterestDrift([]string{" a ", "b"}, []string{"a", "b"})
	assert.Equal(t, 0.0, withSpace.Score, "空格应先 strip 后视为同一标签")

	withBlank := ComputeInterestDrift([]string{"a", "", "  ", "b"}, []string{"a", "b"})
	assert.Equal(t, 0.0, withBlank.Score, "空与纯空白标签应被过滤")
}

// TestComputeInterestDrift_漂移触发更新
func TestComputeInterestDrift_漂移触发更新(t *testing.T) {
	// 单标签 → 多标签：熵显著上升，集合完全不重叠
	drift := ComputeInterestDrift([]string{"a"}, []string{"b", "c", "d", "e"})
	assert.True(t, drift.ShouldUpdate, "score=%v 应超过 0.8", drift.Score)
	assert.Greater(t, drift.Score, 0.8)
}

// TestComputeInterestDrift_阈值用round前的score
// Python: topic_starter.py:172-173 —— should_update 读局部变量 score（未 round），
// 返回的 score 字段是 round(score, 12)。
func TestComputeInterestDrift_阈值用round前的score(t *testing.T) {
	drift := ComputeInterestDrift([]string{"a", "b"}, []string{"c", "d"})
	assert.InDelta(t, 0.6, drift.Score, 1e-9)
	assert.False(t, drift.ShouldUpdate)

	// round12 不改变已 round 值的稳定性
	assert.Equal(t, round12(drift.Score), drift.Score)
}

// TestInterestDistribution_归一化
func TestInterestDistribution_归一化(t *testing.T) {
	dist := interestDistribution([]string{"a", "a", "b"})
	assert.InDelta(t, 2.0/3.0, dist["a"], 1e-12)
	assert.InDelta(t, 1.0/3.0, dist["b"], 1e-12)
	assert.Empty(t, interestDistribution(nil))
	assert.Empty(t, interestDistribution([]string{"", "  "}))
}

// TestInterestEntropy_均匀分布最大
func TestInterestEntropy_均匀分布最大(t *testing.T) {
	assert.InDelta(t, 1.0, interestEntropy(map[string]float64{"a": 0.5, "b": 0.5}), 1e-12)
	assert.InDelta(t, 2.0, interestEntropy(map[string]float64{"a": .25, "b": .25, "c": .25, "d": .25}), 1e-12)
	assert.Equal(t, 0.0, interestEntropy(map[string]float64{"a": 1.0}))
	assert.Equal(t, 0.0, interestEntropy(nil))
}

// newTestStore 建一个落在临时目录的 store。
func newTestStore(t *testing.T) *TopicStarterStore {
	t.Helper()
	store, err := NewTopicStarterStore(filepath.Join(t.TempDir(), "recommendation.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

// TestNewTopicStarterStore_父目录自动创建
// Python: topic_starter.py:183 mkdir(parents=True, exist_ok=True)
func TestNewTopicStarterStore_父目录自动创建(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "a", "b", "c", "recommendation.db")
	store, err := NewTopicStarterStore(nested)
	require.NoError(t, err)
	defer store.Close()

	_, statErr := os.Stat(nested)
	assert.NoError(t, statErr, "数据库文件应已创建")
}

// TestNewTopicStarterStore_建表幂等
func TestNewTopicStarterStore_建表幂等(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recommendation.db")

	first, err := NewTopicStarterStore(path)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := NewTopicStarterStore(path)
	require.NoError(t, err)
	defer second.Close()

	// 表结构仍可读写即证明未被损坏（此处直接用底层连接，不依赖 Task 4 的 RecordEvent）
	db, err := second.connect()
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(
		"INSERT INTO recommendation_event(event_key, session_id, slate_id, event_type, policy_id, selection_mode, propensity_reason, payload_json, created_at) VALUES(?,?,?,?,?,?,?,?,?)",
		"s1|sl1|e1|", "s1", "sl1", "e1", "p", "unknown", "not_estimated", "{}", 1.0,
	)
	assert.NoError(t, err)
}

// TestCreateTables_两表四索引齐备
// Python: topic_starter.py:361 _create_tables
func TestCreateTables_两表四索引齐备(t *testing.T) {
	store := newTestStore(t)

	db, err := store.connect()
	require.NoError(t, err)
	defer db.Close()

	rows, err := db.Query("SELECT name, type FROM sqlite_master WHERE type IN ('table','index') AND name NOT LIKE 'sqlite_%' ORDER BY name")
	require.NoError(t, err)
	defer rows.Close()

	found := map[string]string{}
	for rows.Next() {
		var name, kind string
		require.NoError(t, rows.Scan(&name, &kind))
		found[name] = kind
	}
	require.NoError(t, rows.Err())

	assert.Equal(t, "table", found["profile_snapshot"])
	assert.Equal(t, "table", found["recommendation_event"])
	assert.Equal(t, "index", found["idx_recommendation_event_lookup"])
	assert.Equal(t, "index", found["idx_recommendation_event_topic_lookup"])
	assert.Equal(t, "index", found["idx_recommendation_event_manifest_compat"])
	assert.Equal(t, "index", found["idx_recommendation_event_manifest_trigger"])
}

// TestConnect_busyTimeout生效
// Python: topic_starter.py:355 _connect（PRAGMA busy_timeout = 5000）
func TestConnect_busyTimeout生效(t *testing.T) {
	store := newTestStore(t)

	db, err := store.connect()
	require.NoError(t, err)
	defer db.Close()

	var timeout int
	require.NoError(t, db.QueryRow("PRAGMA busy_timeout").Scan(&timeout))
	assert.Equal(t, 5000, timeout)
}
