//go:build integration

// 本文件对齐 Python test_model_backup_rail.py 的主用例核心场景：
// failover 后备用模型驱动完整 ReAct 循环（工具调用 → 工具执行 → 二轮文本）。
// 与 model_backup_rail_test.go 的差异：后者只验证单轮文本 failover；
// 本文件补充 Python 主用例的 add 工具链路（tool_call → LocalFunction 真实执行
// → 二轮文本总结），验证模型切换后 ReAct 循环的工具分发/结果回填仍正常。

package single_agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	rail "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelBackupReActLoopSuite 模型备份护栏 failover 后完整 ReAct 循环测试套件。
//
// 对齐 Python: tests/system_tests/rail/test_model_backup_rail.py
// test_middleware_executes_when_react_agent_invoke 的工具链路部分——
// Python 用例中 backup_models[0] 被替换为 MockLLMModel，其响应队列为
// [tool_call(add), text]，验证切换备用模型后：
//  1. 备用模型的 tool_call 被正常解析与分发（add 工具真实执行 1+2=3）
//  2. 工具结果回填后二轮模型调用返回文本总结
//  3. 最终输出包含二轮文本（"根据计算结果，1+2=3"）
type ModelBackupReActLoopSuite struct {
	isuite.AgentSuite
	// failClient 主模型 mock 客户端（始终返回错误，触发 ON_MODEL_EXCEPTION）
	failClient *mockllm.MockModelClient
	// failProviderName 主模型 provider 注册名
	failProviderName string
	// backupClient 备用模型 mock 客户端（驱动 ReAct 工具循环）
	backupClient *mockllm.MockModelClient
	// backupProviderName 备用模型 provider 注册名
	backupProviderName string
	// addToolInvokeCount add 工具被真实执行的次数
	addToolInvokeCount int
	// addToolLastResult add 工具最近一次计算结果（断言参数正确性）
	addToolLastResult int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 注册主/备两个 mock provider。
// 主模型 = 始终失败的 mock（对齐 Python api_base="mock_url" 真实连接失败，
// Go 用 AddErrorResponse 错误注入，语义等价：都触发 ON_MODEL_EXCEPTION）。
func (s *ModelBackupReActLoopSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()

	s.failClient = mockllm.NewMockModelClient()
	s.failProviderName = fmt.Sprintf("mbrl_fail_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.failProviderName, "llm", s.failClient.Factory())

	s.backupClient = mockllm.NewMockModelClient()
	s.backupProviderName = fmt.Sprintf("mbrl_backup_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.backupProviderName, "llm", s.backupClient.Factory())
}

// SetupTest 每个测试前重置 mock 响应队列。
// MockModelClient 队列耗尽后返回默认文本响应（对齐 Python _get_next_response
// 耗尽行为），失败场景会静默失效——必须逐测试重置，保证主模型始终失败。
// SetResponses 同时重置调用计数，断言从 0 起算。
func (s *ModelBackupReActLoopSuite) SetupTest() {
	s.addToolInvokeCount = 0
	s.addToolLastResult = 0
	s.failClient.SetResponses(mockllm.CreateErrorResponse(errors.New("模拟主模型服务不可用")))
}

// TearDownSuite 注销 mock provider。
func (s *ModelBackupReActLoopSuite) TearDownSuite() {
	_ = model_clients.GetClientRegistry().Unregister(s.failProviderName, "llm")
	_ = model_clients.GetClientRegistry().Unregister(s.backupProviderName, "llm")
	s.AgentSuite.TearDownSuite()
}

// TestModelBackupReActLoopSuite 运行 failover 后完整 ReAct 循环测试套件
func TestModelBackupReActLoopSuite(t *testing.T) {
	suite.Run(t, new(ModelBackupReActLoopSuite))
}

// TestModelBackupReActLoop_Failover后工具循环 测试主模型故障切换后，
// 备用模型驱动完整 ReAct 循环：tool_call(add) → 工具真实执行 → 二轮文本总结。
// 对齐 Python: test_middleware_executes_when_react_agent_invoke
// （backup 响应队列 [create_tool_call_response("add", '{"a": 1, "b": 2}'),
// create_text_response("根据计算结果，1+2=3")] 的完整链路）
func (s *ModelBackupReActLoopSuite) TestModelBackupReActLoop_Failover后工具循环() {
	ctx := s.Ctx

	// 备用模型响应队列（对齐 Python mock_llm.set_responses）：
	// 第一轮 tool_call 调 add(1,2)；第二轮文本总结
	s.backupClient.SetResponses(
		mockllm.CreateToolCallResponse("add", `{"a": 1, "b": 2}`),
		mockllm.CreateTextResponse("根据计算结果，1+2=3"),
	)

	// 主模型：始终失败（对齐 Python api_base="mock_url"）
	failModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.failProviderName + "_id",
			ClientProvider: s.failProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "fail-model"},
	)
	s.Require().NoError(err, "创建失败主 Model 失败")

	// 备用模型：指向成功 provider（对齐 Python backup_models[0]=MockLLMModel）
	backupModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.backupProviderName + "_id",
			ClientProvider: s.backupProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "backup-model"},
	)
	s.Require().NoError(err, "创建备用 Model 失败")

	// add 工具（对齐 Python _create_add_tool LocalFunction(a+b)）：
	// 用计数器验证工具被真实执行且参数正确
	addTool := s.newAddTool()

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Model:         failModel,
		ToolInstances: []tool.Tool{addTool},
		Rails: []agentinterfaces.AgentRail{
			rail.NewModelBackupRail([]*llm.Model{backupModel}),
		},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// Invoke：主模型失败 → ON_MODEL_EXCEPTION → 切换备用 → 重试
	// → 备用模型 tool_call → add 工具执行 → 二轮文本 → 完成
	result, err := agent.Invoke(ctx, map[string]any{"query": "计算1+2"})
	s.Require().NoError(err, "failover 后完整循环 Invoke 应成功")
	s.Require().NotNil(result)

	// 断言最终输出来自备用模型的二轮文本（对齐 Python assertIn）
	output, _ := result["output"].(string)
	s.Contains(output, "根据计算结果", "输出应为备用模型二轮文本总结")

	// add 工具被真实执行过且参数正确（1+2=3）
	s.Equal(1, s.addToolInvokeCount, "add 工具应被真实执行恰好一次")
	s.Equal(3, s.addToolLastResult, "add 工具计算结果应为 3（a=1, b=2）")

	// 主模型确实被调用过（验证失败源真实触发）
	s.GreaterOrEqual(s.failClient.InvokeCallCount(), 1, "主模型应至少被调用一次")
	// 备用模型至少两轮（tool_call 轮 + 文本轮）
	s.GreaterOrEqual(s.backupClient.InvokeCallCount(), 2, "备用模型应驱动至少两轮循环")
}

// TestModelBackupReActLoop_备用模型二轮故障不再切换 测试备用模型驱动
// 循环途中再次故障且备用链已耗尽时，异常正常上抛（ReAct 循环终止）。
// Python 无对应显式用例，但这是 on_model_exception 语义的自然延伸：
// index 已越界后不再 request_retry，RailExecutor 异常上抛终止循环。
func (s *ModelBackupReActLoopSuite) TestModelBackupReActLoop_备用模型二轮故障不再切换() {
	ctx := s.Ctx

	// 备用模型第一轮成功返回 tool_call，第二轮失败（模拟备用模型中途故障）
	s.backupClient.SetResponses(
		mockllm.CreateToolCallResponse("add", `{"a": 1, "b": 2}`),
		mockllm.CreateErrorResponse(errors.New("备用模型中途故障")),
	)

	failModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.failProviderName + "_id",
			ClientProvider: s.failProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "fail-model"},
	)
	s.Require().NoError(err, "创建失败主 Model 失败")

	backupModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.backupProviderName + "_id",
			ClientProvider: s.backupProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "backup-model"},
	)
	s.Require().NoError(err, "创建备用 Model 失败")

	addTool := s.newAddTool()

	// 备用链只含 1 个模型：第一次故障切换后 index=1 耗尽
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Model:         failModel,
		ToolInstances: []tool.Tool{addTool},
		Rails: []agentinterfaces.AgentRail{
			rail.NewModelBackupRail([]*llm.Model{backupModel}),
		},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// 第一轮：主模型失败 → 切换备用 → 备用 tool_call → add 执行
	// 二轮：备用模型故障 → 备用已耗尽 → 异常上抛
	result, err := agent.Invoke(ctx, map[string]any{"query": "计算1+2"})
	s.Require().Error(err, "备用耗尽后中途故障应正常上抛")
	s.Nil(result, "失败时 result 应为 nil")

	// add 工具在故障前已被真实执行（第一轮 tool_call 完成）
	s.Equal(1, s.addToolInvokeCount, "add 工具应在备用故障前执行过一次")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAddTool 创建 add 加法工具（对齐 Python _create_add_tool）。
// 工具 schema 含 a/b 两个 number 必填参数（对齐 Python input_params），
// 执行真实加法并记录调用计数与结果供断言。
//
// 注意：number 参数必须用 NewNumberParam 构造器而非结构体字面量——
// Param.Minimum/Maximum 以 NaN 表示"未设置"，结构体字面量的零值 0 是
// 合法约束（maximum=0 会导致任何正数参数被 schema 校验拒绝）。
func (s *ModelBackupReActLoopSuite) newAddTool() tool.Tool {
	card := tool.NewToolCardWithID(
		"add", "add", "加法运算",
		[]*schema.Param{
			schema.NewNumberParam("a", "第一个加数", true),
			schema.NewNumberParam("b", "第二个加数", true),
		},
		nil,
	)
	t, _ := tool.NewMapFunction(card,
		func(_ context.Context, inputs map[string]any) (map[string]any, error) {
			a, _ := toFloat(inputs["a"])
			b, _ := toFloat(inputs["b"])
			s.addToolInvokeCount++
			s.addToolLastResult = int(a + b)
			return map[string]any{"result": a + b}, nil
		}, nil,
	)
	return t
}

// toFloat 兼容 JSON 数字解析（float64/int/json.Number）转 float64。
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		var f float64
		_, err := fmt.Sscanf(n, "%g", &f)
		return f, err == nil
	default:
		return 0, false
	}
}
