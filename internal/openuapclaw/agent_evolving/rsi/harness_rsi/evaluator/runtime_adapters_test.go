package evaluator

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	evolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
)

// fakeBashTool 记录 Invoke 入参的桩工具。
type fakeBashTool struct {
	invokedWith map[string]any
}

func (f *fakeBashTool) Card() *tool.ToolCard {
	card := &tool.ToolCard{}
	card.ID = "bash"
	card.Name = "bash"
	return card
}

func (f *fakeBashTool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	f.invokedWith = inputs
	return map[string]any{"ok": true}, nil
}

func (f *fakeBashTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, errors.New("not supported")
}

func (f *fakeBashTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// TestRSIBashToolDecorator_pipefail注入 对照 Python RSIBashTool.invoke。
// Python: runtime_adapters.py:93-102
func TestRSIBashToolDecorator_pipefail注入(t *testing.T) {
	// ① pipefail=true：无前缀命令注入前缀 + 强制 bash
	fake := &fakeBashTool{}
	decorator := NewRSIBashToolDecorator(fake, true)
	_, err := decorator.Invoke(context.Background(), map[string]any{"command": "ls | wc -l"})
	if err != nil {
		t.Fatal(err)
	}
	if got := fake.invokedWith["command"]; got != "set -o pipefail; ls | wc -l" {
		t.Fatalf("应注入 pipefail 前缀, got %v", got)
	}
	if got := fake.invokedWith["shell_type"]; got != "bash" {
		t.Fatalf("shell_type 应强制 bash, got %v", got)
	}

	// ② 已有前缀不重复注入
	fake2 := &fakeBashTool{}
	decorator2 := NewRSIBashToolDecorator(fake2, true)
	_, _ = decorator2.Invoke(context.Background(), map[string]any{"command": "set -o pipefail; ls"})
	if got := fake2.invokedWith["command"]; got != "set -o pipefail; ls" {
		t.Fatalf("已有前缀不应重复注入, got %v", got)
	}

	// ③ 空命令不注入前缀但仍强制 shell_type
	fake3 := &fakeBashTool{}
	decorator3 := NewRSIBashToolDecorator(fake3, true)
	_, _ = decorator3.Invoke(context.Background(), map[string]any{})
	if got, ok := fake3.invokedWith["command"]; ok && got != "" {
		t.Fatalf("空命令不应注入前缀, got %v", got)
	}
	if got := fake3.invokedWith["shell_type"]; got != "bash" {
		t.Fatalf("空命令仍应强制 bash, got %v", got)
	}

	// ④ pipefail=false：完全透传（基类行为）
	fake4 := &fakeBashTool{}
	decorator4 := NewRSIBashToolDecorator(fake4, false)
	_, _ = decorator4.Invoke(context.Background(), map[string]any{"command": "ls"})
	if got := fake4.invokedWith["command"]; got != "ls" {
		t.Fatalf("pipefail=false 应透传, got %v", got)
	}
	if _, ok := fake4.invokedWith["shell_type"]; ok {
		t.Fatal("pipefail=false 不应设置 shell_type")
	}

	// ⑤ Card/Stream 透传
	if decorator.Card().ID != "bash" {
		t.Fatal("Card 应透传")
	}
	if _, err := decorator.Stream(context.Background(), nil); err == nil {
		t.Fatal("Stream 应透传基类错误")
	}
}

// TestNewRSISysOperationRail_选项透传 验证 RSI 选项与基类选项组合装配。
// Python: runtime_adapters.py:108-119 __init__（kwargs 透传基类）
func TestNewRSISysOperationRail_选项透传(t *testing.T) {
	// ① RSI 三选项 + 基类 read_only/deny_patterns 组合构造不 panic，优先级继承基类
	rail := NewRSISysOperationRail(true, true, false,
		rails.WithReadOnly(true),
		rails.WithDenyPatterns([]string{`find /`, `\.\.`}),
	)
	if rail == nil {
		t.Fatal("构造不应返回 nil")
	}

	// ② 非 pipefail 路径不注册装饰器
	rail2 := NewRSISysOperationRail(false, false, true)
	if rail2 == nil {
		t.Fatal("构造不应返回 nil")
	}

	// ③ 默认 allowShell=true 与基类行为一致（不显式传 RSI 选项的等价构造）
	rail3 := rails.NewSysOperationRail()
	if rail3 == nil {
		t.Fatal("基类构造不应返回 nil")
	}
}

// ──────────────────────────── RSITrajectoryRail / recovery（17.6a） ────────────────────────────

// stubInvokeContext 构造 invoke 上下文（零值 extraMuPtr 会 panic）。
func stubInvokeContext() *agentinterfaces.AgentCallbackContext {
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-rsi-traj"})
	return cbc
}

// stubModelContext / stubToolContext 构造模型/工具回调上下文（基类按 inputs 类型分派）。
func stubModelContext() *agentinterfaces.AgentCallbackContext {
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ModelCallInputs{})
	return cbc
}

func stubToolContext() *agentinterfaces.AgentCallbackContext {
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{ToolName: "t"})
	return cbc
}

// recordRSIToolSpan 直接注入 builder tool span（不经过 AfterToolCall 消费）。
func recordRSIToolSpan(t *testing.T, rail *evolution.EvolutionRail, args, result string) {
	t.Helper()
	attrs := map[string]any{
		"gen_ai.operation.name": "execute_tool",
		"gen_ai.tool.name":      "t",
		"gen_ai.tool.call.id":   "",
	}
	if args != "" {
		attrs["gen_ai.tool.call.arguments"] = args
	}
	if result != "" {
		attrs["gen_ai.tool.call.result"] = result
	}
	if err := rail.Builder().RecordSpan(map[string]any{
		"name":       "execute_tool t",
		"attributes": attrs,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestRSITrajectoryRail_计数与问题采集 对齐 Python RSITrajectoryRail 三件。
func TestRSITrajectoryRail_计数与问题采集(t *testing.T) {
	rail := NewRSITrajectoryRail()
	if err := rail.BeforeInvoke(context.Background(), stubInvokeContext()); err != nil {
		t.Fatal(err)
	}
	// 合法 tool span 两次（无 issue）。
	recordRSIToolSpan(t, rail.EvolutionRail, `{"k":1}`, `["ok"]`)
	if err := rail.AfterToolCall(context.Background(), stubToolContext()); err != nil {
		t.Fatal(err)
	}
	if err := rail.AfterModelCall(context.Background(), stubModelContext()); err != nil {
		t.Fatal(err)
	}
	if rail.ToolCallCount() != 1 || rail.ModelCallCount() != 1 {
		t.Fatalf("计数不符: model=%d tool=%d", rail.ModelCallCount(), rail.ToolCallCount())
	}
	if issues := rail.CaptureIssues(); len(issues) != 0 {
		t.Fatalf("合法载荷不应有采集问题: %v", issues)
	}
	// 畸形 JSON → tool_payload_json_error 采集 + gate（增量不入窗口）。
	recordRSIToolSpan(t, rail.EvolutionRail, `{"broken`, "")
	if err := rail.AfterToolCall(context.Background(), stubToolContext()); err != nil {
		t.Fatal(err)
	}
	issues := rail.CaptureIssues()
	if len(issues) != 1 || issues[0] != "tool_payload_json_error" {
		t.Fatalf("应采集 tool_payload_json_error: %v", issues)
	}
	if rail.ToolCallCount() != 2 {
		t.Fatalf("第二次工具调用计数不符: %d", rail.ToolCallCount())
	}
}

// TestRSITrajectoryRail_归档链 25.2b：AfterInvoke 后归档不 panic 且可二次 invoke。
func TestRSITrajectoryRail_归档链(t *testing.T) {
	rail := NewRSITrajectoryRail()
	if err := rail.BeforeInvoke(context.Background(), stubInvokeContext()); err != nil {
		t.Fatal(err)
	}
	recordRSIToolSpan(t, rail.EvolutionRail, `{"k":1}`, `["ok"]`)
	if err := rail.AfterToolCall(context.Background(), stubToolContext()); err != nil {
		t.Fatal(err)
	}
	if err := rail.AfterInvoke(context.Background(), stubInvokeContext()); err != nil {
		t.Fatal(err)
	}
	if err := rail.BeforeInvoke(context.Background(), stubInvokeContext()); err != nil {
		t.Fatal(err)
	}
	if err := rail.AfterInvoke(context.Background(), stubInvokeContext()); err != nil {
		t.Fatal(err)
	}
}

// TestRunAgentWithEmptyResponseRecovery 对齐 Python 恢复链三分支。
func TestRunAgentWithEmptyResponseRecovery(t *testing.T) {
	t.Run("有输出不重跑", func(t *testing.T) {
		calls := 0
		result, err := RunAgentWithEmptyResponseRecovery(context.Background(),
			func(context.Context, map[string]any) (map[string]any, error) {
				calls++
				return map[string]any{"output": "答案"}, nil
			}, map[string]any{})
		if err != nil || calls != 1 {
			t.Fatalf("有输出不应重跑: calls=%d err=%v", calls, err)
		}
		if result["output"] != "答案" {
			t.Fatalf("应返回原响应: %v", result)
		}
	})
	t.Run("空输出重跑一次", func(t *testing.T) {
		calls := 0
		var recoveryQuery string
		result, err := RunAgentWithEmptyResponseRecovery(context.Background(),
			func(_ context.Context, inputs map[string]any) (map[string]any, error) {
				calls++
				if calls == 1 {
					// Python 空响应形态：None（Go nil）。
					return nil, nil
				}
				recoveryQuery, _ = inputs["query"].(string)
				return map[string]any{"output": "恢复后的答案"}, nil
			}, map[string]any{})
		if err != nil || calls != 2 {
			t.Fatalf("空输出应重跑一次: calls=%d err=%v", calls, err)
		}
		// RECOVERY 提示词逐字对齐。
		want := "[RECOVERY] The previous turn produced no answer or tool action. " +
			"Do not restart the analysis. Apply the smallest justified workspace " +
			"edit now, then run the acceptance probe."
		if recoveryQuery != want {
			t.Fatalf("RECOVERY 提示词漂移:\n got=%q\nwant=%q", recoveryQuery, want)
		}
		if result["output"] != "恢复后的答案" {
			t.Fatalf("应返回恢复响应: %v", result)
		}
	})
	t.Run("首次调用报错直接上抛", func(t *testing.T) {
		sentinel := errors.New("invoke failed")
		_, err := RunAgentWithEmptyResponseRecovery(context.Background(),
			func(context.Context, map[string]any) (map[string]any, error) {
				return nil, sentinel
			}, map[string]any{})
		if !errors.Is(err, sentinel) {
			t.Fatalf("错误应上抛: %v", err)
		}
	})
}

// TestResponseText_四键递归 对齐 Python _response_text。
func TestResponseText_四键递归(t *testing.T) {
	if responseText(nil) != "" {
		t.Fatal("nil 应空串")
	}
	if responseText("直接文本") != "直接文本" {
		t.Fatal("字符串原样")
	}
	if responseText(map[string]any{"output": "A"}) != "A" {
		t.Fatal("output 键优先")
	}
	if responseText(map[string]any{"content": map[string]any{"response": "B"}}) != "B" {
		t.Fatal("嵌套递归")
	}
	if responseText(map[string]any{"other": 1}) == "" {
		t.Fatal("无命中键应 JSON 序列化")
	}
	if responseText(map[string]any{"output": ""}) != "" {
		t.Fatal("命中键但空文本应继续判空")
	}
}
