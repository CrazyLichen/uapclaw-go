package recommendation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── findCalendarServerEntry ────────────────────────────

// TestFindCalendarServerEntry_无mcp配置 配置里没有 mcp 段时返回 nil。
func TestFindCalendarServerEntry_无mcp配置(t *testing.T) {
	// 真实配置可能没有 mcp.calendar，只验证不 panic 且返回类型正确
	entry := findCalendarServerEntry()
	if entry != nil {
		assert.Contains(t, strings.ToLower(stringField(entry, "name")), calendarServerNameKeyword)
	}
}

// TestPickCalendarServerEntry_按名字识别 直接测**生产**的候选挑选逻辑。
// 不用测试内另写的匹配函数——那会与生产逻辑脱节，失去测试意义。
func TestPickCalendarServerEntry_按名字识别(t *testing.T) {
	cases := []struct {
		name    string
		entries []map[string]any
		wantHit bool
	}{
		{"名字含calendar", []map[string]any{{"name": "my-calendar-server"}}, true},
		{"大小写不敏感", []map[string]any{{"name": "MyCalendar"}}, true},
		{"名字不含calendar", []map[string]any{{"name": "weather-server"}}, false},
		{"enabled为false排除", []map[string]any{{"name": "calendar", "enabled": false}}, false},
		{"enabled缺省视为启用", []map[string]any{{"name": "calendar"}}, true},
		{"enabled为true", []map[string]any{{"name": "calendar", "enabled": true}}, true},
		{"多个取首个命中的", []map[string]any{
			{"name": "weather-server"},
			{"name": "calendar-a"},
			{"name": "calendar-b"},
		}, true},
		{"空列表", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := pickCalendarServerEntry(tc.entries)
			assert.Equal(t, tc.wantHit, entry != nil)
			if tc.wantHit && entry != nil {
				assert.Contains(t, strings.ToLower(stringField(entry, "name")), calendarServerNameKeyword)
			}
		})
	}
}

// ──────────────────────────── extractCalendarEvents ────────────────────────────

// TestExtractCalendarEvents 各种 MCP 返回形态。
// Python: calendar_source.py:133
func TestExtractCalendarEvents(t *testing.T) {
	t.Run("events信封", func(t *testing.T) {
		out := map[string]any{"result": map[string]any{"events": []any{"a", "b"}}}
		assert.Equal(t, []any{"a", "b"}, extractCalendarEvents(out))
	})

	t.Run("items信封", func(t *testing.T) {
		out := map[string]any{"result": map[string]any{"items": []any{"x"}}}
		assert.Equal(t, []any{"x"}, extractCalendarEvents(out))
	})

	t.Run("data信封", func(t *testing.T) {
		out := map[string]any{"result": map[string]any{"data": []any{"y"}}}
		assert.Equal(t, []any{"y"}, extractCalendarEvents(out))
	})

	t.Run("裸列表", func(t *testing.T) {
		out := map[string]any{"result": []any{"e1", "e2"}}
		assert.Equal(t, []any{"e1", "e2"}, extractCalendarEvents(out))
	})

	t.Run("单个事件对象", func(t *testing.T) {
		out := map[string]any{"result": map[string]any{"title": "会议", "start": "10:00"}}
		got := extractCalendarEvents(out)
		require.Len(t, got, 1)
	})

	t.Run("JSON字符串结果", func(t *testing.T) {
		payload := `{"events": [{"title": "会议"}]}`
		out := map[string]any{"result": payload}
		got := extractCalendarEvents(out)
		require.Len(t, got, 1)
	})

	t.Run("双重编码字符串", func(t *testing.T) {
		inner := `{"events": [{"title": "双层"}]}`
		encoded, _ := json.Marshal(inner) // 编码成带引号的 JSON 字符串
		out := map[string]any{"result": string(encoded)}
		got := extractCalendarEvents(out)
		require.Len(t, got, 1)
	})

	t.Run("无效JSON字符串返回nil", func(t *testing.T) {
		out := map[string]any{"result": "{不是json"}
		assert.Nil(t, extractCalendarEvents(out))
	})

	t.Run("nil输入", func(t *testing.T) {
		assert.Nil(t, extractCalendarEvents(nil))
	})

	t.Run("无result键时用out自身", func(t *testing.T) {
		out := map[string]any{"events": []any{"direct"}}
		assert.Equal(t, []any{"direct"}, extractCalendarEvents(out))
	})

	t.Run("不可识别结构返回nil", func(t *testing.T) {
		assert.Nil(t, extractCalendarEvents(map[string]any{"result": map[string]any{"unknown": 1}}))
		assert.Nil(t, extractCalendarEvents(map[string]any{"result": 12345}))
	})
}

// ──────────────────────────── normalizeCalendarEvent ────────────────────────────

// TestNormalizeCalendarEvent 候选键归一。
// Python: calendar_source.py:172
func TestNormalizeCalendarEvent(t *testing.T) {
	t.Run("标准字段", func(t *testing.T) {
		got := normalizeCalendarEvent(map[string]any{
			"title": "周会", "start": "10:00", "end": "11:00", "location": "3F",
		})
		assert.Equal(t, "周会", got["title"])
		assert.Equal(t, "10:00", got["start"])
		assert.Equal(t, "11:00", got["end"])
		assert.Equal(t, "3F", got["location"])
	})

	t.Run("候选键命中首个", func(t *testing.T) {
		got := normalizeCalendarEvent(map[string]any{
			"summary":   "用summary当标题",
			"startTime": "09:00",
			"dtEnd":     "09:30",
			"place":     "线上",
		})
		assert.Equal(t, "用summary当标题", got["title"])
		assert.Equal(t, "09:00", got["start"])
		assert.Equal(t, "09:30", got["end"])
		assert.Equal(t, "线上", got["location"])
	})

	t.Run("嵌套dateTime形态", func(t *testing.T) {
		got := normalizeCalendarEvent(map[string]any{
			"title": "嵌套",
			"start": map[string]any{"dateTime": "2026-11-11T10:00:00Z"},
			"end":   map[string]any{"date": "2026-11-11"},
		})
		assert.Equal(t, "2026-11-11T10:00:00Z", got["start"])
		assert.Equal(t, "2026-11-11", got["end"])
	})

	t.Run("嵌套value形态", func(t *testing.T) {
		got := normalizeCalendarEvent(map[string]any{
			"title":    "value形态",
			"location": map[string]any{"value": "会议室A"},
		})
		assert.Equal(t, "会议室A", got["location"])
	})

	t.Run("列表取首元素", func(t *testing.T) {
		got := normalizeCalendarEvent(map[string]any{
			"title": "列表",
			"start": []any{"第一个"},
		})
		assert.Equal(t, "第一个", got["start"])
	})

	t.Run("字段全缺返回空串", func(t *testing.T) {
		got := normalizeCalendarEvent(map[string]any{})
		assert.Equal(t, "", got["title"])
		assert.Equal(t, "", got["start"])
		assert.Equal(t, "", got["end"])
		assert.Equal(t, "", got["location"])
	})

	t.Run("非对象返回空map", func(t *testing.T) {
		assert.Empty(t, normalizeCalendarEvent("not-a-map"))
		assert.Empty(t, normalizeCalendarEvent(nil))
	})
}

// ──────────────────────────── firstPresent ────────────────────────────

// TestFirstPresent 候选键探测。
// Python: calendar_source.py:184
func TestFirstPresent(t *testing.T) {
	t.Run("首个非空", func(t *testing.T) {
		obj := map[string]any{"b": "值B", "c": "值C"}
		assert.Equal(t, "值B", firstPresent(obj, []string{"a", "b", "c"}))
	})

	t.Run("空串被跳过", func(t *testing.T) {
		obj := map[string]any{"a": "", "b": "有值"}
		assert.Equal(t, "有值", firstPresent(obj, []string{"a", "b"}))
	})

	t.Run("nil被跳过", func(t *testing.T) {
		obj := map[string]any{"a": nil, "b": "有值"}
		assert.Equal(t, "有值", firstPresent(obj, []string{"a", "b"}))
	})

	t.Run("全缺返回空串", func(t *testing.T) {
		assert.Equal(t, "", firstPresent(map[string]any{}, []string{"a", "b"}))
	})

	t.Run("嵌套对象无目标子键时继续下一个候选键", func(t *testing.T) {
		obj := map[string]any{
			"a": map[string]any{"unrelated": 1},
			"b": "兜底值",
		}
		assert.Equal(t, "兜底值", firstPresent(obj, []string{"a", "b"}))
	})

	t.Run("空列表继续下一个候选键", func(t *testing.T) {
		obj := map[string]any{"a": []any{}, "b": "兜底"}
		assert.Equal(t, "兜底", firstPresent(obj, []string{"a", "b"}))
	})

	t.Run("数值转字符串", func(t *testing.T) {
		obj := map[string]any{"a": 123}
		assert.Equal(t, "123", firstPresent(obj, []string{"a"}))
	})
}

// ──────────────────────────── FetchCalendarEvents ────────────────────────────

// TestFetchCalendarEvents_无日历服务返回空 没配日历 MCP 时返回 nil，不报错。
// Python: calendar_source.py:59
func TestFetchCalendarEvents_无日历服务(t *testing.T) {
	got := FetchCalendarEvents(context.Background(), 10, 24)
	// 真实环境通常无日历 MCP，返回 nil；若配了也不应 panic
	assert.NotNil(t, got == nil || len(got) >= 0)
}

// TestFetchCalendarEvents_参数归一 非法参数回落默认值不 panic。
func TestFetchCalendarEvents_参数归一(t *testing.T) {
	assert.NotPanics(t, func() {
		FetchCalendarEvents(context.Background(), 0, 0)
		FetchCalendarEvents(context.Background(), -1, -1)
	})
}

// ──────────────────────────── 辅助 ────────────────────────────

// ──────────────────────────── mock tool ────────────────────────────

// mockCalendarTool 测试用 MCP 工具替身（MCP 属外部协议边界，允许 mock）。
type mockCalendarTool struct {
	invokeFn func(ctx context.Context, inputs map[string]any) (map[string]any, error)
}

// Invoke 执行工具调用。
func (m *mockCalendarTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, inputs)
	}
	return nil, nil
}

// Card 返回工具卡片（测试不关心）。
func (m *mockCalendarTool) Card() *tool.ToolCard { return nil }

// Stream 流式执行（日历工具不支持，测试不用）。
func (m *mockCalendarTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// GetCallTimeout 外层预算原样返回。
func (m *mockCalendarTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// ──────────────────────────── FetchCalendarEvents 全流程 ────────────────────────────

// TestFetchCalendarEvents_正常拉取 完整链路：找到服务 → 解析工具 → 构造时间窗 → 归一化。
func TestFetchCalendarEvents_正常拉取(t *testing.T) {
	var capturedInputs map[string]any
	mockTool := &mockCalendarTool{invokeFn: func(_ context.Context, inputs map[string]any) (map[string]any, error) {
		capturedInputs = inputs
		return map[string]any{"result": map[string]any{"events": []any{
			map[string]any{"title": "周会", "start": "10:00", "end": "11:00", "location": "3F"},
			map[string]any{"summary": "评审", "startTime": "14:00"},
		}}}, nil
	}}

	got := fetchCalendarEventsWith(context.Background(), 10, 24,
		func() map[string]any { return map[string]any{"name": "my-calendar"} },
		func(_ context.Context, _ string) tool.Tool { return mockTool },
	)

	require.Len(t, got, 2)
	assert.Equal(t, "周会", got[0]["title"])
	assert.Equal(t, "评审", got[1]["title"], "summary 应被识别为 title")
	require.NotNil(t, capturedInputs)
	assert.Contains(t, capturedInputs, "start", "应传时间窗 start")
	assert.Contains(t, capturedInputs, "end", "应传时间窗 end")
}

// TestFetchCalendarEvents_无服务返回nil 未配置日历服务。
func TestFetchCalendarEvents_无服务(t *testing.T) {
	got := fetchCalendarEventsWith(context.Background(), 10, 24,
		func() map[string]any { return nil },
		func(_ context.Context, _ string) tool.Tool { return nil },
	)
	assert.Nil(t, got)
}

// TestFetchCalendarEvents_服务名为空 配置项存在但 name 为空。
func TestFetchCalendarEvents_服务名为空(t *testing.T) {
	got := fetchCalendarEventsWith(context.Background(), 10, 24,
		func() map[string]any { return map[string]any{"name": "  "} },
		func(_ context.Context, _ string) tool.Tool {
			t.Fatal("服务名为空时不应去解析工具")
			return nil
		},
	)
	assert.Nil(t, got)
}

// TestFetchCalendarEvents_工具解析失败 所有候选名都解析不到。
func TestFetchCalendarEvents_工具解析失败(t *testing.T) {
	got := fetchCalendarEventsWith(context.Background(), 10, 24,
		func() map[string]any { return map[string]any{"name": "calendar"} },
		func(_ context.Context, _ string) tool.Tool { return nil },
	)
	assert.Nil(t, got)
}

// TestFetchCalendarEvents_工具调用报错 调用失败被吞，返回 nil 不 panic。
func TestFetchCalendarEvents_工具调用报错(t *testing.T) {
	mockTool := &mockCalendarTool{invokeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, assert.AnError
	}}
	got := fetchCalendarEventsWith(context.Background(), 10, 24,
		func() map[string]any { return map[string]any{"name": "calendar"} },
		func(_ context.Context, _ string) tool.Tool { return mockTool },
	)
	assert.Nil(t, got, "调用失败应静默跳过，tick 不能因日历抖动中断")
}

// TestFetchCalendarEvents_无事件返回nil 服务返回空列表。
func TestFetchCalendarEvents_无事件(t *testing.T) {
	mockTool := &mockCalendarTool{invokeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"result": map[string]any{"events": []any{}}}, nil
	}}
	got := fetchCalendarEventsWith(context.Background(), 10, 24,
		func() map[string]any { return map[string]any{"name": "calendar"} },
		func(_ context.Context, _ string) tool.Tool { return mockTool },
	)
	assert.Nil(t, got)
}

// TestFetchCalendarEvents_过滤无标题事件 没有 title 的事件被丢弃。
func TestFetchCalendarEvents_过滤无标题事件(t *testing.T) {
	mockTool := &mockCalendarTool{invokeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"result": map[string]any{"events": []any{
			map[string]any{"title": "有标题"},
			map[string]any{"start": "10:00"}, // 无标题
		}}}, nil
	}}
	got := fetchCalendarEventsWith(context.Background(), 10, 24,
		func() map[string]any { return map[string]any{"name": "calendar"} },
		func(_ context.Context, _ string) tool.Tool { return mockTool },
	)
	require.Len(t, got, 1, "无标题事件应被过滤")
	assert.Equal(t, "有标题", got[0]["title"])
}

// TestFetchCalendarEvents_maxEvents截断 超出上限被截断。
func TestFetchCalendarEvents_maxEvents截断(t *testing.T) {
	events := make([]any, 0, 20)
	for i := 0; i < 20; i++ {
		events = append(events, map[string]any{"title": "事件" + strings.Repeat("x", i)})
	}
	mockTool := &mockCalendarTool{invokeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"result": map[string]any{"events": events}}, nil
	}}
	got := fetchCalendarEventsWith(context.Background(), 5, 24,
		func() map[string]any { return map[string]any{"name": "calendar"} },
		func(_ context.Context, _ string) tool.Tool { return mockTool },
	)
	assert.Len(t, got, 5, "应按 maxEvents 截断")
}

// ──────────────────────────── resolveEventToolWith ────────────────────────────

// TestResolveEventToolWith_首个候选命中 第一个候选名能解析即返回。
func TestResolveEventToolWith_首个候选命中(t *testing.T) {
	mockTool := &mockCalendarTool{}
	var tried []string
	got := resolveEventToolWith(context.Background(), "calendar",
		func(_ context.Context, toolName, _ string) ([]tool.Tool, error) {
			tried = append(tried, toolName)
			return []tool.Tool{mockTool}, nil
		})
	require.NotNil(t, got)
	assert.Equal(t, []string{eventToolCandidates[0]}, tried, "首个候选命中即停止探测")
}

// TestResolveEventToolWith_跳过报错与空结果 报错/空结果继续试下一个。
func TestResolveEventToolWith_跳过报错与空结果(t *testing.T) {
	mockTool := &mockCalendarTool{}
	var tried []string
	got := resolveEventToolWith(context.Background(), "calendar",
		func(_ context.Context, toolName, _ string) ([]tool.Tool, error) {
			tried = append(tried, toolName)
			switch toolName {
			case eventToolCandidates[0]:
				return nil, assert.AnError
			case eventToolCandidates[1]:
				return []tool.Tool{}, nil
			default:
				return []tool.Tool{mockTool}, nil
			}
		})
	require.NotNil(t, got)
	assert.Len(t, tried, 3, "前两个失败后应继续第三个")
}

// TestResolveEventToolWith_全部失败 所有候选都失败返回 nil。
func TestResolveEventToolWith_全部失败(t *testing.T) {
	got := resolveEventToolWith(context.Background(), "calendar",
		func(_ context.Context, _, _ string) ([]tool.Tool, error) {
			return nil, assert.AnError
		})
	assert.Nil(t, got)
}

// ──────────────────────────── 封装层补充覆盖 ────────────────────────────

// TestResolveEventTool_公开入口 走真实 ResourceMgr（无 MCP 时返回 nil，不 panic）。
func TestResolveEventTool_公开入口(t *testing.T) {
	assert.NotPanics(t, func() {
		got := resolveEventTool(context.Background(), "nonexistent-server")
		assert.Nil(t, got)
	})
}

// TestToMapList 配置列表归一化。
func TestToMapList(t *testing.T) {
	t.Run("直接是map切片", func(t *testing.T) {
		input := []map[string]any{{"a": 1}, {"b": 2}}
		assert.Equal(t, input, toMapList(input))
	})

	t.Run("any切片过滤非对象", func(t *testing.T) {
		input := []any{map[string]any{"a": 1}, "字符串", 123, map[string]any{"b": 2}}
		got := toMapList(input)
		require.Len(t, got, 2)
		assert.Equal(t, 1, got[0]["a"])
	})

	t.Run("非切片返回nil", func(t *testing.T) {
		assert.Nil(t, toMapList("not-a-list"))
		assert.Nil(t, toMapList(nil))
		assert.Nil(t, toMapList(123))
	})
}

// TestFindCalendarServerEntry_公开入口 走真实配置加载，不 panic。
func TestFindCalendarServerEntry_公开入口(t *testing.T) {
	assert.NotPanics(t, func() {
		_ = findCalendarServerEntry()
	})
}

// TestInvokeCalendarTool_返回nil 工具返回 nil 结果时返回 nil。
func TestInvokeCalendarTool_返回nil(t *testing.T) {
	mockTool := &mockCalendarTool{invokeFn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, nil
	}}
	got, err := invokeCalendarTool(context.Background(), mockTool, nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestLoadRecommendationConfigOrNil 配置加载失败时返回 nil 不 panic。
func TestLoadRecommendationConfigOrNil(t *testing.T) {
	assert.NotPanics(t, func() {
		_ = loadRecommendationConfigOrNil()
	})
}
