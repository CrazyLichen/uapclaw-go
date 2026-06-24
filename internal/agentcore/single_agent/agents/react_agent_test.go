//go:build test

package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewReActAgent 验证 ReActAgent 构造函数
func TestNewReActAgent(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("test_react"),
		cschema.WithDescription("测试 ReActAgent"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
	)

	agent := NewReActAgent(card, config)
	assert.NotNil(t, agent)
	assert.NotNil(t, agent.base)
	assert.Equal(t, config, agent.config)
	assert.NotNil(t, agent.promptBuilder)
}

// TestNewSystemPromptBuilder 验证系统提示词构建器构造
func TestNewSystemPromptBuilder(t *testing.T) {
	builder := NewSystemPromptBuilder()
	assert.NotNil(t, builder)
	assert.Equal(t, "cn", builder.Language)
	assert.NotNil(t, builder.sections)
}

// TestNewPromptSection 验证提示节构造
func TestNewPromptSection(t *testing.T) {
	section := NewPromptSection("test", map[string]string{"cn": "测试"}, 10)
	assert.Equal(t, "test", section.Name)
	assert.Equal(t, 10, section.Priority)
	assert.Equal(t, "测试", section.Content["cn"])
}

// TestPromptSection_Render 验证提示节渲染
func TestPromptSection_Render(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文", "en": "English"},
		Priority: 10,
	}

	// 匹配指定语言
	assert.Equal(t, "中文", section.Render("cn"))
	assert.Equal(t, "English", section.Render("en"))

	// 回退到默认语言
	assert.Equal(t, "中文", section.Render("fr"))

	// 空内容
	emptySection := PromptSection{Name: "empty", Content: map[string]string{}}
	assert.Equal(t, "", emptySection.Render("cn"))
}

// TestSystemPromptBuilder_AddSection 验证添加节
func TestSystemPromptBuilder_AddSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 20})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 10})

	assert.True(t, builder.HasSection("a"))
	assert.True(t, builder.HasSection("b"))
}

// TestSystemPromptBuilder_RemoveSection 验证移除节
func TestSystemPromptBuilder_RemoveSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	assert.True(t, builder.HasSection("a"))

	builder.RemoveSection("a")
	assert.False(t, builder.HasSection("a"))
}

// TestSystemPromptBuilder_Build 验证构建结果按优先级排序
func TestSystemPromptBuilder_Build(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "low", Content: map[string]string{"cn": "LOW"}, Priority: 30})
	builder.AddSection(PromptSection{Name: "mid", Content: map[string]string{"cn": "MID"}, Priority: 20})
	builder.AddSection(PromptSection{Name: "high", Content: map[string]string{"cn": "HIGH"}, Priority: 10})

	result := builder.Build()
	assert.Equal(t, "HIGH\n\nMID\n\nLOW", result)
}

// TestReActAgent_InvokeImpl_空输入 验证无 query 时不 panic
func TestReActAgent_InvokeImpl_空输入(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("test_react"),
		cschema.WithDescription("测试 ReActAgent"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	// 空 inputs，无 context engine，无法真正执行 LLM 调用
	// 但 InvokeImpl 应正常处理空输入场景
	inputs := map[string]any{}
	_, _ = agent.InvokeImpl(context.Background(), inputs)
	// 不 assert 结果，因为 LLM 未初始化会返回错误
	// 只要不 panic 就算通过
}

// TestReActAgent_AgentID 验证 AgentID 返回正确值
func TestReActAgent_AgentID(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("id_test"),
		cschema.WithDescription("ID 测试"),
	)
	agent := NewReActAgent(card, nil)
	assert.Equal(t, card.ID, agent.AgentID())
}

// TestReActAgent_CallbackManager 验证 CallbackManager 不为 nil
func TestReActAgent_CallbackManager(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("cb_test"),
		cschema.WithDescription("回调测试"),
	)
	agent := NewReActAgent(card, nil)
	assert.NotNil(t, agent.CallbackManager())
}

// TestReActAgent_Configure 验证 Configure 正常配置
func TestReActAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("cfg_test"),
		cschema.WithDescription("配置测试"),
	)
	agent := NewReActAgent(card, nil)

	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(5),
	)
	err := agent.Configure(context.Background(), config)
	assert.NoError(t, err)
	assert.Equal(t, config, agent.config)
}

// TestReActAgent_Configure_nil配置 验证 nil 配置返回错误
func TestReActAgent_Configure_nil配置(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("nil_cfg"),
		cschema.WithDescription("nil 配置测试"),
	)
	agent := NewReActAgent(card, nil)
	err := agent.Configure(context.Background(), nil)
	assert.Error(t, err)
}

// TestReActAgent_AddPromptBuilderSection 验证添加提示节
func TestReActAgent_AddPromptBuilderSection(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("prompt_test"),
		cschema.WithDescription("提示词测试"),
	)
	agent := NewReActAgent(card, nil)
	agent.AddPromptBuilderSection("identity", map[string]string{"cn": "我是测试Agent"}, 10)
	assert.True(t, agent.promptBuilder.HasSection("identity"))
}

// TestReActAgent_StreamImpl 验证 StreamImpl 返回 channel
func TestReActAgent_StreamImpl(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("stream_test"),
		cschema.WithDescription("流式测试"),
	)
	config := saconfig.NewReActAgentConfig(
		saconfig.WithModelName("qwen-max"),
		saconfig.WithMaxIterations(1),
	)
	agent := NewReActAgent(card, config)

	inputs := map[string]any{"query": "test"}
	ch, _ := agent.StreamImpl(context.Background(), inputs)
	// channel 应该可用（即使内部执行可能出错）
	assert.NotNil(t, ch)
	// 消费 channel 避免泄漏
	for range ch {
	}
}

// TestReActAgent_getLLM_无配置 验证无配置时返回错误
func TestReActAgent_getLLM_无配置(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("llm_test"),
		cschema.WithDescription("LLM 测试"),
	)
	agent := NewReActAgent(card, nil)
	_, err := agent.getLLM()
	assert.Error(t, err)
}

// TestReActAgent_initContext_无引擎 验证无 context engine 时返回 nil
func TestReActAgent_initContext_无引擎(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("ctx_test"),
		cschema.WithDescription("上下文测试"),
	)
	agent := NewReActAgent(card, nil)
	mc, err := agent.initContext(context.Background(), nil)
	assert.NoError(t, err)
	assert.Nil(t, mc)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
