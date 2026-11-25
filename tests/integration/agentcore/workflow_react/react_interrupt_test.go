//go:build integration

// 8.26 ReAct→workflow 层 2 中断桥 MockLLM 集成（spec §5 用例 1/2；plan Task 6）。
// 真实跨子系统链：workflow（外层图编排 + 子 workflow 实例）× single_agent/agents
// （ReAct 循环 + 层 2 中断桥）× ability（workflow 工具分发 + D-26-5 对象旁路）
// × session/interaction（交互帧 / InteractiveInput 恢复）× stream（过滤单发）。
//
// 断言纪律：只钉确定性结构契约（中断态形状 / INTERACTION 帧存在 / 单发块数 /
// pending 轮转序 / 对象形态 / 恢复后终态），不断言模型具体输出（MockLLM 固定
// 响应队列，全部确定性）。
package workflow_react

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	saconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/config"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	llmcomp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/llm/react"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ReactInterruptSuite 8.26 层 2 中断桥 MockLLM 端到端。
type ReactInterruptSuite struct {
	suite.Suite
}

func TestReactInterruptSuite(t *testing.T) {
	suite.Run(t, new(ReactInterruptSuite))
}

// newQuestionerComp 构造提问组件（mock 队列：首轮 {} 抽取缺失 → 追问挂起；
// 恢复轮抽齐 → COMPLETED）。
func (s *ReactInterruptSuite) newQuestionerComp(responses ...string) *llmcomp.QuestionerComponent {
	mocks := make([]mockllm.MockResponse, 0, len(responses))
	for _, r := range responses {
		mocks = append(mocks, mockllm.CreateTextResponse(r))
	}
	return llmcomp.NewQuestionerComponent(&llmcomp.QuestionerConfig{
		ExtractFieldsFromResponse: boolPtrR(true),
		FieldNames: []llmcomp.QuestionerFieldInfo{
			{FieldName: "name", Description: "姓名", Type: "string", Required: true},
		},
	}, llmcomp.WithQuestionerLLM(mockllm.NewMockModelClient(mocks...)))
}

func boolPtrR(b bool) *bool { return &b }

// buildQuestionerWorkflow 装配 start → questioner → end 子 workflow（包级助手，
// MockLLM 与真链两套件共用）。
func buildQuestionerWorkflow(t *testing.T, comp *llmcomp.QuestionerComponent) *workflow.Workflow {
	inputsSchema := map[string]any{"query": "${query}"}
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("ask", comp, nil, inputsSchema, nil, nil, nil, nil, 0, -1.0, nil, nil)
	endCfg, err := flow.NewEndConfig("collected:{{name}}")
	require.NoError(t, err)
	end, err := flow.NewEnd(endCfg)
	require.NoError(t, err)
	w.SetEndComp("end", end, map[string]any{"name": "${ask.name}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "ask")
	w.AddConnection([]string{"ask"}, "end")
	return w
}

// workflowIfaceAdapter 把 *workflow.Workflow 适配为 sainterfaces.Workflow
// （生产链路 workflow-as-tool 的注册面；ability.executeWorkflow →
// runner.RunWorkflow → WithWorkflowSession/WithWorkflowContext 选项下发）。
type workflowIfaceAdapter struct {
	w    *workflow.Workflow
	card *workflow.WorkflowCard
}

func (a *workflowIfaceAdapter) Invoke(ctx context.Context, inputs any, opts ...sainterfaces.WorkflowOption) (any, error) {
	o := sainterfaces.NewWorkflowOptions(opts...)
	// *session.WorkflowSession 不实现 SessionFacade；用其会话 ID 构建真实
	// *session.Session 承接（恢复链快照按 session ID 键对齐——
	// 中断轮与恢复轮共用同一 agent 会话 ID）
	var sess sessioninterfaces.SessionFacade
	if o.Session != nil {
		sess = session.NewSession(session.WithSessionID(o.Session.SessionID()))
	}
	return a.w.Invoke(ctx, inputs, sess, o.Context, nil)
}

func (a *workflowIfaceAdapter) Stream(ctx context.Context, inputs any, opts ...sainterfaces.WorkflowOption) (<-chan stream.Schema, error) {
	o := sainterfaces.NewWorkflowOptions(opts...)
	var sess sessioninterfaces.SessionFacade
	if o.Session != nil {
		sess = session.NewSession(session.WithSessionID(o.Session.SessionID()))
	}
	raw, err := a.w.Stream(ctx, inputs, sess, o.Context, nil, nil)
	if err != nil {
		return nil, err
	}
	// Workflow.Stream 原始返回 <-chan any（帧混流），桥接为 <-chan stream.Schema
	out := make(chan stream.Schema)
	go func() {
		defer close(out)
		for frame := range raw {
			if sc, ok := frame.(stream.Schema); ok {
				out <- sc
			}
		}
	}()
	return out, nil
}

func (a *workflowIfaceAdapter) Card() *workflow.WorkflowCard { return a.card }

// registerSubWorkflow 将子 workflow 注册进全局 ResourceMgr（react 内嵌 agent 的
// abilityManager 经 runner.GetResourceMgr() 查找实例）。
func registerSubWorkflow(t *testing.T, name string, w *workflow.Workflow) *workflow.WorkflowCard {
	card := workflow.NewWorkflowCard(commonschema.WithName(name), commonschema.WithDescription("子工作流 "+name))
	card.InputParams = map[string]any{
		"type":       "object",
		"properties": map[string]any{"query": map[string]any{"type": "string", "description": "待补齐的用户信息抽取任务"}},
		"required":   []string{"query"},
	}
	adapter := &workflowIfaceAdapter{w: w, card: card}
	require.NoError(t, runner.GetResourceMgr().AddWorkflow(card,
		func(_ context.Context, _ *workflow.WorkflowCard) (sainterfaces.Workflow, error) {
			return adapter, nil
		}))
	return card
}

// buildOuterWorkflow 装配 start → react → end 外层 workflow。
func buildOuterWorkflow(t *testing.T, comp *react.ReActAgentComp) *workflow.Workflow {
	inputsSchema := map[string]any{"query": "${query}"}
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("react", comp, nil, inputsSchema, nil, nil, nil, nil, 0, -1.0, nil, nil)
	endCfg, err := flow.NewEndConfig("done:{{output}}")
	require.NoError(t, err)
	end, err := flow.NewEnd(endCfg)
	require.NoError(t, err)
	w.SetEndComp("end", end, map[string]any{"output": "${react.output}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "react")
	w.AddConnection([]string{"react"}, "end")
	return w
}

// interactionFrames 提取 WorkflowOutput.Result 中的交互帧文本（8.24 套件同款）。
func interactionFrames(t *testing.T, wo *workflowbase.WorkflowOutput) []string {
	out := make([]string, 0)
	chunks, ok := wo.Result.([]stream.Schema)
	require.True(t, ok, "Result 应为 Schema 列表，got %T", wo.Result)
	for _, c := range chunks {
		switch t := c.(type) {
		case stream.OutputSchema:
			if t.Type == interaction.InteractionType {
				if io, isIO := t.Payload.(interaction.InteractionOutput); isIO {
					out = append(out, fmt.Sprintf("%s:%v", io.ID, io.Value))
				}
			}
		case *stream.OutputSchema:
			if t.Type == interaction.InteractionType {
				if io, isIO := t.Payload.(interaction.InteractionOutput); isIO {
					out = append(out, fmt.Sprintf("%s:%v", io.ID, io.Value))
				}
			}
		}
	}
	return out
}

// TestReactInterrupt_双workflow中断_轮转恢复 全链用例 1（spec §5）：
// 外层 ReAct 节点（mock 双工具调用）→ 两个子 workflow 同时 INPUT_REQUIRED →
// 外层 INPUT_REQUIRED 且交互帧单发（pendingID 过滤）→ 反馈 wf_a → 轮转 wf_b
// 再中断 → 反馈 wf_b → 全收齐并发恢复（D-26-5 对象旁路）→ ReAct 续跑终答 →
// 外层 COMPLETED。
func (s *ReactInterruptSuite) TestReactInterrupt_双workflow中断_轮转恢复() {
	ctx := context.Background()
	sess := session.NewSession(session.WithEnvs(map[string]any{}))

	// 外层 ReAct 的 mock 模型（ClientRegistry 注入，8.24 网关套件先例）：
	// 轮1 双工具调用 [wf_a, wf_b]；轮2（恢复后）文本终答
	mockReact := mockllm.NewMockModelClient(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "wf_a", ArgsJSON: `{}`, CallID: "call_a"},
			{Name: "wf_b", ArgsJSON: `{}`, CallID: "call_b"},
		}),
		mockllm.CreateTextResponse("final done"),
	)
	providerName := "mock_react_itest"
	model_clients.GetClientRegistry().Register(providerName, "llm", mockReact.Factory())
	// 经 SetLLM 注入完整 Model（getLLM 兜底路径不填 client_id，181002）
	mockModel, mErr := llm.NewModel(
		&schema.ModelClientConfig{
			ClientID:       providerName,
			ClientProvider: providerName,
			APIBase:        "http://mock.local",
			APIKey:         "mock-key",
		},
		func() *schema.ModelRequestConfig {
			mc := schema.NewModelRequestConfig()
			mc.ModelName = "mock-react"
			return mc
		}(),
	)
	s.Require().NoError(mErr)

	// 子 workflow：各自带 mock 提问组件（轮1 {} 抽取缺失 → 挂起；恢复轮抽齐）
	wfA := buildQuestionerWorkflow(s.T(), s.newQuestionerComp(`{}`, `{"name": "张三"}`))
	wfB := buildQuestionerWorkflow(s.T(), s.newQuestionerComp(`{}`, `{"name": "李四"}`))
	cardA := registerSubWorkflow(s.T(), "wf_a", wfA)
	cardB := registerSubWorkflow(s.T(), "wf_b", wfB)

	// 外层 workflow 装配 + 向 react 内嵌 agent 注册 workflow 能力
	reactCfg := &react.ReActAgentCompConfig{ReActAgentConfig: saconfig.NewReActAgentConfig(
		saconfig.WithModelProvider(providerName),
		saconfig.WithModelName("mock-react"),
	)}
	reactComp := react.NewReActAgentComp(reactCfg)
	w := buildOuterWorkflow(s.T(), reactComp)
	exec, ok := reactComp.ToExecutableFn().(*react.ReActExecutable)
	s.Require().True(ok, "executable 应为 *ReActExecutable")
	exec.ReactAgent().SetLLM(mockModel)
	exec.AbilityManager().Add(cardA)
	exec.AbilityManager().Add(cardB)

	// ---- 第一调：双 workflow 同时 INPUT_REQUIRED，交互帧单发（pending=wf_a）----
	out, err := w.Invoke(ctx, map[string]any{"query": "请补齐两位用户信息"}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.T().Logf("首调 result=%v mockCalls=%d", wo.Result, mockReact.InvokeCallCount())
	s.Require().Equal(workflowbase.WorkflowExecutionStateInputRequired, wo.State, "双中断应使外层挂起")
	frames := interactionFrames(s.T(), wo)
	s.Require().Len(frames, 1, "过滤单发：调用方只看到恰好一个交互块（react_agent.py L3406 语义），got %v", frames)
	s.Contains(frames[0], "react", "交互帧应携带挂起节点（react）的 executable ID——恢复按节点路由")

	// ---- 第二调：反馈 wf_a → pending 轮转到 wf_b，仍挂起 ----
	ii1, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(ii1.Update("react", "张三"))
	out2, err := w.Invoke(ctx, ii1, sess, nil, nil)
	s.Require().NoError(err)
	wo2, ok := out2.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.T().Logf("二调 result=%v", wo2.Result)
	s.Require().Equal(workflowbase.WorkflowExecutionStateInputRequired, wo2.State, "仅收齐一个反馈应轮转后再挂起")
	frames2 := interactionFrames(s.T(), wo2)
	s.Require().Len(frames2, 1, "轮转后仍应单发")
	s.Contains(frames2[0], "react")

	// ---- 第三调：反馈 wf_b → 全收齐并发恢复（D-26-5 对象旁路）→ ReAct 续跑终答 ----
	ii2, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(ii2.Update("react", "李四"))
	out3, err := w.Invoke(ctx, ii2, sess, nil, nil)
	s.Require().NoError(err)
	wo3, ok := out3.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.Require().Equal(workflowbase.WorkflowExecutionStateCompleted, wo3.State,
		"全收齐并发恢复后 ReAct 续跑至终态，got state=%v result=%v", wo3.State, wo3.Result)
	s.Contains(fmt.Sprintf("%v", wo3.Result), "final done", "ReAct 恢复轮应产出 mock 终答")
}

// newModelRequestConfig 构造模型请求配置（真链/单测共用助手）。
func newModelRequestConfig(modelName string) *schema.ModelRequestConfig {
	mc := schema.NewModelRequestConfig()
	mc.ModelName = modelName
	return mc
}
