package tool_discovery

import (
	"context"
	"strings"
	"testing"

	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// TestNormalizeToolSearchLimit 验证条数归一化边界。
func TestNormalizeToolSearchLimit(t *testing.T) {
	cases := map[int]int{-5: 1, -1: 1, 0: 1, 1: 1, 5: 5, 20: 20, 21: 20, 100: 20}
	for input, want := range cases {
		if got := NormalizeToolSearchLimit(input); got != want {
			t.Fatalf("limit %d 应归一化为 %d，got %d", input, want, got)
		}
	}
}

// TestToolSearchTool_调用与渲染 验证搜索回调透传与模型可见文本。
func TestToolSearchTool_调用与渲染(t *testing.T) {
	called := 0
	search := func(_ context.Context, query string, limit int, _ sessioninterfaces.SessionFacade) ([]map[string]any, error) {
		called++
		if query != "excel" || limit != 3 {
			t.Fatalf("入参应透传，got query=%s limit=%d", query, limit)
		}
		return []map[string]any{{
			"name":        "xlsx_read",
			"description": "读取表格",
			"parameters":  map[string]any{"type": "object"},
		}}, nil
	}
	searchTool := NewToolSearchTool(search, "cn", "agent-1", DefaultToolSearchLimit)

	out, err := searchTool.Invoke(context.Background(), map[string]any{"query": "excel", "limit": 3})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if called != 1 {
		t.Fatalf("搜索回调应被调用一次，got %d", called)
	}
	if count, _ := out["count"].(int); count != 1 {
		t.Fatalf("count 应为 1，got %v", out["count"])
	}
	rendered, _ := out[mappedContentKey].(string)
	if !strings.Contains(rendered, "xlsx_read") || !strings.Contains(rendered, `"type":"object"`) {
		t.Fatalf("渲染须含名称与精确 schema，got %s", rendered)
	}
}

// TestToolSearchTool_空查询 验证空查询由检索侧短路（对齐上游：工具总会调用检索回调）。
func TestToolSearchTool_空查询(t *testing.T) {
	called := 0
	searchTool := NewToolSearchTool(
		func(_ context.Context, query string, _ int, _ sessioninterfaces.SessionFacade) ([]map[string]any, error) {
			called++
			if strings.TrimSpace(query) != "" {
				t.Fatalf("空查询应以空串透传，got %q", query)
			}
			return nil, nil
		}, "cn", "", DefaultToolSearchLimit)

	out, err := searchTool.Invoke(context.Background(), map[string]any{"query": "  "})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if called != 1 {
		t.Fatalf("上游语义：工具总会调用检索回调，got %d", called)
	}
	if count, _ := out["count"].(int); count != 0 {
		t.Fatalf("空查询结果应为 0，got %v", out["count"])
	}
	rendered, _ := out[mappedContentKey].(string)
	if !strings.Contains(rendered, "No tools matched") {
		t.Fatalf("空结果文案不符，got %s", rendered)
	}
}

// TestToolSearchTool_默认条数 验证缺省 limit 使用工具默认值。
func TestToolSearchTool_默认条数(t *testing.T) {
	var gotLimit int
	searchTool := NewToolSearchTool(
		func(_ context.Context, _ string, limit int, _ sessioninterfaces.SessionFacade) ([]map[string]any, error) {
			gotLimit = limit
			return nil, nil
		}, "cn", "", 7)

	if _, err := searchTool.Invoke(context.Background(), map[string]any{"query": "x"}); err != nil {
		t.Fatal(err)
	}
	if gotLimit != 7 {
		t.Fatalf("缺省应使用工具默认 7，got %d", gotLimit)
	}
}
