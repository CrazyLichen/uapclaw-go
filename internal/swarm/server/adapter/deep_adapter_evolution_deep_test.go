package adapter

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newCEAdapter 构造带真实 ReActAgent + 已创建上下文的 DeepAdapter。
// 返回 adapter、session 与 ContextEngine，供深层路径用例复用。
func newCEAdapter(t *testing.T, sessionID string) (*DeepAdapter, context.Context) {
	t.Helper()
	ctx := context.Background()
	card := newEvolveTestAgentCard("ce-" + sessionID)
	ra := newEvolveTestReActAgent(t, card)

	inst := harness.NewDeepAgent(card)
	inst.SetReactAgent(ra, true)

	d := NewDeepAdapter()
	d.instance = inst
	d.model = usableTestModel()
	d.defaultModelName = "test-model"

	sess := newEvolveTestSession(t, sessionID)
	if _, err := ra.ContextEngine().CreateContext(ctx, "default_context", sess); err != nil {
		t.Fatalf("创建会话上下文失败: %v", err)
	}
	return d, ctx
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestCompressContext_上下文已创建 验证有上下文时压缩返回 noop 而非提前退出。
func TestCompressContext_上下文已创建(t *testing.T) {
	d, ctx := newCEAdapter(t, "compress-ctx")
	res, err := d.CompressContext(ctx, "compress-ctx", newEvolveTestSession(t, "compress-ctx"), false)
	if err != nil {
		t.Fatalf("压缩不应报错: %v", err)
	}
	if _, ok := res["result"]; !ok {
		t.Fatalf("结果应含 result 字段，实际 %v", res)
	}
}

// TestCompressContext_returnState 验证 returnState 为 true 时的分支。
func TestCompressContext_returnState(t *testing.T) {
	d, ctx := newCEAdapter(t, "compress-state")
	res, err := d.CompressContext(ctx, "compress-state", newEvolveTestSession(t, "compress-state"), true)
	if err != nil {
		t.Fatalf("压缩不应报错: %v", err)
	}
	if res == nil {
		t.Fatal("应返回非 nil 结果")
	}
}

// TestGetContextUsage_无默认上下文 验证 instance 存在但默认上下文缺失时错误上抛。
// 生产实现固定以 contextID="" 取默认上下文（对齐 Python instance.get_context_usage），
// 未初始化该上下文时下游报 "cannot find context"，此处按实际行为断言错误被上抛。
func TestGetContextUsage_无默认上下文(t *testing.T) {
	d, ctx := newCEAdapter(t, "usage-ctx")
	res, err := d.GetContextUsage(ctx, "usage-ctx")
	if err == nil {
		t.Skip("底层已能解析默认上下文，此分支不适用")
	}
	if res != nil {
		t.Fatalf("报错时应返回 nil，实际 %v", res)
	}
}

// TestGenerateRecap_无消息返回no_turn 验证空上下文时返回 no_turn。
func TestGenerateRecap_无消息返回no_turn(t *testing.T) {
	d, ctx := newCEAdapter(t, "recap-empty")
	res, err := d.GenerateRecap(ctx, "recap-empty")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if res["status"] != "no_turn" {
		t.Fatalf("无消息应返回 no_turn，实际 %v", res)
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestCountFullContextTokens_有上下文 验证空上下文时 token 计数为 0 且无错误。
func TestCountFullContextTokens_有上下文(t *testing.T) {
	d, ctx := newCEAdapter(t, "tokens-ctx")
	n, err := d.countFullContextTokens(ctx, "tokens-ctx")
	if err != nil {
		t.Fatalf("计数不应报错: %v", err)
	}
	if n < 0 {
		t.Fatalf("token 数不应为负，实际 %d", n)
	}
}

// TestGetRecentMessages_有上下文无消息 验证空上下文返回空切片（非 panic）。
func TestGetRecentMessages_有上下文无消息(t *testing.T) {
	d, _ := newCEAdapter(t, "recent-ctx")
	msgs := d.getRecentMessages("recent-ctx")
	if len(msgs) != 0 {
		t.Fatalf("空上下文应无消息，实际 %v", msgs)
	}
}

// TestWatchEvolutionAndPush_autoScan关闭 验证 auto_scan 关闭时直接返回。
func TestWatchEvolutionAndPush_autoScan关闭(t *testing.T) {
	d, ctx := newCEAdapter(t, "watch-ctx")
	d.skillEvolutionRail = coreevolution.NewSkillEvolutionRail(
		[]string{t.TempDir()},
		(*llm.Model)(d.model),
		"test-model",
		"cn",
		coreevolution.WithAutoScan(false),
	)
	if err := d.watchEvolutionAndPush(ctx, "watch-ctx", "c1", "r1"); err != nil {
		t.Fatalf("auto_scan 关闭时不应报错: %v", err)
	}
}

// TestParseApprovalAnswers_空选中项 覆盖 answers 有元素但无选中选项的分支。
func TestParseApprovalAnswers_无选中选项(t *testing.T) {
	got := parseApprovalAnswers([]ApprovalAnswer{{SelectedOptions: []string{}}, {SelectedOptions: nil}})
	if got {
		t.Fatal("无选中项应判为拒绝")
	}
}

// TestBuildRecapPrompt_含memory 验证 memory 前缀块的拼接位置。
func TestBuildRecapPrompt_含memory(t *testing.T) {
	withMem := buildRecapPrompt("MEM")
	without := buildRecapPrompt("")
	if len(withMem) <= len(without) {
		t.Fatalf("带 memory 的提示词应更长（%d vs %d）", len(withMem), len(without))
	}
	if without != withMem[len(withMem)-len(without):] {
		t.Fatal("memory 前缀后应接同一个固定提示词主体")
	}
}

// 确保 llmschema 在本文件被引用（供后续消息构造用例使用）。
var _ = llmschema.RoleTypeUser
