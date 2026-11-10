package harness

import (
	"context"
	"testing"

	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
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

// TestNormalizeInputs_用量归属血缘 验证六字段与 turn 号的解析与回写。
// Python: deep_agent.py L1871-1883（解析）/ L1985-2003（_to_effective_inputs 回写）
func TestNormalizeInputs_用量归属血缘(t *testing.T) {
	d := newBareDeepAgent(t)

	got, err := d.normalizeInputs(map[string]any{
		"query":                "hi",
		"parent_session_id":    "parent-1",
		"invocation_id":        "inv-1",
		"parent_invocation_id": "inv-parent",
		"delegation_id":        "deleg-1",
		"agent_path":           []string{"root", "child"},
		"depth":                1,
		"_turn_number":         3,
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got.ParentSessionID != "parent-1" || got.InvocationID != "inv-1" ||
		got.ParentInvocationID != "inv-parent" || got.DelegationID != "deleg-1" {
		t.Fatalf("血缘字段解析错误: %+v", got)
	}
	if len(got.AgentPath) != 2 || got.AgentPath[1] != "child" {
		t.Fatalf("AgentPath = %v, 期望 [root child]", got.AgentPath)
	}
	if got.Depth != 1 {
		t.Fatalf("Depth = %d, 期望 1", got.Depth)
	}
	if got.TurnNumber != 3 {
		t.Fatalf("TurnNumber = %d, 期望 3", got.TurnNumber)
	}

	effective := toEffectiveInputs(got)
	if effective["_turn_number"] != 3 {
		t.Fatalf("_turn_number 未回写: %v", effective["_turn_number"])
	}
	if effective["parent_session_id"] != "parent-1" || effective["invocation_id"] != "inv-1" ||
		effective["parent_invocation_id"] != "inv-parent" || effective["delegation_id"] != "deleg-1" {
		t.Fatalf("血缘字段未回写: %v", effective)
	}
	if effective["depth"] != 1 {
		t.Fatalf("depth 未回写: %v", effective["depth"])
	}
}

// TestNormalizeInputs_turn号非法值被忽略 验证非正整数一律视为未提供。
func TestNormalizeInputs_turn号非法值被忽略(t *testing.T) {
	d := newBareDeepAgent(t)

	for _, raw := range []any{0, -1, "3", 3.0} {
		got, err := d.normalizeInputs(map[string]any{"query": "hi", "_turn_number": raw})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if got.TurnNumber != 0 {
			t.Fatalf("_turn_number=%v 应被忽略，实际 %d", raw, got.TurnNumber)
		}
	}
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

// TestPrepareSingleRoundSession_按会话ID自建 验证单轮模式按 conversation_id 建会话并注 env。
// Python: DeepAgent._prepare_single_round_session (deep_agent.py L2053-2075)
func TestPrepareSingleRoundSession_按会话ID自建(t *testing.T) {
	d := newBareDeepAgent(t)
	inputs := &agentinterfaces.InvokeInputs{
		Query:           agentinterfaces.InvokeQueryString("hi"),
		ConversationID:  "conv-1",
		ParentSessionID: "product-1",
	}

	sess, err := d.prepareSingleRoundSession(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if sess == nil {
		t.Fatal("单轮模式应按 conversation_id 建会话")
	}
	if sess.GetSessionID() != "conv-1" {
		t.Fatalf("SessionID = %q, 期望 conv-1", sess.GetSessionID())
	}
	if got := sess.GetEnv(kv_cache.KVCacheAffinityParentSessionIDEnv); got != "product-1" {
		t.Fatalf("父会话 env 未注入: %v", got)
	}
}

// TestPrepareSingleRoundSession_已有会话不动 验证外部传入会话时原样返回。
func TestPrepareSingleRoundSession_已有会话不动(t *testing.T) {
	d := newBareDeepAgent(t)
	existing := session.NewSession(session.WithSessionID("given"))

	sess, err := d.prepareSingleRoundSession(context.Background(),
		&agentinterfaces.InvokeInputs{ConversationID: "conv-2"}, existing)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if sess != existing {
		t.Fatal("已有会话必须原样返回")
	}
}

// TestPrepareSingleRoundSession_无会话ID 验证缺 conversation_id 时不建会话。
func TestPrepareSingleRoundSession_无会话ID(t *testing.T) {
	d := newBareDeepAgent(t)

	sess, err := d.prepareSingleRoundSession(context.Background(), &agentinterfaces.InvokeInputs{}, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if sess != nil {
		t.Fatal("无 conversation_id 不应建会话")
	}
}

// TestPrepareSingleRoundSession_任务循环跳过 验证任务循环模式下会话归调用方。
func TestPrepareSingleRoundSession_任务循环跳过(t *testing.T) {
	d := newBareDeepAgent(t)
	d.deepConfig = &hschema.DeepAgentConfig{EnableTaskLoop: true}

	sess, err := d.prepareSingleRoundSession(context.Background(),
		&agentinterfaces.InvokeInputs{ConversationID: "conv-3"}, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if sess != nil {
		t.Fatal("任务循环模式应由调用方持有会话")
	}
}
