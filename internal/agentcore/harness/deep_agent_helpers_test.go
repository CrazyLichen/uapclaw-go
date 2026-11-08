package harness

import (
	"testing"

	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newBareDeepAgent 构造最小 DeepAgent（不配置、不初始化）。
func newBareDeepAgent(t *testing.T) *DeepAgent {
	t.Helper()
	return NewDeepAgent(agentschema.NewAgentCard(
		agentschema.WithAgentID("bare-agent"),
		agentschema.WithAgentName("bare-agent"),
	))
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestDeepAgent_SubagentOverrides 验证子 Agent 创建与调度覆写的读写闭环。
func TestDeepAgent_SubagentOverrides(t *testing.T) {
	d := newBareDeepAgent(t)

	t.Run("初始均为 nil", func(t *testing.T) {
		if d.CreateSubagentOverride() != nil {
			t.Fatal("初始 CreateSubagentOverride 应为 nil")
		}
	})

	t.Run("设置 nil 后仍为 nil", func(t *testing.T) {
		d.SetCreateSubagentOverride(nil)
		if d.CreateSubagentOverride() != nil {
			t.Fatal("设置 nil 后应为 nil")
		}
	})

	t.Run("调度覆写可设置并读回", func(t *testing.T) {
		d.SetScheduleAutoInvokeOverride(nil)
		if d.scheduleAutoInvokeOverride != nil {
			t.Fatal("设置 nil 后应为 nil")
		}
	})
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestMcpConfigEqual 验证 MCP 配置等价的比较语义。
func TestMcpConfigEqual(t *testing.T) {
	t.Run("完全相同的配置判等", func(t *testing.T) {
		a := &mcptypes.McpServerConfig{ServerID: "s1", ServerName: "n", ClientType: "sse"}
		b := &mcptypes.McpServerConfig{ServerID: "s1", ServerName: "n", ClientType: "sse"}
		if !mcpConfigEqual(a, b) {
			t.Fatal("相同配置应判等")
		}
	})

	t.Run("字段不同判不等", func(t *testing.T) {
		a := &mcptypes.McpServerConfig{ServerID: "s1"}
		b := &mcptypes.McpServerConfig{ServerID: "s2"}
		if mcpConfigEqual(a, b) {
			t.Fatal("不同配置应判不等")
		}
	})

	t.Run("nil 与空结构体判不等", func(t *testing.T) {
		if mcpConfigEqual(nil, &mcptypes.McpServerConfig{}) {
			t.Fatal("nil 与空结构体序列化结果不同，应判不等")
		}
	})

	t.Run("两者均为 nil 判等", func(t *testing.T) {
		if !mcpConfigEqual(nil, nil) {
			t.Fatal("均为 nil 时序列化结果相同，应判等")
		}
	})
}

// TestNormalizeInputs 验证用户输入归一化的各形态。
func TestNormalizeInputs(t *testing.T) {
	d := newBareDeepAgent(t)

	t.Run("map 中 query 为字符串", func(t *testing.T) {
		got, err := d.normalizeInputs(map[string]any{"query": "hi", "conversation_id": "c1"})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if got.ConversationID != "c1" {
			t.Fatalf("ConversationID = %q, 期望 c1", got.ConversationID)
		}
	})

	t.Run("map 缺少 query 时为空查询", func(t *testing.T) {
		if _, err := d.normalizeInputs(map[string]any{}); err != nil {
			t.Fatalf("不应报错: %v", err)
		}
	})

	t.Run("map 中 query 为非字符串时格式化", func(t *testing.T) {
		if _, err := d.normalizeInputs(map[string]any{"query": 42}); err != nil {
			t.Fatalf("不应报错: %v", err)
		}
	})

	t.Run("map 含 run 段落时解析 kind", func(t *testing.T) {
		if _, err := d.normalizeInputs(map[string]any{
			"query": "hi",
			"run":   map[string]any{"kind": "task"},
		}); err != nil {
			t.Fatalf("不应报错: %v", err)
		}
	})

	t.Run("非 map 输入报错或降级", func(t *testing.T) {
		// 具体行为由实现决定；此处只要求不 panic
		assertNoPanicHarness(t, func() { _, _ = d.normalizeInputs(42) })
	})
}

// assertNoPanicHarness 断言 fn 不 panic。
func assertNoPanicHarness(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("不应 panic，实际 %v", r)
		}
	}()
	fn()
}
