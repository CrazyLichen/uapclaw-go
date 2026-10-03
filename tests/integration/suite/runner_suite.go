//go:build integration

package suite

import (
	"fmt"
	"time"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RunnerSuite 包含 Runner 生命周期的集成测试套件。
// 等价于 Python conftest.py 中的 runner fixture（Runner.start() / Runner.stop()）。
//
// 提供：
//   - BaseIntegrationSuite 全部能力
//   - ClientRegistry（已注册 MockLLM）
//   - Runner 全局实例（已启动）
//   - ModelClientConfig / ModelRequestConfig（供创建 Model 使用）
type RunnerSuite struct {
	BaseIntegrationSuite
	// ProviderName MockLLM 在 ClientRegistry 中的 provider 名称
	ProviderName string
	// ClientConfig MockLLM 的客户端配置
	ClientConfig *llmschema.ModelClientConfig
	// ModelConfig MockLLM 的模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化 Runner 测试环境。
// 注册 MockLLM 到 ClientRegistry 并启动 Runner。
// 对齐 Python: async def asyncSetUp() → await Runner.start()
func (s *RunnerSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	// 注册 MockLLM 到全局 ClientRegistry
	// 对齐 Python: mock_llm_context() 中 patch Model.invoke/stream
	providerName := uniqueProviderName("itest_runner")
	model_clients.GetClientRegistry().Register(
		providerName, "llm",
		s.MockLLM.Factory(),
	)
	s.ProviderName = providerName
	s.ClientConfig = &llmschema.ModelClientConfig{
		ClientProvider: providerName,
		ClientID:       providerName + "_id",
		APIKey:         "mock-api-key",
	}
	s.ModelConfig = &llmschema.ModelRequestConfig{
		ModelName: "mock-model",
	}

	// 启动 Runner（对齐 Python: await Runner.start()）
	if err := runner.Start(s.Ctx); err != nil {
		s.T().Fatalf("Runner 启动失败: %v", err)
	}
}

// TearDownSuite 清理 Runner 测试环境。
// 停止 Runner 并注销 MockLLM。
// 对齐 Python: async def asyncTearDown() → await Runner.stop()
func (s *RunnerSuite) TearDownSuite() {
	if err := runner.Stop(s.Ctx); err != nil {
		s.T().Logf("Runner 停止失败（不影响测试结果）: %v", err)
	}

	// 注销 MockLLM
	if s.ProviderName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.ProviderName, "llm")
	}

	s.BaseIntegrationSuite.TearDownSuite()
}

// GetResourceMgr 返回 Runner 的资源管理器，供子 Suite 注册 Agent/SysOperation。
// 对齐 Python: Runner.resource_mgr
func (s *RunnerSuite) GetResourceMgr() *resources_manager.ResourceMgr {
	return runner.GetResourceMgr()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// uniqueProviderName 生成唯一的 provider 名称，避免并发测试冲突。
// 对齐 Python 中 mock_llm_context 的 patch 路径唯一性。
func uniqueProviderName(prefix string) string {
	return fmt.Sprintf("mock_%s_%d", prefix, time.Now().UnixNano())
}
