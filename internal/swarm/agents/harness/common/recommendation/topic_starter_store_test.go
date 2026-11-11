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

// TestEventKeyOf_五段拼接
// Python: topic_starter.py:486 event_key_of
func TestEventKeyOf_五段拼接(t *testing.T) {
	store := newTestStore(t)

	assert.Equal(t, "s|sl|e|", store.EventKeyOf(map[string]any{
		"session_id": "s", "slate_id": "sl", "event_type": "e",
	}))
	assert.Equal(t, "s|sl|e|t", store.EventKeyOf(map[string]any{
		"session_id": "s", "slate_id": "sl", "event_type": "e", "topic_id": "t",
	}))
}

// TestEventKeyOf_feedbackRecordId为truthy才追加
func TestEventKeyOf_feedbackRecordId为truthy才追加(t *testing.T) {
	store := newTestStore(t)
	base := map[string]any{"session_id": "s", "slate_id": "sl", "event_type": "e", "topic_id": "t"}
	want := "s|sl|e|t"

	for _, falsy := range []any{nil, "", 0, 0.0, false} {
		event := map[string]any{}
		for k, v := range base {
			event[k] = v
		}
		event["feedback_record_id"] = falsy
		assert.Equal(t, want, store.EventKeyOf(event), "falsy 值 %#v 不应追加", falsy)
	}

	event := map[string]any{}
	for k, v := range base {
		event[k] = v
	}
	event["feedback_record_id"] = "fb-1"
	assert.Equal(t, "s|sl|e|t|fb-1", store.EventKeyOf(event))
}

// TestRecordEvent_首次成功二次返回假
// Python: topic_starter.py:498 record_event（INSERT OR IGNORE + rowcount == 1）
func TestRecordEvent_首次成功二次返回假(t *testing.T) {
	store := newTestStore(t)
	event := map[string]any{"session_id": "s", "slate_id": "sl", "event_type": "e", "topic_id": "t"}

	first, err := store.RecordEvent(event)
	require.NoError(t, err)
	assert.True(t, first)

	second, err := store.RecordEvent(event)
	require.NoError(t, err)
	assert.False(t, second, "同 key 重复写入应返回 false")
}

// TestRecordEvent_五层默认值兜底
// Python: topic_starter.py:507-512 —— policy_id/selection_mode/propensity_reason/payload/created_at
func TestRecordEvent_五层默认值兜底(t *testing.T) {
	store := newTestStore(t)
	ok, err := store.RecordEvent(map[string]any{
		"session_id": "s", "slate_id": "sl", "event_type": "e",
	})
	require.NoError(t, err)
	require.True(t, ok)

	events, err := store.ListEvents()
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, "topic-starter-mvp-v1", events[0]["policy_id"])
	assert.Equal(t, "unknown", events[0]["selection_mode"])
	assert.Equal(t, "not_estimated", events[0]["propensity_reason"])
	assert.Equal(t, map[string]any{}, events[0]["payload"])
	assert.Greater(t, events[0]["created_at"].(float64), 0.0)
	assert.Nil(t, events[0]["topic_id"])
	assert.Nil(t, events[0]["position"])
	assert.Nil(t, events[0]["profile_version"])
	assert.Nil(t, events[0]["propensity"])
}

// TestRecordEvent_非ASCII原样存储
func TestRecordEvent_非ASCII原样存储(t *testing.T) {
	store := newTestStore(t)
	_, err := store.RecordEvent(map[string]any{
		"session_id": "s", "slate_id": "sl", "event_type": "e",
		"payload": map[string]any{"title": "中文标题 & <tag>"},
	})
	require.NoError(t, err)

	events, err := store.ListEvents()
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "中文标题 & <tag>", events[0]["payload"].(map[string]any)["title"])
}

// TestRecordEventsAtomic_正常写入整组
// Python: topic_starter.py:527 record_events_atomic
func TestRecordEventsAtomic_正常写入整组(t *testing.T) {
	store := newTestStore(t)
	events := []map[string]any{
		{"session_id": "s", "slate_id": "sl", "event_type": "e1", "topic_id": "t1"},
		{"session_id": "s", "slate_id": "sl", "event_type": "e2", "topic_id": "t2"},
		{"session_id": "s", "slate_id": "sl", "event_type": "e3", "topic_id": "t3"},
	}
	ok, err := store.RecordEventsAtomic(events)
	require.NoError(t, err)
	assert.True(t, ok)

	stored, err := store.ListEvents()
	require.NoError(t, err)
	assert.Len(t, stored, 3)
}

// TestRecordEventsAtomic_空列表返回假
func TestRecordEventsAtomic_空列表返回假(t *testing.T) {
	store := newTestStore(t)
	ok, err := store.RecordEventsAtomic(nil)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = store.RecordEventsAtomic([]map[string]any{})
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestRecordEventsAtomic_组内重复返回假且不写库
func TestRecordEventsAtomic_组内重复返回假且不写库(t *testing.T) {
	store := newTestStore(t)
	same := map[string]any{"session_id": "s", "slate_id": "sl", "event_type": "e", "topic_id": "t"}
	ok, err := store.RecordEventsAtomic([]map[string]any{same, same})
	require.NoError(t, err)
	assert.False(t, ok)

	stored, err := store.ListEvents()
	require.NoError(t, err)
	assert.Empty(t, stored, "组内重复应整组拒绝，不写任何行")
}

// TestRecordEventsAtomic_跨组重复整组回滚
func TestRecordEventsAtomic_跨组重复整组回滚(t *testing.T) {
	store := newTestStore(t)
	existing := map[string]any{"session_id": "s", "slate_id": "sl", "event_type": "e", "topic_id": "t0"}
	ok, err := store.RecordEvent(existing)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.RecordEventsAtomic([]map[string]any{
		{"session_id": "s", "slate_id": "sl", "event_type": "e2", "topic_id": "t2"},
		existing,
	})
	assert.False(t, ok, "跨组重复应返回 false")
	assert.NoError(t, err, "唯一约束错误应转为 false 而非 error")

	stored, err := store.ListEvents()
	require.NoError(t, err)
	assert.Len(t, stored, 1, "整组回滚，只保留原有 1 条")
}

// seedQueryEvents 造一批带 payload 的事件。
func seedQueryEvents(t *testing.T, store *TopicStarterStore) {
	t.Helper()
	events := []map[string]any{
		{"session_id": "s1", "slate_id": "sl1", "event_type": "topic_click", "topic_id": "t1",
			"payload":    map[string]any{"context_scope": "global_new_chat", "negative_preference_version": 3},
			"created_at": 100.0},
		{"session_id": "s2", "slate_id": "sl2", "event_type": "topic_click", "topic_id": "t2",
			"payload":    map[string]any{"context_scope": "project", "negative_preference_version": 1},
			"created_at": 200.0},
		{"session_id": "s3", "slate_id": "sl3", "event_type": "session_bound", "topic_id": "t3",
			"payload":    map[string]any{"context_scope": "global_new_chat"},
			"created_at": 300.0},
		{"session_id": "s4", "slate_id": "sl4", "event_type": "topic_click", "topic_id": "t4",
			"payload":    map[string]any{"context_scope": nil, "status": "ok"},
			"created_at": 400.0},
	}
	ok, err := store.RecordEventsAtomic(events)
	require.NoError(t, err)
	require.True(t, ok)
}

// TestQueryEvents_四条守卫
// Python: topic_starter.py:590-601
func TestQueryEvents_四条守卫(t *testing.T) {
	store := newTestStore(t)
	seedQueryEvents(t, store)

	got, err := store.QueryEvents(QueryEventsOptions{EventType: "", Limit: 10})
	require.NoError(t, err)
	assert.Nil(t, got, "event_type 为空返回 nil")

	got, err = store.QueryEvents(QueryEventsOptions{EventType: "topic_click", Limit: 0})
	require.NoError(t, err)
	assert.Nil(t, got, "limit < 1 返回 nil")

	got, err = store.QueryEvents(QueryEventsOptions{EventType: "topic_click", Limit: -1})
	require.NoError(t, err)
	assert.Nil(t, got, "limit 负数返回 nil")

	got, err = store.QueryEvents(QueryEventsOptions{EventType: "topic_click", Limit: 10, Offset: -1})
	require.NoError(t, err)
	assert.Nil(t, got, "offset 负数返回 nil")

	_, err = store.QueryEvents(QueryEventsOptions{
		EventType:     "topic_click",
		Limit:         10,
		PayloadEquals: map[string]any{"not_a_whitelisted_field": 1},
	})
	require.Error(t, err, "白名单外字段应报错")
	assert.Contains(t, err.Error(), "not_a_whitelisted_field")
}

// TestQueryEvents_按类型与slate过滤
func TestQueryEvents_按类型与slate过滤(t *testing.T) {
	store := newTestStore(t)
	seedQueryEvents(t, store)

	got, err := store.QueryEvents(QueryEventsOptions{EventType: "topic_click", Limit: 100})
	require.NoError(t, err)
	assert.Len(t, got, 3)

	slate := "sl2"
	got, err = store.QueryEvents(QueryEventsOptions{EventType: "topic_click", SlateID: &slate, Limit: 100})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "t2", got[0]["topic_id"])
}

// TestQueryEvents_payloadEquals匹配JSONnull
// Python: topic_starter.py:626 `json_extract(...) IS json_extract(?, '$')`
func TestQueryEvents_payloadEquals匹配JSONnull(t *testing.T) {
	store := newTestStore(t)
	seedQueryEvents(t, store)

	got, err := store.QueryEvents(QueryEventsOptions{
		EventType:     "topic_click",
		PayloadEquals: map[string]any{"context_scope": nil},
		Limit:         100,
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "IS 语义应能匹配 JSON null")
	assert.Equal(t, "t4", got[0]["topic_id"])
}

// TestQueryEvents_payload整数上界与存在性
func TestQueryEvents_payload整数上界与存在性(t *testing.T) {
	store := newTestStore(t)
	seedQueryEvents(t, store)

	got, err := store.QueryEvents(QueryEventsOptions{
		EventType:         "topic_click",
		PayloadIntegerLTE: &PayloadIntegerLTE{Field: "negative_preference_version", Value: 1},
		Limit:             100,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "t2", got[0]["topic_id"])

	present := "status"
	got, err = store.QueryEvents(QueryEventsOptions{
		EventType:      "topic_click",
		PayloadPresent: &present,
		Limit:          100,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "t4", got[0]["topic_id"])
}

// TestQueryEvents_排序与分页
func TestQueryEvents_排序与分页(t *testing.T) {
	store := newTestStore(t)
	seedQueryEvents(t, store)

	// 默认升序
	got, err := store.QueryEvents(QueryEventsOptions{EventType: "topic_click", Limit: 100})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "t1", got[0]["topic_id"])

	// 最新优先
	got, err = store.QueryEvents(QueryEventsOptions{EventType: "topic_click", Limit: 100, NewestFirst: true})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "t4", got[0]["topic_id"])

	// 分页
	got, err = store.QueryEvents(QueryEventsOptions{EventType: "topic_click", Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "t2", got[0]["topic_id"])
}

// TestQueryEvents_按payload整数降序排序
// Python: topic_starter.py:645-652 order_by_payload_integer_desc 前缀
func TestQueryEvents_按payload整数降序排序(t *testing.T) {
	store := newTestStore(t)
	seedQueryEvents(t, store)

	field := "negative_preference_version"
	got, err := store.QueryEvents(QueryEventsOptions{
		EventType:                 "topic_click",
		OrderByPayloadIntegerDesc: &field,
		Limit:                     100,
	})
	require.NoError(t, err)
	require.Len(t, got, 3)
	// version 3 > 1 > NULL
	assert.Equal(t, "t1", got[0]["topic_id"], "version=3 应排最前")
	assert.Equal(t, "t2", got[1]["topic_id"], "version=1 次之")
}

// TestSaveSnapshot_版本自增并回写字段
// Python: topic_starter.py:437 save_snapshot
func TestSaveSnapshot_版本自增并回写字段(t *testing.T) {
	store := newTestStore(t)

	first, err := store.SaveSnapshot(map[string]any{"interests": []any{"a"}}, 1000.0)
	require.NoError(t, err)
	assert.Equal(t, 1, first["version"])
	assert.Equal(t, 1000.0, first["evidence_until"])
	assert.Greater(t, first["updated_at"].(float64), 0.0)
	assert.Equal(t, []any{"a"}, first["interests"])

	second, err := store.SaveSnapshot(map[string]any{"interests": []any{"b"}}, 2000.0)
	require.NoError(t, err)
	assert.Equal(t, 2, second["version"])

	third, err := store.SaveSnapshot(map[string]any{}, 3000.0)
	require.NoError(t, err)
	assert.Equal(t, 3, third["version"])
}

// TestLoadLatestSnapshot_取最新
func TestLoadLatestSnapshot_取最新(t *testing.T) {
	store := newTestStore(t)

	got, err := store.LoadLatestSnapshot()
	require.NoError(t, err)
	assert.Nil(t, got, "空表返回 nil")

	_, err = store.SaveSnapshot(map[string]any{"mark": "v1"}, 1.0)
	require.NoError(t, err)
	_, err = store.SaveSnapshot(map[string]any{"mark": "v2"}, 2.0)
	require.NoError(t, err)

	got, err = store.LoadLatestSnapshot()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "v2", got["mark"])
	// 从库里读回来的是 JSON 数字，encoding/json 解成 float64（不是 SaveSnapshot 返回的 int）
	assert.Equal(t, float64(2), got["version"])
}

// TestLoadLatestSnapshot_跳过v4行
// Python: topic_starter.py:469-484 —— schema_version == "topic-starter-v4" 的跳过
func TestLoadLatestSnapshot_跳过v4行(t *testing.T) {
	store := newTestStore(t)

	_, err := store.SaveSnapshot(map[string]any{"mark": "v3-readable"}, 1.0)
	require.NoError(t, err)

	// 直接插一条 v4 快照（模拟另一套格式）
	db, err := store.connect()
	require.NoError(t, err)
	_, err = db.Exec(
		"INSERT INTO profile_snapshot(version, created_at, evidence_until, snapshot_json) VALUES(?, ?, ?, ?)",
		99, 1.0, 1.0, `{"schema_version":"topic-starter-v4","mark":"v4-only"}`,
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	got, err := store.LoadLatestSnapshot()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "v3-readable", got["mark"], "应跳过 v4 行")
}
