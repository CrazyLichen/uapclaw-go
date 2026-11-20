//go:build integration

// Package single_agent 单 Agent 层集成测试。
// 本文件：ModelBackupRail 模型备份护栏 failover 全链路测试。

package single_agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/schema"
	hconfig "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/modalityprobe"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"
	rail "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelBackupRailSuite 模型备份护栏集成测试套件。
// 对齐 Python: tests/system_tests/rail/test_model_backup_rail.py
// (test_middleware_executes_when_react_agent_invoke)
type ModelBackupRailSuite struct {
	isuite.AgentSuite
	// failClient 主模型 mock 客户端（始终返回错误）
	failClient *mockllm.MockModelClient
	// failProviderName 主模型 provider 注册名
	failProviderName string
	// backupClient 备用模型 mock 客户端
	backupClient *mockllm.MockModelClient
	// backupProviderName 备用模型 provider 注册名
	backupProviderName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 注册主/备两个 mock provider。
// 主模型 = 始终失败的 mock（对齐 Python api_base="mock_url" 真实连接失败，
// Go 用 AddErrorResponse 错误注入，语义等价：都触发 ON_MODEL_EXCEPTION）。
func (s *ModelBackupRailSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()

	s.failClient = mockllm.NewMockModelClient()
	s.failProviderName = fmt.Sprintf("mb_fail_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.failProviderName, "llm", s.failClient.Factory())

	s.backupClient = mockllm.NewMockModelClient()
	s.backupProviderName = fmt.Sprintf("mb_backup_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.backupProviderName, "llm", s.backupClient.Factory())
}

// SetupTest 每个测试前重置 mock 客户端响应队列。
// MockModelClient 队列耗尽后返回默认文本响应（对齐 Python _get_next_response
// 耗尽行为），失败场景会静默失效——测试 1 会消费掉 SetupSuite 注入的错误响应，
// 导致测试 2 的主模型调用"成功"。必须逐测试重置，保证主模型始终失败。
// SetResponses 同时重置调用计数，断言从 0 起算。
func (s *ModelBackupRailSuite) SetupTest() {
	s.failClient.SetResponses(mockllm.CreateErrorResponse(errors.New("模拟主模型服务不可用")))
	s.backupClient.SetResponses(mockllm.CreateTextResponse("备份模型接管成功：计算完成 1+2=3"))
}

// TearDownSuite 注销 mock provider。
func (s *ModelBackupRailSuite) TearDownSuite() {
	_ = model_clients.GetClientRegistry().Unregister(s.failProviderName, "llm")
	_ = model_clients.GetClientRegistry().Unregister(s.backupProviderName, "llm")
	s.AgentSuite.TearDownSuite()
}

// prefillImageProbeCache 预填模态探测缓存（结论 false=不支持）。
// 等价 Python 集成测试的净效果：Python schedule_image_support_probe 是
// asyncio.create_task，同步测试无 running loop → RuntimeError → 探测跳过、
// mock 队列不被消耗；Go 侧探测是 goroutine（23.7 spec 偏差 D1），会真实
// 消费 MockModelClient 队列里的预设响应，把 failover 断成「主模型伪成功」。
// CreateDeepAgent 前预填缓存使 GetCachedImageSupport 命中、跳过调度。
// Reason: failClient 的错误响应被探测请求吃掉 → ON_MODEL_EXCEPTION 不触发。
// Scope: 所有向 CreateDeepAgent 传入自定义 Model 且依赖 mock 队列时序的集成用例。
func prefillImageProbeCache(models ...*llm.Model) {
	for _, m := range models {
		if m == nil || m.ModelConfig == nil {
			continue
		}
		apiBase := ""
		if m.ClientConfig != nil {
			apiBase = m.ClientConfig.APIBase
		}
		modalityprobe.SetCachedImageSupport(
			modalityprobe.ProbeCacheKey{APIBase: apiBase, ModelName: m.ModelConfig.ModelName}, false)
	}
}

// TestModelBackupRailSuite 运行模型备份护栏集成测试套件
func TestModelBackupRailSuite(t *testing.T) {
	suite.Run(t, new(ModelBackupRailSuite))
}

// TestModelBackupRail_Failover主模型故障切换备用 测试主模型调用异常时
// ModelBackupRail 切换到备用模型并重试成功。
// 对齐 Python: test_middleware_executes_when_react_agent_invoke
func (s *ModelBackupRailSuite) TestModelBackupRail_Failover主模型故障切换备用() {
	// 主模型：指向始终失败的 provider（对齐 Python api_base="mock_url"）
	failModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.failProviderName + "_id",
			ClientProvider: s.failProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "fail-model"},
	)
	s.Require().NoError(err, "创建失败主 Model 失败")

	// 备用模型：指向成功 provider（对齐 Python 测试中 backup_models[0]=MockLLMModel）
	backupModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.backupProviderName + "_id",
			ClientProvider: s.backupProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "backup-model"},
	)
	s.Require().NoError(err, "创建备用 Model 失败")

	// 创建 DeepAgent：主模型 failModel + 挂 ModelBackupRail（内含 backupModel）
	// 预填模态探测缓存，避免后台探测消耗 failClient 队列（见 prefillImageProbeCache）
	prefillImageProbeCache(failModel, backupModel)
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Model: failModel,
		Rails: []agentinterfaces.AgentRail{
			rail.NewModelBackupRail([]*llm.Model{backupModel}),
		},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// Invoke：主模型失败 → ON_MODEL_EXCEPTION → 切换备用 → 重试 → 成功
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "计算1+2"})
	s.Require().NoError(err, "failover 后 Invoke 应成功")
	s.Require().NotNil(result)

	// 断言输出来自备用模型
	output, _ := result["output"].(string)
	s.Contains(output, "计算完成 1+2=3", "输出应来自备用模型")

	// 主模型确实被调用过（至少 1 次，验证失败源真实触发）
	s.GreaterOrEqual(s.failClient.InvokeCallCount(), 1, "主模型应至少被调用一次")
	// 备用模型接管后产生响应
	s.GreaterOrEqual(s.backupClient.InvokeCallCount(), 1, "备用模型应接管响应")
}

// TestModelBackupRail_备用耗尽异常上抛 测试备用模型耗尽后异常正常上抛。
// 对齐 Python 语义：index >= len(backup_models) 时不 request_retry。
func (s *ModelBackupRailSuite) TestModelBackupRail_备用耗尽异常上抛() {
	failModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.failProviderName + "_id",
			ClientProvider: s.failProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "fail-model"},
	)
	s.Require().NoError(err, "创建失败主 Model 失败")

	// 空备用链：任何异常都不请求重试
	// 预填模态探测缓存，避免后台探测消耗 failClient 队列（见 prefillImageProbeCache）
	prefillImageProbeCache(failModel)
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Model: failModel,
		Rails: []agentinterfaces.AgentRail{
			rail.NewModelBackupRail(nil),
		},
		EnableModelAnomalyDetectionRail: anomalyRailDisabled(),
		MaxIterations:                   3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "必然失败"})
	s.Require().Error(err, "备用耗尽后异常应正常上抛")
	s.True(strings.Contains(err.Error(), "模拟主模型服务不可用") ||
		strings.Contains(err.Error(), "invoke") ||
		strings.Contains(err.Error(), "模型"),
		"错误信息应包含主模型失败原因")
	s.Nil(result, "失败时 result 应为 nil")
}
