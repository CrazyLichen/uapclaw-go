package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── NewReActAgentConfig 测试 ────────────────────────────

// TestNewReActAgentConfig 验证默认构造值。
func TestNewReActAgentConfig(t *testing.T) {
	cfg := NewReActAgentConfig()

	assert.Equal(t, "openai", cfg.ModelProvider, "ModelProvider 默认应为 openai")
	assert.Equal(t, 5, cfg.MaxIterations, "MaxIterations 默认应为 5")
	assert.Equal(t, 1, cfg.LLMTopLogprobs, "LLMTopLogprobs 默认应为 1")
	assert.Equal(t, 200, cfg.ContextEngineConfig.MaxContextMessageNum, "ContextEngineConfig.MaxContextMessageNum 默认应为 200")
	assert.Equal(t, 10, cfg.ContextEngineConfig.DefaultWindowRoundNum, "ContextEngineConfig.DefaultWindowRoundNum 默认应为 10")

	// 其他字段应为零值
	assert.Empty(t, cfg.MemScopeIDVal)
	assert.Empty(t, cfg.ModelNameVal)
	assert.Empty(t, cfg.APIKey)
	assert.Empty(t, cfg.APIBase)
	assert.Nil(t, cfg.CustomHeaders)
	assert.Empty(t, cfg.PromptTemplateName)
	assert.Nil(t, cfg.PromptTemplate)
	assert.False(t, cfg.LLMReturnTokenIDs)
	assert.False(t, cfg.LLMLogprobs)
	assert.Nil(t, cfg.ModelClientConfig)
	assert.Nil(t, cfg.ModelRequestConfig)
	assert.Empty(t, cfg.SysOperationID)
	assert.Nil(t, cfg.ContextProcessors)
	assert.Nil(t, cfg.Workspace)
}

// TestNewReActAgentConfig_WithOptions 验证各基础 Option 函数逐一生效。
func TestNewReActAgentConfig_WithOptions(t *testing.T) {
	t.Run("WithMemScopeID", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithMemScopeID("scope1"))
		assert.Equal(t, "scope1", cfg.MemScopeIDVal)
	})

	t.Run("WithModelName", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelName("gpt-4"))
		assert.Equal(t, "gpt-4", cfg.ModelNameVal)
	})

	t.Run("WithModelProvider", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelProvider("dashscope"))
		assert.Equal(t, "dashscope", cfg.ModelProvider)
	})

	t.Run("WithAPIKey", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithAPIKey("key123"))
		assert.Equal(t, "key123", cfg.APIKey)
	})

	t.Run("WithAPIBase", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithAPIBase("https://api.example.com"))
		assert.Equal(t, "https://api.example.com", cfg.APIBase)
	})

	t.Run("WithCustomHeaders", func(t *testing.T) {
		headers := map[string]string{"x-custom": "val"}
		cfg := NewReActAgentConfig(WithCustomHeaders(headers))
		assert.Equal(t, headers, cfg.CustomHeaders)
	})

	t.Run("WithPromptTemplateName", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithPromptTemplateName("default"))
		assert.Equal(t, "default", cfg.PromptTemplateName)
	})

	t.Run("WithPromptTemplate", func(t *testing.T) {
		tmpl := []map[string]any{{"role": "system", "content": "you are helpful"}}
		cfg := NewReActAgentConfig(WithPromptTemplate(tmpl))
		assert.Equal(t, tmpl, cfg.PromptTemplate)
	})

	t.Run("WithMaxIterations", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithMaxIterations(20))
		assert.Equal(t, 20, cfg.MaxIterations)
	})

	t.Run("WithLLMReturnTokenIDs", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithLLMReturnTokenIDs(true))
		assert.True(t, cfg.LLMReturnTokenIDs)
	})

	t.Run("WithLLMLogprobs", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithLLMLogprobs(true))
		assert.True(t, cfg.LLMLogprobs)
	})

	t.Run("WithLLMTopLogprobs", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithLLMTopLogprobs(5))
		assert.Equal(t, 5, cfg.LLMTopLogprobs)
	})

	t.Run("WithSysOperationID", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithSysOperationID("op1"))
		assert.Equal(t, "op1", cfg.SysOperationID)
	})

	t.Run("WithWorkspace", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithWorkspace("test_workspace"))
		assert.Equal(t, "test_workspace", cfg.Workspace)
	})

	t.Run("WithContextEngineConfig", func(t *testing.T) {
		ceCfg := ceschema.ContextEngineConfig{
			MaxContextMessageNum:     300,
			DefaultWindowRoundNum:    15,
			ModelContextWindowTokens: make(map[string]int),
		}
		cfg := NewReActAgentConfig(WithContextEngineConfig(ceCfg))
		assert.Equal(t, 300, cfg.ContextEngineConfig.MaxContextMessageNum)
		assert.Equal(t, 15, cfg.ContextEngineConfig.DefaultWindowRoundNum)
	})

	t.Run("WithContextProcessors", func(t *testing.T) {
		procs := []ceiface.ProcessorSpec{
			{Type: "DialogueCompressor"},
		}
		cfg := NewReActAgentConfig(WithContextProcessors(procs))
		assert.Equal(t, procs, cfg.ContextProcessors)
	})
}

// TestNewReActAgentConfig_WithModelClient 验证复合 Option 联动。
func TestNewReActAgentConfig_WithModelClient(t *testing.T) {
	cfg := NewReActAgentConfig(WithModelClient(
		"dashscope", "sk-test", "https://dashscope.api", "qwen-max",
	))

	// 验证顶层字段被设置
	assert.Equal(t, "dashscope", cfg.ModelProvider)
	assert.Equal(t, "sk-test", cfg.APIKey)
	assert.Equal(t, "https://dashscope.api", cfg.APIBase)
	assert.Equal(t, "qwen-max", cfg.ModelNameVal)

	// 验证 ModelClientConfig 被创建
	assert.NotNil(t, cfg.ModelClientConfig)
	assert.Equal(t, "dashscope", cfg.ModelClientConfig.ClientProvider)
	assert.Equal(t, "sk-test", cfg.ModelClientConfig.APIKey)
	assert.Equal(t, "https://dashscope.api", cfg.ModelClientConfig.APIBase)
	assert.False(t, cfg.ModelClientConfig.VerifySSL, "VerifySSL 默认应为 false（对齐 Python verify_ssl=False）")

	// 验证 ModelRequestConfig 被创建
	assert.NotNil(t, cfg.ModelRequestConfig)
	assert.Equal(t, "qwen-max", cfg.ModelRequestConfig.ModelName)
}

// TestNewReActAgentConfig_WithModelClient_扩展参数 验证 WithVerifySSL 和 WithExtraCustomHeaders 扩展参数。
func TestNewReActAgentConfig_WithModelClient_扩展参数(t *testing.T) {
	headers := map[string]string{"x-trace": "123"}
	cfg := NewReActAgentConfig(WithModelClient(
		"openai", "sk-xxx", "https://api.openai.com", "gpt-4",
		WithVerifySSL(false),
		WithExtraCustomHeaders(headers),
	))

	assert.NotNil(t, cfg.ModelClientConfig)
	assert.False(t, cfg.ModelClientConfig.VerifySSL, "WithVerifySSL(false) 应生效")
	assert.Equal(t, headers, cfg.ModelClientConfig.CustomHeaders, "WithExtraCustomHeaders 应设置到 ModelClientConfig.CustomHeaders")
}

// TestNewReActAgentConfig_WithModelClient_CustomHeaders回退 验证 WithModelClient 自动回退到 c.CustomHeaders。
func TestNewReActAgentConfig_WithModelClient_CustomHeaders回退(t *testing.T) {
	// 先 WithCustomHeaders 再 WithModelClient，ModelClientConfig.CustomHeaders 应从 c.CustomHeaders 回退
	headers := map[string]string{"X-Custom": "val"}
	cfg := NewReActAgentConfig(
		WithCustomHeaders(headers),
		WithModelClient("openai", "sk-xxx", "https://api.openai.com", "gpt-4"),
	)
	assert.NotNil(t, cfg.ModelClientConfig)
	assert.Equal(t, headers, cfg.ModelClientConfig.CustomHeaders, "WithModelClient 应回退到 c.CustomHeaders")

	// WithExtraCustomHeaders 优先级高于 c.CustomHeaders
	extraHeaders := map[string]string{"X-Extra": "extra"}
	cfg2 := NewReActAgentConfig(
		WithCustomHeaders(headers),
		WithModelClient("openai", "sk-xxx", "https://api.openai.com", "gpt-4",
			WithExtraCustomHeaders(extraHeaders),
		),
	)
	assert.Equal(t, extraHeaders, cfg2.ModelClientConfig.CustomHeaders, "WithExtraCustomHeaders 应优先于 c.CustomHeaders")
}

// TestNewReActAgentConfig_WithModelProviderDetails 验证复合 Option 仅设置顶层字段。
func TestNewReActAgentConfig_WithModelProviderDetails(t *testing.T) {
	cfg := NewReActAgentConfig(WithModelProviderDetails("dashscope", "sk-test", "https://dashscope.api"))

	// 验证顶层字段被设置
	assert.Equal(t, "dashscope", cfg.ModelProvider)
	assert.Equal(t, "sk-test", cfg.APIKey)
	assert.Equal(t, "https://dashscope.api", cfg.APIBase)

	// 验证 ModelClientConfig 未被创建（与 WithModelClient 区分）
	assert.Nil(t, cfg.ModelClientConfig)
}

// TestNewReActAgentConfig_WithContextEngine 验证复合 Option 构建上下文引擎配置。
func TestNewReActAgentConfig_WithContextEngine(t *testing.T) {
	cfg := NewReActAgentConfig(WithContextEngine(500, 20, true, true))

	assert.Equal(t, 500, cfg.ContextEngineConfig.MaxContextMessageNum)
	assert.Equal(t, 20, cfg.ContextEngineConfig.DefaultWindowRoundNum)
	assert.True(t, cfg.ContextEngineConfig.EnableReload)
	assert.True(t, cfg.ContextEngineConfig.EnableKVCacheRelease)
}

// TestNewReActAgentConfig_WithCustomHeadersSync 验证复合 Option 同步 CustomHeaders 到 ModelClientConfig。
func TestNewReActAgentConfig_WithCustomHeadersSync(t *testing.T) {
	headers := map[string]string{"x-req-id": "abc", "x-trace": "xyz"}

	// 先用 WithModelClient 创建 ModelClientConfig，再用 WithCustomHeadersSync 同步
	cfg := NewReActAgentConfig(
		WithModelClient("openai", "sk-xxx", "https://api.openai.com", "gpt-4"),
		WithCustomHeadersSync(headers),
	)

	// 验证顶层 CustomHeaders 被设置
	assert.Equal(t, headers, cfg.CustomHeaders)

	// 验证 ModelClientConfig.CustomHeaders 被同步
	assert.NotNil(t, cfg.ModelClientConfig)
	assert.Equal(t, headers, cfg.ModelClientConfig.CustomHeaders)
}

// TestNewReActAgentConfig_WithCustomHeadersSync_无ModelClientConfig 验证 ModelClientConfig 为 nil 时不 panic。
func TestNewReActAgentConfig_WithCustomHeadersSync_无ModelClientConfig(t *testing.T) {
	headers := map[string]string{"x-custom": "val"}
	cfg := NewReActAgentConfig(WithCustomHeadersSync(headers))

	assert.Equal(t, headers, cfg.CustomHeaders)
	assert.Nil(t, cfg.ModelClientConfig, "ModelClientConfig 应为 nil，不 panic")
}

// ──────────────────────────── Validate 测试 ────────────────────────────

// TestReActAgentConfig_Validate 验证校验逻辑。
func TestReActAgentConfig_Validate(t *testing.T) {
	t.Run("正常情况返回nil", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelName("test-model"))
		assert.Nil(t, cfg.Validate())
	})

	t.Run("ModelName为空返回错误", func(t *testing.T) {
		cfg := NewReActAgentConfig()
		assert.Error(t, cfg.Validate())
	})

	t.Run("LLMTopLogprobs为0通过", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelName("test-model"), WithLLMTopLogprobs(0))
		assert.Nil(t, cfg.Validate())
	})

	t.Run("LLMTopLogprobs为20通过", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelName("test-model"), WithLLMTopLogprobs(20))
		assert.Nil(t, cfg.Validate())
	})

	t.Run("LLMTopLogprobs为-1返回错误", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithLLMTopLogprobs(-1))
		assert.Error(t, cfg.Validate())
	})

	t.Run("LLMTopLogprobs为21返回错误", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithLLMTopLogprobs(21))
		assert.Error(t, cfg.Validate())
	})

	t.Run("MaxIterations为0返回错误", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithMaxIterations(0))
		assert.Error(t, cfg.Validate())
	})

	t.Run("MaxIterations为1通过", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelName("test-model"), WithMaxIterations(1))
		assert.Nil(t, cfg.Validate())
	})

	t.Run("MaxIterations为-1返回错误", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithMaxIterations(-1))
		assert.Error(t, cfg.Validate())
	})

	t.Run("ModelClientConfig非nil时递归校验_空APIKey返回错误", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelName("test-model"))
		// 构造一个 APIKey 为空的 ModelClientConfig
		cfg.ModelClientConfig = llmschema.NewModelClientConfig("openai", "", "https://api.openai.com")
		assert.Error(t, cfg.Validate())
	})

	t.Run("ModelClientConfig非nil时递归校验_有效配置通过", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelClient("openai", "sk-xxx", "https://api.openai.com", "gpt-4"))
		assert.Nil(t, cfg.Validate())
	})

	t.Run("ContextEngineConfig递归校验_负值返回错误", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelName("test-model"))
		cfg.ContextEngineConfig.MaxContextMessageNum = -1
		assert.Error(t, cfg.Validate())
	})

	t.Run("ContextEngineConfig递归校验_有效配置通过", func(t *testing.T) {
		cfg := NewReActAgentConfig(WithModelName("test-model"), WithContextEngine(100, 5, false, false))
		assert.Nil(t, cfg.Validate())
	})
}

// ──────────────────────────── AgentConfig 接口测试 ────────────────────────────

// TestReActAgentConfig_AgentConfig接口 验证接口实现和接口方法返回值。
func TestReActAgentConfig_AgentConfig接口(t *testing.T) {
	// 编译期断言：*ReActAgentConfig 实现 interfaces.AgentConfig
	var _ interfaces.AgentConfig = (*ReActAgentConfig)(nil)

	// 创建实例验证接口方法
	cfg := NewReActAgentConfig(
		WithModelName("qwen-max"),
		WithMemScopeID("scope-42"),
		WithModelClient("openai", "sk-xxx", "https://api.openai.com", "qwen-max"),
	)

	// ModelName() 接口方法
	assert.Equal(t, "qwen-max", cfg.ModelName(), "ModelName() 应返回 ModelName 字段")

	// MemScopeID() 接口方法
	assert.Equal(t, "scope-42", cfg.MemScopeID(), "MemScopeID() 应返回 MemScopeID 字段")

	// GetContextEngineConfig() 接口方法
	ceCfg := cfg.GetContextEngineConfig()
	assert.Equal(t, 200, ceCfg.MaxContextMessageNum, "GetContextEngineConfig() 应返回 ContextEngineConfig")

	// GetModelClientConfig() 接口方法
	mcCfg := cfg.GetModelClientConfig()
	assert.NotNil(t, mcCfg, "GetModelClientConfig() 应返回 ModelClientConfig 指针")
	assert.Equal(t, "openai", mcCfg.ClientProvider)
}

// ──────────────────────────── JSON 序列化测试 ────────────────────────────

// TestReActAgentConfig_JSON序列化 验证 JSON round-trip。
func TestReActAgentConfig_JSON序列化(t *testing.T) {
	original := NewReActAgentConfig(
		WithMemScopeID("scope1"),
		WithModelName("gpt-4"),
		WithModelProvider("openai"),
		WithAPIKey("sk-xxx"),
		WithAPIBase("https://api.openai.com"),
		WithMaxIterations(10),
		WithLLMReturnTokenIDs(true),
		WithLLMLogprobs(true),
		WithLLMTopLogprobs(5),
		WithSysOperationID("op1"),
		WithCustomHeaders(map[string]string{"x-custom": "val"}),
	)

	data, err := json.Marshal(original)
	assert.NoError(t, err, "序列化不应报错")

	var restored ReActAgentConfig
	err = json.Unmarshal(data, &restored)
	assert.NoError(t, err, "反序列化不应报错")

	// 比较关键字段
	assert.Equal(t, original.MemScopeIDVal, restored.MemScopeIDVal, "MemScopeIDVal round-trip 一致")
	assert.Equal(t, original.ModelNameVal, restored.ModelNameVal, "ModelNameVal round-trip 一致")
	assert.Equal(t, original.ModelProvider, restored.ModelProvider, "ModelProvider round-trip 一致")
	assert.Equal(t, original.APIKey, restored.APIKey, "APIKey round-trip 一致")
	assert.Equal(t, original.APIBase, restored.APIBase, "APIBase round-trip 一致")
	assert.Equal(t, original.MaxIterations, restored.MaxIterations, "MaxIterations round-trip 一致")
	assert.Equal(t, original.LLMReturnTokenIDs, restored.LLMReturnTokenIDs, "LLMReturnTokenIDs round-trip 一致")
	assert.Equal(t, original.LLMLogprobs, restored.LLMLogprobs, "LLMLogprobs round-trip 一致")
	assert.Equal(t, original.LLMTopLogprobs, restored.LLMTopLogprobs, "LLMTopLogprobs round-trip 一致")
	assert.Equal(t, original.SysOperationID, restored.SysOperationID, "SysOperationID round-trip 一致")
	assert.Equal(t, original.CustomHeaders, restored.CustomHeaders, "CustomHeaders round-trip 一致")
	assert.Equal(t, original.ContextEngineConfig.MaxContextMessageNum, restored.ContextEngineConfig.MaxContextMessageNum, "ContextEngineConfig round-trip 一致")
}
