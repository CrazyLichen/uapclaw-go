package browser_move

import (
	"context"
	"errors"
	"testing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeBrowserRuntime 可控的 BrowserRuntime 假实现。
type fakeBrowserRuntime struct {
	result map[string]any
	err    error
	called bool
}

// RunBrowserTask 记录调用并返回预设结果。
func (f *fakeBrowserRuntime) RunBrowserTask(_ context.Context, _ string, _ string, _ string, _ *int) (map[string]any, error) {
	f.called = true
	return f.result, f.err
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestActionController_RunnerBinding 验证运行器绑定、清除与快照暴露。
func TestActionController_RunnerBinding(t *testing.T) {
	c := NewActionController()

	t.Run("初始态运行器为空", func(t *testing.T) {
		if c.RuntimeRunner() != nil {
			t.Fatal("初始 runtimeRunner 应为 nil")
		}
		if c.CodeExecutor() != nil {
			t.Fatal("初始 codeExecutor 应为 nil")
		}
	})

	t.Run("BindRuntimeRunner 后可读取", func(t *testing.T) {
		runner := func(context.Context, string, string, string, *int) ActionResult {
			return ActionResult{"ok": true}
		}
		c.BindRuntimeRunner(runner)
		if c.RuntimeRunner() == nil {
			t.Fatal("绑定后运行器不应为 nil")
		}
	})

	t.Run("ClearRuntimeRunner 清空", func(t *testing.T) {
		c.ClearRuntimeRunner()
		if c.RuntimeRunner() != nil {
			t.Fatal("清除后应为 nil")
		}
	})

	t.Run("CodeExecutor 绑定与清除", func(t *testing.T) {
		c.BindCodeExecutor(func(context.Context, string) (any, error) {
			return nil, nil
		})
		if c.CodeExecutor() == nil {
			t.Fatal("绑定后不应为 nil")
		}
		c.ClearCodeExecutor()
		if c.CodeExecutor() != nil {
			t.Fatal("清除后应为 nil")
		}
	})
}

// TestActionController_BindRuntime 验证运行器包装的三条返回路径。
func TestActionController_BindRuntime(t *testing.T) {
	ctx := context.Background()

	t.Run("成功时透传结果", func(t *testing.T) {
		c := NewActionController()
		rt := &fakeBrowserRuntime{result: map[string]any{"ok": true, "value": 1}}
		if err := c.BindRuntime(rt); err != nil {
			t.Fatalf("绑定失败: %v", err)
		}
		runner := c.RuntimeRunner()
		if runner == nil {
			t.Fatal("绑定后运行器不应为 nil")
		}
		got := runner(ctx, "task", "s1", "r1", nil)
		if got["ok"] != true || got["value"] != 1 {
			t.Fatalf("应透传运行时结果，实际 %v", got)
		}
		if !rt.called {
			t.Fatal("应调用底层 RunBrowserTask")
		}
	})

	t.Run("运行时出错包装为 ok=false", func(t *testing.T) {
		c := NewActionController()
		_ = c.BindRuntime(&fakeBrowserRuntime{err: errors.New("boom")})
		got := c.RuntimeRunner()(ctx, "task", "s1", "r1", nil)
		if got["ok"] != false {
			t.Fatalf("出错时应 ok=false，实际 %v", got)
		}
		if got["error"] != "boom" {
			t.Fatalf("error 应透传错误文本，实际 %v", got["error"])
		}
	})

	t.Run("运行时返回 nil 结果时报 empty result", func(t *testing.T) {
		c := NewActionController()
		_ = c.BindRuntime(&fakeBrowserRuntime{result: nil})
		got := c.RuntimeRunner()(ctx, "task", "s1", "r1", nil)
		if got["ok"] != false || got["error"] != "empty result" {
			t.Fatalf("应报 empty result，实际 %v", got)
		}
	})
}

// TestActionController_SnapshotRestore 验证快照与恢复的往返一致性。
func TestActionController_SnapshotRestore(t *testing.T) {
	c := NewActionController()
	handler := ActionHandler(func(context.Context, string, string, map[string]any) ActionResult { return ActionResult{"ok": true} })
	if err := c.RegisterAction("click", handler, false); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	snap := c.Snapshot()
	if snap == nil {
		t.Fatal("快照不应为 nil")
	}
	for _, key := range []string{"actions", "action_specs", "runtime_runner", "code_executor"} {
		if _, ok := snap[key]; !ok {
			t.Fatalf("快照缺少字段 %q", key)
		}
	}

	// 恢复到新控制器
	restored := NewActionController()
	restored.Restore(snap)
	got := restored.Snapshot()
	actions, _ := got["actions"].(map[string]ActionHandler)
	if len(actions) != 1 {
		t.Fatalf("恢复后应有 1 个动作，实际 %v", got["actions"])
	}
	if _, ok := actions["click"]; !ok {
		t.Fatalf("应恢复 click 动作，实际 %v", actions)
	}
}

// TestActionController_Restore_空快照 验证空快照恢复为空状态且不 panic。
func TestActionController_Restore_空快照(t *testing.T) {
	c := NewActionController()
	c.Restore(map[string]any{})
	snap := c.Snapshot()
	if actions, _ := snap["actions"].(map[string]ActionHandler); len(actions) != 0 {
		t.Fatalf("空快照恢复后动作应为空，实际 %v", actions)
	}
}
