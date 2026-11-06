package agents

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
)

// newWindowCfg 构造带模型元数据的 agent 配置（model_config_obj.context_window）。
func newWindowCfg(window int, engineModelName string, engineOverride int) *config.ReActAgentConfig {
	cfg := config.NewReActAgentConfig(
		config.WithModelName("qwen3-max"),
		config.WithMaxIterations(1),
	)
	cfg.ModelRequestConfig = llmschema.NewModelRequestConfig(
		llmschema.WithModelName("qwen3-max"),
		llmschema.WithContextWindow(window),
	)
	cfg.ContextEngineConfig.ModelName = engineModelName
	cfg.ContextEngineConfig.ModelContextWindowTokensOverride = engineOverride
	return cfg
}

// TestConfigure_从模型配置写入窗口override 验证 configure 装配模型名与窗口元数据。
func TestConfigure_从模型配置写入窗口override(t *testing.T) {
	cfg := newWindowCfg(262144, "", 0)
	agent := newTestAgent("window_cfg")
	if err := agent.Configure(context.Background(), cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if got := cfg.ContextEngineConfig.ModelContextWindowTokensOverride; got != 262144 {
		t.Fatalf("override = %d, want 262144", got)
	}
	if got := cfg.ContextEngineConfig.ModelName; got != "qwen3-max" {
		t.Fatalf("model_name 应从模型配置补齐，got %q", got)
	}
}

// TestConfigure_显式模型名不被覆盖 验证已配置的引擎模型名优先。
func TestConfigure_显式模型名不被覆盖(t *testing.T) {
	cfg := newWindowCfg(131072, "explicit-model", 0)
	agent := newTestAgent("window_explicit")
	if err := agent.Configure(context.Background(), cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if got := cfg.ContextEngineConfig.ModelName; got != "explicit-model" {
		t.Fatalf("显式模型名不应被覆盖，got %q", got)
	}
}

// TestConfigure_无窗口元数据保留显式override 验证缺元数据时不清空 override。
func TestConfigure_无窗口元数据保留显式override(t *testing.T) {
	cfg := config.NewReActAgentConfig(
		config.WithModelName("qwen3-max"),
		config.WithMaxIterations(1),
	)
	cfg.ContextEngineConfig.ModelContextWindowTokensOverride = 4096
	agent := newTestAgent("window_keep")
	if err := agent.Configure(context.Background(), cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if got := cfg.ContextEngineConfig.ModelContextWindowTokensOverride; got != 4096 {
		t.Fatalf("无模型元数据时应保留显式 override，got %d", got)
	}
}

// TestUpdateModelContext_透传引擎 验证热切换同时更新 agent 配置与引擎。
func TestUpdateModelContext_透传引擎(t *testing.T) {
	engine := &cancelAwareContextEngine{}
	agent := newTestAgent("window_update")
	agent.contextEngine = engine
	agent.config.ContextEngineConfig.ModelContextWindowTokensOverride = 0

	agent.UpdateModelContext("glm-4-long", 1000000)
	if got := agent.config.ContextEngineConfig.ModelName; got != "glm-4-long" {
		t.Fatalf("agent 配置 model_name = %q", got)
	}
	if got := agent.config.ContextEngineConfig.ModelContextWindowTokensOverride; got != 1000000 {
		t.Fatalf("agent 配置 override = %d", got)
	}

	agent.UpdateModelContext("glm-4-long", 0)
	if got := agent.config.ContextEngineConfig.ModelContextWindowTokensOverride; got != 0 {
		t.Fatalf("<=0 应清除 override，got %d", got)
	}
	if len(engine.modelUpdates) != 2 {
		t.Fatalf("应两次透传引擎，got %d", len(engine.modelUpdates))
	}
	if engine.modelUpdates[0][0] != "glm-4-long" || engine.modelUpdates[0][1] != 1000000 {
		t.Fatalf("首次透传参数错误: %v", engine.modelUpdates[0])
	}
}
