package tool_discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockSessionFacade 模拟会话门面
type mockSessionFacade struct{}

func (m *mockSessionFacade) GetSessionID() string                             { return "test-session" }
func (m *mockSessionFacade) UpdateState(_ map[string]any)                     {}
func (m *mockSessionFacade) GetState(_ state.StateKey) (any, error)           { return nil, nil }
func (m *mockSessionFacade) DumpState() map[string]any                        { return nil }
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error       { return nil }
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (m *mockSessionFacade) GetEnv(_ string, _ ...any) any                    { return nil }
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error          { return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSearchToolsTool_Card 测试工具卡片
func TestSearchToolsTool_Card(t *testing.T) {
	searchFn := func(_ context.Context, _ string, _ int, _ int) ([]map[string]any, error) {
		return nil, nil
	}
	traceFn := func(_ interfaces.SessionFacade, _ map[string]any) {}
	st := NewSearchToolsTool(searchFn, traceFn, "cn", "agent-1")

	card := st.Card()
	if card == nil {
		t.Fatal("Card() 不应为 nil")
	}
	if card.Name != "search_tools" {
		t.Errorf("期望 name=search_tools，实际 %s", card.Name)
	}
	if card.Properties["tool_id"] != "SearchToolsTool" {
		t.Errorf("期望 tool_id=SearchToolsTool，实际 %v", card.Properties["tool_id"])
	}
}

// TestSearchToolsTool_Invoke_正常 测试正常调用
func TestSearchToolsTool_Invoke_正常(t *testing.T) {
	called := false
	searchFn := func(_ context.Context, query string, limit int, detailLevel int) ([]map[string]any, error) {
		called = true
		if query != "搜索文件" {
			t.Errorf("期望 query=搜索文件，实际 %s", query)
		}
		if limit != 10 {
			t.Errorf("期望 limit=10，实际 %d", limit)
		}
		if detailLevel != 1 {
			t.Errorf("期望 detailLevel=1，实际 %d", detailLevel)
		}
		return []map[string]any{
			{"name": "read_file", "description": "读取文件"},
			{"name": "write_file", "description": "写入文件"},
		}, nil
	}
	traceCalled := false
	traceFn := func(_ interfaces.SessionFacade, event map[string]any) {
		traceCalled = true
		if event["event_type"] != "tool_search" {
			t.Errorf("期望 event_type=tool_search，实际 %v", event["event_type"])
		}
	}
	st := NewSearchToolsTool(searchFn, traceFn, "cn", "agent-1")

	result, err := st.Invoke(
		context.Background(),
		map[string]any{"query": "搜索文件"},
		tool.WithToolSession(&mockSessionFacade{}),
	)
	if err != nil {
		t.Fatalf("Invoke 不应返回错误：%v", err)
	}
	if !called {
		t.Fatal("searchFn 应被调用")
	}
	if !traceCalled {
		t.Fatal("traceFn 应被调用")
	}
	if result["query"] != "搜索文件" {
		t.Errorf("期望 query=搜索文件，实际 %v", result["query"])
	}
	if result["count"] != 2 {
		t.Errorf("期望 count=2，实际 %v", result["count"])
	}
	matches, ok := result["matches"].([]map[string]any)
	if !ok {
		t.Fatal("matches 应为 []map[string]any")
	}
	if len(matches) != 2 {
		t.Errorf("期望 len(matches)=2，实际 %d", len(matches))
	}
	if _, ok := result["callability_note"]; !ok {
		t.Error("结果应包含 callability_note")
	}
	if _, ok := result["next_step_hint"]; !ok {
		t.Error("结果应包含 next_step_hint")
	}
}

// TestSearchToolsTool_Invoke_限幅 测试 limit 限幅到 [1, 20]
func TestSearchToolsTool_Invoke_限幅(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"负数应限幅为1", -5, 1},
		{"零应限幅为1", 0, 1},
		{"1应保持", 1, 1},
		{"10应保持", 10, 10},
		{"20应保持", 20, 20},
		{"超过20应限幅为20", 25, 20},
		{"100应限幅为20", 100, 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var actualLimit int
			searchFn := func(_ context.Context, _ string, limit int, _ int) ([]map[string]any, error) {
				actualLimit = limit
				return nil, nil
			}
			traceFn := func(_ interfaces.SessionFacade, _ map[string]any) {}
			st := NewSearchToolsTool(searchFn, traceFn, "cn", "agent-1")

			_, err := st.Invoke(
				context.Background(),
				map[string]any{"query": "test", "limit": tc.input},
			)
			if err != nil {
				t.Fatalf("Invoke 不应返回错误：%v", err)
			}
			if actualLimit != tc.expected {
				t.Errorf("期望 limit=%d，实际 %d", tc.expected, actualLimit)
			}
		})
	}
}

// TestSearchToolsTool_Invoke_错误 测试搜索回调返回错误
func TestSearchToolsTool_Invoke_错误(t *testing.T) {
	searchFn := func(_ context.Context, _ string, _ int, _ int) ([]map[string]any, error) {
		return nil, errors.New("搜索服务不可用")
	}
	traceFn := func(_ interfaces.SessionFacade, _ map[string]any) {}
	st := NewSearchToolsTool(searchFn, traceFn, "cn", "agent-1")

	result, err := st.Invoke(
		context.Background(),
		map[string]any{"query": "test"},
	)
	if err != nil {
		t.Fatalf("Invoke 不应返回 error（错误应在 result 中）：%v", err)
	}
	if result["success"] != false {
		t.Errorf("期望 success=false，实际 %v", result["success"])
	}
	if result["error"] == nil {
		t.Error("结果应包含 error 字段")
	}
}

// TestSearchToolsTool_Stream 测试流式调用返回不支持错误
func TestSearchToolsTool_Stream(t *testing.T) {
	searchFn := func(_ context.Context, _ string, _ int, _ int) ([]map[string]any, error) {
		return nil, nil
	}
	traceFn := func(_ interfaces.SessionFacade, _ map[string]any) {}
	st := NewSearchToolsTool(searchFn, traceFn, "cn", "agent-1")

	_, err := st.Stream(context.Background(), nil)
	if err == nil {
		t.Fatal("Stream 应返回错误")
	}
}

// TestClampLimit 测试 clampLimit 函数
func TestClampLimit(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{-10, 1},
		{0, 1},
		{1, 1},
		{5, 5},
		{20, 20},
		{21, 20},
		{100, 20},
	}
	for _, tc := range tests {
		result := clampLimit(tc.input)
		if result != tc.expected {
			t.Errorf("clampLimit(%d) = %d，期望 %d", tc.input, result, tc.expected)
		}
	}
}
