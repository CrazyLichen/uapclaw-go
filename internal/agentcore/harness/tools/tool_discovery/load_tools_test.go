package tool_discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLoadToolsTool_Card 测试工具卡片
func TestLoadToolsTool_Card(t *testing.T) {
	loadFn := func(_ context.Context, _ interfaces.SessionFacade, _ []string, _ bool) (map[string]any, error) {
		return nil, nil
	}
	lt := NewLoadToolsTool(loadFn, "cn", "agent-1")

	card := lt.Card()
	if card == nil {
		t.Fatal("Card() 不应为 nil")
	}
	if card.Name != "load_tools" {
		t.Errorf("期望 name=load_tools，实际 %s", card.Name)
	}
}

// TestLoadToolsTool_Invoke_正常 测试正常调用
func TestLoadToolsTool_Invoke_正常(t *testing.T) {
	called := false
	loadFn := func(_ context.Context, session interfaces.SessionFacade, toolNames []string, replace bool) (map[string]any, error) {
		called = true
		if len(toolNames) != 2 {
			t.Errorf("期望 len(toolNames)=2，实际 %d", len(toolNames))
		}
		if toolNames[0] != "read_file" {
			t.Errorf("期望 toolNames[0]=read_file，实际 %s", toolNames[0])
		}
		if replace {
			t.Error("期望 replace=false")
		}
		if session == nil {
			t.Error("session 不应为 nil")
		}
		return map[string]any{
			"success":       true,
			"loaded_tools":  toolNames,
			"visible_tools": []string{"read_file", "write_file", "read_file"},
			"skipped_tools": []string{},
		}, nil
	}

	lt := NewLoadToolsTool(loadFn, "cn", "agent-1")

	result, err := lt.Invoke(
		context.Background(),
		map[string]any{
			"tool_names": []any{"read_file", "write_file"},
			"replace":    false,
		},
		tool.WithToolSession(&mockSessionFacade{}),
	)
	if err != nil {
		t.Fatalf("Invoke 不应返回错误：%v", err)
	}
	if !called {
		t.Fatal("loadFn 应被调用")
	}
	if result["success"] != true {
		t.Errorf("期望 success=true，实际 %v", result["success"])
	}
}

// TestLoadToolsTool_Invoke_替换模式 测试 replace=true
func TestLoadToolsTool_Invoke_替换模式(t *testing.T) {
	var actualReplace bool
	loadFn := func(_ context.Context, _ interfaces.SessionFacade, _ []string, replace bool) (map[string]any, error) {
		actualReplace = replace
		return map[string]any{"success": true}, nil
	}

	lt := NewLoadToolsTool(loadFn, "cn", "agent-1")

	_, err := lt.Invoke(
		context.Background(),
		map[string]any{
			"tool_names": []any{"read_file"},
			"replace":    true,
		},
	)
	if err != nil {
		t.Fatalf("Invoke 不应返回错误：%v", err)
	}
	if !actualReplace {
		t.Error("期望 replace=true")
	}
}

// TestLoadToolsTool_Invoke_错误 测试加载回调返回错误
func TestLoadToolsTool_Invoke_错误(t *testing.T) {
	loadFn := func(_ context.Context, _ interfaces.SessionFacade, _ []string, _ bool) (map[string]any, error) {
		return nil, errors.New("工具加载失败")
	}

	lt := NewLoadToolsTool(loadFn, "cn", "agent-1")

	result, err := lt.Invoke(
		context.Background(),
		map[string]any{
			"tool_names": []any{"nonexistent"},
		},
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

// TestLoadToolsTool_Stream 测试流式调用返回不支持错误
func TestLoadToolsTool_Stream(t *testing.T) {
	loadFn := func(_ context.Context, _ interfaces.SessionFacade, _ []string, _ bool) (map[string]any, error) {
		return nil, nil
	}
	lt := NewLoadToolsTool(loadFn, "cn", "agent-1")

	_, err := lt.Stream(context.Background(), nil)
	if err == nil {
		t.Fatal("Stream 应返回错误")
	}
}
