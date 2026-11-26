//go:build integration && llm

// 8.26 ReAct→workflow 中断恢复真链冒烟（复用 27.4 内置智谱免费模型域；用户拍板
// 2026-11-24：集成测试用真实 LLM，真实 e2e 覆盖 react 到 workflow 的中断和恢复）。
//
// 双守卫（真实外部服务）：
//  1. integration && llm 标签
//  2. ZHIPU_BUILTIN_API_KEY 未注入（且内置常量为空）→ Skip
//
// 运行方式：
//
//	go test -tags "sqlite_fts5 test integration llm" \
//	  ./tests/integration/agentcore/workflow_react/ -run TestReactRealLLMSuite -count=1 -v
//
// 断言纪律（8.24 真链先例同款）：只钉确定性结构契约（INPUT_REQUIRED→恢复→终态
// 状态机、INTERACTION 帧存在、单发块数），不断言模型具体输出值——与模型行为无关。
//
// 链路：外层 workflow（批式）→ ReActComponent 节点（真 ReActAgent + 智谱模型）
// → 模型产出工具调用 → 子 workflow 工具（QuestionerComp 真模型抽取）→ 真实
// INPUT_REQUIRED 中断 → 单发交互块 → 反馈 → 并发恢复 → ReAct 续跑终态。
package workflow_react

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	saconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/config"
	llmcomp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/llm/react"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/configpanel"
)

// ReactRealLLMSuite 8.26 真链冒烟：内置智谱免费模型走完整
// ReAct→子 workflow 抽取→中断挂起→反馈恢复→续跑链路。
type ReactRealLLMSuite struct {
	suite.Suite
}

func TestReactRealLLMSuite(t *testing.T) {
	suite.Run(t, new(ReactRealLLMSuite))
}

// zhipuClientConfig 取内置免费模型客户端配置（双守卫：空 → Skip）。
func (s *ReactRealLLMSuite) zhipuClientConfig(clientID string) *schema.ModelClientConfig {
	entries := configpanel.ZhipuFreeModelEntries()
	if len(entries) == 0 {
		s.T().Skip("ZHIPU_BUILTIN_API_KEY 未注入且内置常量为空，跳过真链冒烟")
	}
	mcc := entries[0]["model_client_config"].(map[string]any)
	return &schema.ModelClientConfig{
		ClientID:       clientID,
		ClientProvider: "OpenAI", // 智谱 OpenAI 兼容端点
		APIKey:         mcc["api_key"].(string),
		APIBase:        mcc["api_base"].(string),
	}
}

// zhipuModelName 取免费模型名。
func (s *ReactRealLLMSuite) zhipuModelName() string {
	entries := configpanel.ZhipuFreeModelEntries()
	if len(entries) == 0 {
		s.T().Skip("ZHIPU_BUILTIN_API_KEY 未注入且内置常量为空，跳过真链冒烟")
	}
	mcc := entries[0]["model_client_config"].(map[string]any)
	return mcc["model_name"].(string)
}

// realQuestionerComp 构造真链提问组件（不注入 fake；直接构造路径：
// ModelClientConfig+ModelConfig → foundation/llm.NewModel，8.24 真链同款）
func (s *ReactRealLLMSuite) realQuestionerComp() *llmcomp.QuestionerComponent {
	modelName := s.zhipuModelName()
	cfg := &llmcomp.QuestionerConfig{
		ExtractFieldsFromResponse: boolPtrR(true),
		FieldNames: []llmcomp.QuestionerFieldInfo{
			{FieldName: "name", Description: "姓名", Type: "string", Required: true},
		},
		ModelClientConfig: s.zhipuClientConfig("zhipu-free-react-realchain"),
		ModelConfig:       newModelRequestConfig(modelName),
	}
	return llmcomp.NewQuestionerComponent(cfg)
}

// TestReactRealLLM_中断恢复 主链真链用例：ReAct 节点内嵌真模型 + 子 workflow
// QuestionerComp 真模型抽取 → 必填缺失追问挂起 → 反馈恢复 → ReAct 续跑终态。
// 模型行为两可分支按结构契约接受（8.24 先例）：
//   - 首调即补全（模型直接抽齐/幻觉直通）→ COMPLETED 亦为合法终局；
//   - 恢复轮仍缺必填 → 再次 INPUT_REQUIRED 亦合法（未超限）。
func (s *ReactRealLLMSuite) TestReactRealLLM_中断恢复() {
	ctx := context.Background()
	sess := session.NewSession(session.WithEnvs(map[string]any{}))

	// react 模型：直接构造 + SetLLM 注入（MockLLM 套件同款接缝）
	reactModel, err := llm.NewModel(
		s.zhipuClientConfig("zhipu-free-react-realchain"),
		newModelRequestConfig(s.zhipuModelName()),
	)
	s.Require().NoError(err)

	// 子 workflow：真链 QuestionerComp
	wfA := buildQuestionerWorkflow(s.T(), s.realQuestionerComp())
	cardA := registerSubWorkflow(s.T(), "wf_a", wfA)

	// 外层 workflow 装配 + react 内嵌 agent 注册能力 + 模型注入
	reactCfg := &react.ReActAgentCompConfig{ReActAgentConfig: saconfig.NewReActAgentConfig(
		saconfig.WithModelName(s.zhipuModelName()),
	)}
	reactComp := react.NewReActAgentComp(reactCfg)
	w := buildOuterWorkflow(s.T(), reactComp)
	exec, ok := reactComp.ToExecutableFn().(*react.ReActExecutable)
	s.Require().True(ok, "executable 应为 *ReActExecutable")
	exec.ReactAgent().SetLLM(reactModel)
	exec.AbilityManager().Add(cardA)

	// ---- 首调：模型产出工具调用 → 子 workflow 抽取 → 追问挂起 ----
	out, err := w.Invoke(ctx, map[string]any{"query": "请通过工具补齐用户姓名"}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.T().Logf("首调 state=%v result=%v", wo.State, fmt.Sprintf("%v", wo.Result))

	if wo.State == workflowbase.WorkflowExecutionStateCompleted {
		// 模型直通（未走工具/直接补全）——合法终局；但 ReAct 执行错误伪装成
		// COMPLETED 必须显式失败（invokeEntry 吞错转 error output）
		s.Require().NotContains(fmt.Sprintf("%v", wo.Result), "Error in ReAct execution",
			"ReAct 执行错误不应伪装成 COMPLETED 终局")
		s.T().Logf("模型直通终局（合法）：%v", wo.Result)
		return
	}
	s.Require().Equal(workflowbase.WorkflowExecutionStateInputRequired, wo.State)
	frames := interactionFrames(s.T(), wo)
	s.Require().Len(frames, 1, "过滤单发：调用方只看到恰好一个交互块，got %v", frames)
	s.Contains(frames[0], "react", "交互帧应携带挂起节点 executable ID")

	// ---- 二调：真实回答 → 恢复 → ReAct 续跑 ----
	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(ii.Update("react", "张三"))
	out2, err := w.Invoke(ctx, ii, sess, nil, nil)
	s.Require().NoError(err)
	wo2, ok := out2.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.T().Logf("二调 state=%v result=%v", wo2.State, fmt.Sprintf("%v", wo2.Result))
	// 终态两可：恢复成功 → COMPLETED；子 workflow 模型仍缺必填（未超限）→ 再次追问挂起
	s.Contains([]workflowbase.WorkflowExecutionState{
		workflowbase.WorkflowExecutionStateCompleted,
		workflowbase.WorkflowExecutionStateInputRequired,
	}, wo2.State, "got state=%v result=%v", wo2.State, wo2.Result)
}
