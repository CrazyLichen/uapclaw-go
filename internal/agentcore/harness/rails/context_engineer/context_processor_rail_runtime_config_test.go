package context_engineer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// pendingFingerprintProvider 读取上下文引擎待落地的配置指纹（测试用）。
type pendingFingerprintProvider interface {
	// PendingFingerprint 返回已推送待落地的配置指纹
	PendingFingerprint() string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextProcessorRail_Init推送运行时配置 Init 应把配置与处理器规格推给上下文引擎，
// 供下一次 CreateContext 应用到已存在的会话（老会话热重载跟随）。
func TestContextProcessorRail_Init推送运行时配置(t *testing.T) {
	agent := harness.NewDeepAgent(agentschema.NewAgentCard())
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), &hschema.DeepAgentConfig{
		Workspace: hworkspace.NewWorkspace(t.TempDir(), "cn"),
	}))

	engine := agent.ReactAgent().ContextEngine()
	require.NotNil(t, engine, "DeepAgent 内层 ReActAgent 应持有上下文引擎")
	provider, ok := engine.(pendingFingerprintProvider)
	require.True(t, ok, "上下文引擎应实现配置指纹查询")

	rail := NewContextProcessorRail()
	require.NoError(t, rail.Init(context.Background(), agent))

	require.NotEmpty(t, provider.PendingFingerprint(),
		"Init 末尾应把运行时配置推给上下文引擎")
}

// TestContextProcessorRail_Init无引擎时跳过推送 agent 未暴露上下文引擎时不得 panic。
func TestContextProcessorRail_Init无引擎时跳过推送(t *testing.T) {
	reactCfg := saconfig.NewReActAgentConfig()
	reactCfg.ModelNameVal = "mock-model"

	agent := &railCfgAgent{
		railAgent: &railAgent{builder: saprompt.NewSystemPromptBuilder()},
		cfg:       reactCfg,
	}

	rail := NewContextProcessorRail()
	require.NoError(t, rail.Init(context.Background(), agent))
	require.NotEmpty(t, reactCfg.ContextProcessors, "处理器仍应写入 react config")
}

// TestPushRuntimeConfig_空agent不panic 覆盖 resolveContextEngine 的 nil 分支。
func TestPushRuntimeConfig_空agent不panic(t *testing.T) {
	require.NotPanics(t, func() {
		pushRuntimeConfig(nil, saconfig.NewReActAgentConfig().ContextEngineConfig, nil)
	})
}
