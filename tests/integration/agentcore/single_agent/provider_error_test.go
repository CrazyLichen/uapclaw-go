//go:build integration

// 本文件覆盖 ReAct 的「模型响应错误」判定链路：provider 错误响应与
// 输出上限命中且无答案。两者都不是传输层错误（Invoke 正常返回消息），
// 必须由 ReAct 主动判定并转成异常，否则本轮会以空答案"成功"结束。
//
// 对齐 Python tests/unit_tests/agent/react_agent/test_react_agent_provider_error.py，
// 但走真实跨子系统链路：agents（ReAct 循环 + 判定）→ foundation/llm 与
// model_clients（客户端注册表，仅在网络边界 mock）→ common/exception（错误载体）
// → session/stream（事件发射）。

package single_agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProviderErrorSuite 模型响应错误判定测试套件。
//
// 对齐 Python: tests/unit_tests/agent/react_agent/test_react_agent_provider_error.py
type ProviderErrorSuite struct {
	isuite.AgentSuite
	// client mock 模型客户端（按用例预置「错误响应」而非「传输错误」）
	client *mockllm.MockModelClient
	// providerName mock provider 注册名（suite 内唯一，避免并发污染）
	providerName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 注册 mock provider。
func (s *ProviderErrorSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()

	s.client = mockllm.NewMockModelClient()
	s.providerName = fmt.Sprintf("provider_err_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.client.Factory())
}

// TearDownSuite 注销 mock provider。
func (s *ProviderErrorSuite) TearDownSuite() {
	_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	s.AgentSuite.TearDownSuite()
}

// TestProviderErrorSuite 运行模型响应错误判定测试套件
func TestProviderErrorSuite(t *testing.T) {
	suite.Run(t, new(ProviderErrorSuite))
}

// TestProviderError_错误详情按字符截断 验证 provider 错误响应转异常后：
// 错误文案带上 provider code/finish_reason，且 provider 文案按「字符」截断到
// 500（不是按字节，否则中文会被劈成乱码），详情落在 BaseError.details。
func (s *ProviderErrorSuite) TestProviderError_错误详情按字符截断() {
	// 600 个字符的 provider 错误文案（超过 500 上限）
	longErrMsg := strings.Repeat("配额超限请稍后重试", 60)
	s.client.SetResponses(mockllm.CreateProviderErrorResponse(12345, longErrMsg))

	agent := s.newAgent()
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "你好"})
	s.Require().Error(err, "provider 错误响应必须转成异常")
	s.Nil(result, "失败时不应返回结果")

	msg := err.Error()
	s.Contains(msg, "provider response error", "错误文案应标识 provider 错误响应")
	s.Contains(msg, "code=12345", "错误文案应带 provider 错误码")
	s.Contains(msg, "finish_reason=error", "错误文案应带 finish_reason")
	s.True(utf8.ValidString(msg), "截断后错误文案必须是合法 UTF-8")

	// 前 500 个字符原样保留；完整的 600 字符不应出现在错误里
	s.Contains(msg, string([]rune(longErrMsg)[:500]), "provider 文案应按字符截断到 500")
	s.NotContains(msg, longErrMsg, "超过 500 字符的部分应被截断")

	// 结构化详情落在 details（Python: raise_error(..., details={...})）
	details := providerErrorDetails(err)
	s.Require().NotNil(details, "错误链上应带结构化详情")
	s.Equal(12345, details["provider_code"])
	s.Equal("error", details["finish_reason"])
	providerMessage, _ := details["provider_message"].(string)
	s.Equal(500, utf8.RuneCountInString(providerMessage), "details 里的 provider_message 也应按字符截断")
}

// TestProviderError_输出上限无答案 验证 finish_reason=length 且无正文/无工具调用
// 时判定为模型调用失败（推理模型把预算花在思维链上的典型空响应）。
func (s *ProviderErrorSuite) TestProviderError_输出上限无答案() {
	s.client.SetResponses(mockllm.CreateOutputLimitResponse(4096))

	agent := s.newAgent()
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "你好"})
	s.Require().Error(err, "输出上限命中且无答案必须转成异常")
	s.Nil(result, "失败时不应返回结果")

	msg := err.Error()
	s.Contains(msg, "model hit its output limit", "错误文案应说明命中了输出上限")
	s.Contains(msg, "output_tokens=4096", "错误文案应带输出 token 计数")

	// 结构化详情落在 details（Python: raise_error(..., details={...})）
	details := providerErrorDetails(err)
	s.Require().NotNil(details, "错误链上应带结构化详情")
	s.Equal("length", details["finish_reason"])
	s.Equal(4096, details["output_tokens"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// providerErrorDetails 沿错误链找到携带结构化详情的 BaseError。
//
// 控制器会把模型调用失败再包一层 BaseError（只带 error_msg 模板参数，details 为 nil），
// 详情保留在内层，故必须遍历 Unwrap 链而不是只看最外层。
func providerErrorDetails(err error) map[string]any {
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		var be *exception.BaseError
		if errors.As(cur, &be) {
			if details, ok := be.Details().(map[string]any); ok && details != nil {
				return details
			}
		}
	}
	return nil
}

// newAgent 创建指向 mock provider 的 DeepAgent（单轮，无工具）。
// anomalyRailDisabled 关闭默认注册的模型异常检测 Rail。
//
// 本套用例验证"provider 错误必须上抛"：若保留默认 rail，瞬态重试会用 mock 的
// 下一条默认响应把错误吞掉（与上游测试显式传 enable_model_anomaly_detection_rail=False 同理）。
func anomalyRailDisabled() *bool {
	disabled := false
	return &disabled
}

func (s *ProviderErrorSuite) newAgent() *harness.DeepAgent {
	model, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.providerName + "_id",
			ClientProvider: s.providerName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "provider-error-model"},
	)
	s.Require().NoError(err, "创建 Model 失败")

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Model:                           model,
		EnableModelAnomalyDetectionRail: anomalyRailDisabled(),
		MaxIterations:                   3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")
	return agent
}
