package context_engine

import (
	"context"
	"fmt"
	"testing"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// refreshTestProcessorConfig 测试用处理器配置（只承载一个标签）。
type refreshTestProcessorConfig struct {
	// Tag 配置标签，用于区分不同规格
	Tag string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Validate 校验配置，恒通过。
func (c *refreshTestProcessorConfig) Validate() error { return nil }

// SetModelDefaults 空实现（测试配置不持有模型）。
func (c *refreshTestProcessorConfig) SetModelDefaults(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) {
}

// GetModel 返回 nil（测试配置不持有模型）。
func (c *refreshTestProcessorConfig) GetModel() *llmschema.ModelRequestConfig { return nil }

// TestCreateContext_配置未变不重建处理器 同一份配置重复推送时不得重建实例。
func TestCreateContext_配置未变不重建处理器(t *testing.T) {
	registerRefreshProcessor(t, "refresh_proc_stable")

	config := refreshTestConfig("model-a", map[string]int{"model-a": 800})
	engine := NewContextEngine(config)
	concrete := engine.(*contextEngine)
	sess := session.NewSession(session.WithSessionID("sess_stable"))
	specs := []iface.ProcessorSpec{{Type: "refresh_proc_stable", Config: &refreshTestProcessorConfig{Tag: "v1"}}}

	// 生产顺序：rail Init 先推送配置，随后才有会话创建上下文
	concrete.ApplyRuntimeConfig(config, specs)
	mc := createRefreshContext(t, engine, sess, specs)
	firstProcessor := mc.Processors()[0]

	// 同一份配置再次推送（例如无改动地重复保存）不得重建
	concrete.ApplyRuntimeConfig(config, specs)
	again := createRefreshContext(t, engine, sess, specs)

	if again != mc {
		t.Fatal("同一会话应命中上下文池并返回同一实例")
	}
	if got := again.Processors()[0]; got != firstProcessor {
		t.Error("配置未变化时不应重建处理器实例")
	}
}

// TestCreateContext_配置变化后重建并刷新原料 老上下文必须换成新处理器实例，且窗口原料同步刷新。
func TestCreateContext_配置变化后重建并刷新原料(t *testing.T) {
	registerRefreshProcessor(t, "refresh_proc_old")
	registerRefreshProcessor(t, "refresh_proc_new")

	engine := NewContextEngine(refreshTestConfig("model-a", map[string]int{"model-a": 800}))
	concrete := engine.(*contextEngine)
	sess := session.NewSession(session.WithSessionID("sess_refresh"))

	oldSpecs := []iface.ProcessorSpec{{Type: "refresh_proc_old", Config: &refreshTestProcessorConfig{Tag: "old"}}}
	mc := createRefreshContext(t, engine, sess, oldSpecs)
	oldProcessor := mc.Processors()[0]

	if got := mc.ContextMaxTokens(); got != 800 {
		t.Fatalf("初始上下文窗口应为 800，实际 %d", got)
	}

	newConfig := refreshTestConfig("model-b", map[string]int{"model-a": 800, "model-b": 600})
	newSpecs := []iface.ProcessorSpec{{Type: "refresh_proc_new", Config: &refreshTestProcessorConfig{Tag: "new"}}}
	concrete.ApplyRuntimeConfig(newConfig, newSpecs)
	if got := concrete.PendingFingerprint(); got == "" {
		t.Fatal("推送后应记录待落地指纹")
	}

	refreshed := createRefreshContext(t, engine, sess, newSpecs)
	if refreshed != mc {
		t.Fatal("应命中同一上下文实例（消息不重建）")
	}
	if got := refreshed.Processors()[0]; got == oldProcessor {
		t.Error("配置变化后应替换处理器实例")
	}
	if got := refreshed.ContextMaxTokens(); got != 600 {
		t.Errorf("上下文窗口应刷新为 600，实际 %d", got)
	}
	if got := concrete.AppliedFingerprint(); got == "" {
		t.Error("应用成功后应记录配置指纹")
	}
}

// TestCreateContext_处理器创建失败保留旧实例 失败时不替换实例、不推进指纹，下一轮仍会重试。
func TestCreateContext_处理器创建失败保留旧实例(t *testing.T) {
	registerRefreshProcessor(t, "refresh_proc_ok")
	registerBrokenRefreshProcessor(t, "refresh_proc_broken")

	engine := NewContextEngine(refreshTestConfig("model-a", map[string]int{"model-a": 800}))
	concrete := engine.(*contextEngine)
	sess := session.NewSession(session.WithSessionID("sess_fail"))

	okSpecs := []iface.ProcessorSpec{{Type: "refresh_proc_ok", Config: &refreshTestProcessorConfig{Tag: "ok"}}}
	mc := createRefreshContext(t, engine, sess, okSpecs)
	keptProcessor := mc.Processors()[0]
	fingerprintBefore := concrete.AppliedFingerprint()

	brokenSpecs := []iface.ProcessorSpec{{Type: "refresh_proc_broken", Config: &refreshTestProcessorConfig{Tag: "bad"}}}
	concrete.ApplyRuntimeConfig(refreshTestConfig("model-b", map[string]int{"model-b": 600}), brokenSpecs)
	createRefreshContext(t, engine, sess, brokenSpecs)

	if got := mc.Processors()[0]; got != keptProcessor {
		t.Error("处理器创建失败时应保留旧实例")
	}
	if got := concrete.AppliedFingerprint(); got != fingerprintBefore {
		t.Error("应用失败时不应推进指纹（否则后续不会重试）")
	}
	if got := mc.ContextMaxTokens(); got != 800 {
		t.Errorf("应用失败时不应刷新窗口原料，实际 %d", got)
	}
}

// TestCreateContext_刷新覆盖池内全部会话 配置落地应作用于池内所有上下文，而不只是触发请求的那个。
func TestCreateContext_刷新覆盖池内全部会话(t *testing.T) {
	registerRefreshProcessor(t, "refresh_proc_multi")

	engine := NewContextEngine(refreshTestConfig("model-a", map[string]int{"model-a": 800}))
	concrete := engine.(*contextEngine)
	specs := []iface.ProcessorSpec{{Type: "refresh_proc_multi", Config: &refreshTestProcessorConfig{Tag: "v1"}}}

	sess1 := session.NewSession(session.WithSessionID("sess_multi_1"))
	sess2 := session.NewSession(session.WithSessionID("sess_multi_2"))
	mc1 := createRefreshContext(t, engine, sess1, specs)
	mc2 := createRefreshContext(t, engine, sess2, specs)

	// 池内混入一个未实现可选能力的 mock 上下文：刷新时应跳过而不报错
	concrete.contextPool["legacy_default_context_id"] = &mockModelContext{sessionID: "legacy"}

	newConfig := refreshTestConfig("model-b", map[string]int{"model-b": 600})
	concrete.ApplyRuntimeConfig(newConfig, specs)
	createRefreshContext(t, engine, sess1, specs)

	if got := mc1.ContextMaxTokens(); got != 600 {
		t.Errorf("sess_multi_1 窗口应刷新为 600，实际 %d", got)
	}
	if got := mc2.ContextMaxTokens(); got != 600 {
		t.Errorf("sess_multi_2 窗口也应刷新为 600，实际 %d", got)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// refreshTestConfig 构造测试用引擎配置。
func refreshTestConfig(modelName string, windowTokens map[string]int) schema.ContextEngineConfig {
	config := schema.NewContextEngineConfig()
	config.ModelName = modelName
	config.ModelContextWindowTokens = windowTokens
	return config
}

// registerRefreshProcessor 注册一个恒成功的测试处理器类型。
func registerRefreshProcessor(t *testing.T, processorType string) {
	t.Helper()
	RegisterProcessorFactory(processorType, func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		return processor.NewBaseProcessor(config), nil
	})
}

// registerBrokenRefreshProcessor 注册一个恒失败的测试处理器类型。
func registerBrokenRefreshProcessor(t *testing.T, processorType string) {
	t.Helper()
	RegisterProcessorFactory(processorType, func(_ iface.ProcessorConfig) (iface.ContextProcessor, error) {
		return nil, fmt.Errorf("测试用处理器创建失败: %s", processorType)
	})
}

// createRefreshContext 创建（或取回）指定会话的上下文并断言类型。
func createRefreshContext(
	t *testing.T,
	engine iface.ContextEngine,
	sess *session.Session,
	specs []iface.ProcessorSpec,
) *cecontext.SessionModelContext {
	t.Helper()

	mc, err := engine.CreateContext(context.Background(), "default_context_id", sess,
		iface.WithProcessors(specs))
	if err != nil {
		t.Fatalf("CreateContext 失败: %v", err)
	}
	concrete, ok := mc.(*cecontext.SessionModelContext)
	if !ok {
		t.Fatalf("上下文类型应为 *SessionModelContext，实际 %T", mc)
	}
	return concrete
}

// marshalFailConfig 序列化必失败的处理器配置（func 字段无法 JSON 编码）。
type marshalFailConfig struct {
	// Fn 故意保留函数字段，用于覆盖指纹计算的失败分支
	Fn func()
}

// Validate 校验配置，恒通过。
func (c *marshalFailConfig) Validate() error { return nil }

// SetModelDefaults 空实现。
func (c *marshalFailConfig) SetModelDefaults(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) {
}

// GetModel 返回 nil。
func (c *marshalFailConfig) GetModel() *llmschema.ModelRequestConfig { return nil }

// TestRuntimeConfigFingerprint_序列化失败退化为唯一值 覆盖指纹计算失败分支。
func TestRuntimeConfigFingerprint_序列化失败退化为唯一值(t *testing.T) {
	fingerprint := runtimeConfigFingerprint(schema.NewContextEngineConfig(), []iface.ProcessorSpec{
		{Type: "marshal_fail", Config: &marshalFailConfig{Fn: func() {}}},
	})
	if fingerprint == "" {
		t.Fatal("序列化失败时应退化为唯一值，而不是空串（空串会被当成未推送）")
	}
}

// TestShortFingerprint 覆盖截断与不截断两个分支。
func TestShortFingerprint(t *testing.T) {
	if got := shortFingerprint("abc"); got != "abc" {
		t.Errorf("短指纹应原样返回，实际 %q", got)
	}
	if got := shortFingerprint("0123456789"); got != "01234567" {
		t.Errorf("长指纹应截断为前 8 位，实际 %q", got)
	}
}
