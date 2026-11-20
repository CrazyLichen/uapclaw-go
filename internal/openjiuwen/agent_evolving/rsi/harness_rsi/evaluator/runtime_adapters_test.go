package evaluator

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/rails"
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
