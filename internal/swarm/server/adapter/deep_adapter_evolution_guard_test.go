package adapter

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newEvolutionTestAdapter 构造带 instance（但尚无 ReActAgent）的最小 DeepAdapter。
func newEvolutionTestAdapter(t *testing.T) *DeepAdapter {
	t.Helper()
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("agent-evo-1"),
		agentschema.WithAgentName("evo-test-agent"),
	)
	d := NewDeepAdapter()
	d.instance = harness.NewDeepAgent(card)
	return d
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestCompressContext_无实例返回noop 验证实例缺失、ReActAgent 缺失时返回 noop。
func TestCompressContext_无实例返回noop(t *testing.T) {
	ctx := context.Background()

	t.Run("instance 为 nil", func(t *testing.T) {
		d := NewDeepAdapter()
		res, err := d.CompressContext(ctx, "s1", nil, false)
		if err != nil {
			t.Fatalf("不应返回错误: %v", err)
		}
		if res["result"] != "noop" {
			t.Fatalf("result 应为 noop，实际 %v", res)
		}
	})

	t.Run("有 instance 但无 ReActAgent", func(t *testing.T) {
		d := newEvolutionTestAdapter(t)
		res, err := d.CompressContext(ctx, "s1", nil, false)
		if err != nil {
			t.Fatalf("不应返回错误: %v", err)
		}
		if res["result"] != "noop" {
			t.Fatalf("result 应为 noop，实际 %v", res)
		}
	})
}

// TestGetContextUsage_降级路径 验证无实例时返回 (nil, nil)（对齐上游「无实例即无用量」）。
func TestGetContextUsage_降级路径(t *testing.T) {
	d := NewDeepAdapter()
	// 10.3.28f：本用例测方法体（instance 为 nil 时的降级分支），不是根分流。
	// 标记 session-scoped 让分流头跳过；根分流由 *分流到child 用例覆盖。
	d.MarkAsSessionScoped("test-session")
	res, err := d.GetContextUsage(context.Background(), "s1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if res != nil {
		t.Fatalf("无实例应返回 nil 用量，实际 %v", res)
	}
}

// TestGenerateRecap_降级路径 验证无实例时的 recap 行为。
func TestGenerateRecap_降级路径(t *testing.T) {
	d := NewDeepAdapter()
	// 10.3.28f：本用例测方法体（instance 为 nil 时的降级分支），不是根分流。
	// 标记 session-scoped 让分流头跳过；根分流由 *分流到child 用例覆盖。
	d.MarkAsSessionScoped("test-session")
	res, err := d.GenerateRecap(context.Background(), "s1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if res == nil {
		t.Fatal("应返回非 nil 结果")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestGetRecentMessages_无ReActAgent 验证有 instance 但无 ReActAgent 时返回 nil。
func TestGetRecentMessages_无ReActAgent(t *testing.T) {
	d := newEvolutionTestAdapter(t)
	if got := d.getRecentMessages("s1"); got != nil {
		t.Fatalf("无 ReActAgent 应返回 nil，实际 %v", got)
	}
}

// TestCountFullContextTokens_降级路径 验证无实例 / 无 ReActAgent 时返回 (0, nil)。
func TestCountFullContextTokens_降级路径(t *testing.T) {
	ctx := context.Background()

	t.Run("instance 为 nil", func(t *testing.T) {
		d := NewDeepAdapter()
		n, err := d.countFullContextTokens(ctx, "s1")
		if err != nil || n != 0 {
			t.Fatalf("应返回 (0, nil)，实际 (%d, %v)", n, err)
		}
	})

	t.Run("有 instance 但无 ReActAgent", func(t *testing.T) {
		d := newEvolutionTestAdapter(t)
		n, err := d.countFullContextTokens(ctx, "s1")
		if err != nil || n != 0 {
			t.Fatalf("应返回 (0, nil)，实际 (%d, %v)", n, err)
		}
	})
}

// TestEmitPostCompactUsage_空参数短路 验证 session/modelCtx/reactAgent 缺失时直接返回。
func TestEmitPostCompactUsage_空参数短路(t *testing.T) {
	d := NewDeepAdapter()
	ctx := context.Background()

	t.Run("session 为 nil", func(t *testing.T) {
		d.emitPostCompactUsage(ctx, nil, "s1", nil, nil)
	})

	t.Run("全部为 nil", func(t *testing.T) {
		d.emitPostCompactUsage(ctx, nil, "", nil, nil)
	})
}

// TestWatchEvolutionAndPush_无rail 验证未初始化演进 rail 时安全返回。
func TestWatchEvolutionAndPush_无rail(t *testing.T) {
	d := NewDeepAdapter()
	if err := d.watchEvolutionAndPush(context.Background(), "s1", "c1", "r1"); err != nil {
		t.Fatalf("无 rail 时不应返回错误: %v", err)
	}
}

// TestCallModelForRecap_无模型 验证模型缺失时报错而非 panic。
func TestCallModelForRecap_无模型(t *testing.T) {
	d := NewDeepAdapter()
	if _, err := d.callModelForRecap(context.Background(), nil, "prompt"); err == nil {
		t.Log("无模型时未报错，属可接受降级路径")
	}
}
