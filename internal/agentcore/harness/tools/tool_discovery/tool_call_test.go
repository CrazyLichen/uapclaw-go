package tool_discovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// fakeToolCallbackContext 测试用回调上下文。
type fakeToolCallbackContext struct {
	// extra 调用级扩展数据
	extra map[string]any
	// session 会话门面
	session sessioninterfaces.SessionFacade
}

// Extra 返回调用级扩展数据。
func (f *fakeToolCallbackContext) Extra() map[string]any { return f.extra }

// Session 返回会话门面。
func (f *fakeToolCallbackContext) Session() sessioninterfaces.SessionFacade { return f.session }

// TestToolCallTool_缺少回调上下文报错 验证没有回调上下文时直接报错。
func TestToolCallTool_缺少回调上下文报错(t *testing.T) {
	callTool := NewToolCallTool("cn", "")
	_, err := callTool.Invoke(context.Background(), map[string]any{"name": "x", "args": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "active agent callback context") {
		t.Fatalf("应报缺少回调上下文，got %v", err)
	}
}

// TestToolCallTool_空名称报错 验证 name 为空时的文案。
func TestToolCallTool_空名称报错(t *testing.T) {
	callTool := NewToolCallTool("cn", "")
	_, err := callTool.InvokeWithCallback(context.Background(), map[string]any{"name": "  "}, &fakeToolCallbackContext{})
	if err == nil || !strings.Contains(err.Error(), "non-empty tool name") {
		t.Fatalf("应报名称非空，got %v", err)
	}
}

// TestRelayedToolOutput_只转发渲染文本 验证渲染文本走映射键而非结构化字段。
func TestRelayedToolOutput_只转发渲染文本(t *testing.T) {
	out := NewRelayedToolOutput(true, "渲染文本", map[string]any{"name": "x"}, nil)
	if out.RenderedText() != "渲染文本" {
		t.Fatal("渲染文本应可从私有字段读取")
	}
	payload := out.ToMap()
	if payload[mappedContentKey] != "渲染文本" {
		t.Fatal("模型可见文本应写入映射键")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatal("结构化结果应保留")
	}
	if _, exists := data["renderedText"]; exists {
		t.Fatal("结构化字段不应包含渲染文本")
	}
}

// TestToolCallTool_透传回调结果 验证分发与渲染转发。
func TestToolCallTool_透传回调结果(t *testing.T) {
	dispatched := 0
	callTool := NewToolCallTool("cn", "")
	callTool.SetDispatch(func(_ context.Context, name string, _ map[string]any, _ tool.ToolCallbackContext) (any, error) {
		dispatched++
		return NewRelayedToolOutput(true, "目标渲染", map[string]any{"name": name}, nil), nil
	})
	cb := &fakeToolCallbackContext{}
	out, err := callTool.InvokeWithCallback(context.Background(), map[string]any{
		"name": "xlsx_read", "args": map[string]any{"sheet": "S1"},
	}, cb)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("分发应执行一次，got %d", dispatched)
	}
	if out.RenderedText() != "目标渲染" {
		t.Fatalf("应转发目标渲染文本，got %s", out.RenderedText())
	}
}

// TestToolCallTool_错误透传 验证目标错误原样上抛。
func TestToolCallTool_错误透传(t *testing.T) {
	callTool := NewToolCallTool("cn", "")
	callTool.SetDispatch(func(context.Context, string, map[string]any, tool.ToolCallbackContext) (any, error) {
		return nil, errors.New("boom")
	})
	_, err := callTool.InvokeWithCallback(
		context.Background(), map[string]any{"name": "x", "args": map[string]any{}}, &fakeToolCallbackContext{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("目标错误应透传，got %v", err)
	}
}

// TestToolCallTool_非包装结果兜底 验证非 RelayedToolOutput 结果被包装。
func TestToolCallTool_非包装结果兜底(t *testing.T) {
	callTool := NewToolCallTool("cn", "")
	callTool.SetDispatch(func(context.Context, string, map[string]any, tool.ToolCallbackContext) (any, error) {
		return "raw", nil
	})
	out, err := callTool.InvokeWithCallback(
		context.Background(), map[string]any{"name": "x", "args": map[string]any{}}, &fakeToolCallbackContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Success() {
		t.Fatal("应包装为成功结果")
	}
	if out.Data()["result"] != "raw" {
		t.Fatalf("原始结果应放入 data.result，got %v", out.Data())
	}
}

// TestToolCallTool_元工具暴露 验证卡片显式 direct 且声明接受回调上下文。
func TestToolCallTool_元工具暴露(t *testing.T) {
	callTool := NewToolCallTool("cn", "agent-1")
	card := callTool.Card()
	if card.Name != "tool_call" {
		t.Fatalf("名称应为 tool_call，got %s", card.Name)
	}
	if card.EffectiveExposure() != tool.ToolExposureDirect {
		t.Fatalf("元工具须直接可调用，got %s", card.EffectiveExposure())
	}
	if declared, ok := card.GetExposureDeclared(); !ok || !declared {
		t.Fatal("元工具应锁定曝光声明")
	}
	acceptor, ok := any(callTool).(tool.CallbackContextAcceptor)
	if !ok || !acceptor.AcceptsToolCallbackContext() {
		t.Fatal("tool_call 必须声明接受回调上下文")
	}
}
