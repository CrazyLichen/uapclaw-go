//go:build integration

package workflow_questioner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	llmcomp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/llm"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// QuestionerSuite 8.24 交互提问组件中断恢复端到端（spec §4.10-11）。
type QuestionerSuite struct {
	suite.Suite
}

func TestQuestionerSuite(t *testing.T) {
	suite.Run(t, new(QuestionerSuite))
}

// newQuestionerComp 构造提问组件（mock LLM 响应队列：首轮 {} 未抽取 → 追问；
// 恢复轮抽齐）
func (s *QuestionerSuite) newQuestionerComp(responses ...string) (*llmcomp.QuestionerComponent, *mockllm.MockModelClient) {
	mocks := make([]mockllm.MockResponse, 0, len(responses))
	for _, r := range responses {
		mocks = append(mocks, mockllm.CreateTextResponse(r))
	}
	mock := mockllm.NewMockModelClient(mocks...)
	comp := llmcomp.NewQuestionerComponent(&llmcomp.QuestionerConfig{
		ExtractFieldsFromResponse: boolPtrQ(true),
		FieldNames: []llmcomp.QuestionerFieldInfo{
			{FieldName: "name", Description: "姓名", Type: "string", Required: true},
		},
	}, llmcomp.WithQuestionerLLM(mock))
	return comp, mock
}

func boolPtrQ(b bool) *bool { return &b }

// buildWorkflow 装配 start → questioner → end（引用抽取字段）
func (s *QuestionerSuite) buildWorkflow(comp *llmcomp.QuestionerComponent) *workflow.Workflow {
	inputsSchema := map[string]any{"query": "${query}"}
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("ask", comp, nil, inputsSchema, nil, nil, nil, nil, 0, -1.0, nil, nil)
	endCfg, err := flow.NewEndConfig("collected:{{name}}")
	s.Require().NoError(err)
	end, err := flow.NewEnd(endCfg)
	s.Require().NoError(err)
	// End 模板变量经节点 InputsSchema 绑定 questioner 输出（end_render 套件同款）
	w.SetEndComp("end", end, map[string]any{"name": "${ask.name}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "ask")
	w.AddConnection([]string{"ask"}, "end")
	return w
}

// resultText 提取 WorkflowOutput 的可读文本（8.23 套件同款）
func (s *QuestionerSuite) resultText(wo *workflowbase.WorkflowOutput) string {
	return fmt.Sprintf("%v", wo.Result)
}

// TestQuestioner_中断恢复端到端 首调 INPUT_REQUIRED（INTERACTION 帧）→
// InteractiveInput 二次调用 → 恢复执行到下游 end（spec §4.10）。
// 对齐 Python test_runner_agent_resource_management 欠的 Questioner
// __interaction__ → 二次调用恢复场景（6.25 ⤵️ 回填兑现）。
func (s *QuestionerSuite) TestQuestioner_中断恢复端到端() {
	comp, _ := s.newQuestionerComp(`{}`, `{"name": "张三"}`)
	w := s.buildWorkflow(comp)

	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	ctx := context.Background()

	// ---- 首调：必填缺失 → 追问挂起 INPUT_REQUIRED ----
	out, err := w.Invoke(ctx, map[string]any{"query": "你好"}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.Require().Equal(workflowbase.WorkflowExecutionStateInputRequired, wo.State)
	// INTERACTION 帧在 Result chunks 中（Python _stream 帧 一帧两用语义）
	chunks, ok := wo.Result.([]stream.Schema)
	s.Require().True(ok, "Result 应为 Schema 列表，got %T", wo.Result)
	var interactionFrameText string
	for _, c := range chunks {
		if sc, isSchema := c.(stream.OutputSchema); isSchema && sc.Type == interaction.InteractionType {
			if io, isIO := sc.Payload.(interaction.InteractionOutput); isIO {
				interactionFrameText = fmt.Sprintf("%v", io.Value)
			}
		}
	}
	s.Require().NotEmpty(interactionFrameText, "Result 应含 INTERACTION 帧: %v", chunks)
	s.Contains(interactionFrameText, "请您提供姓名相关的信息")

	// ---- 二次调用：InteractiveInput 按 questioner 节点 id 寻址恢复 ----
	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(ii.Update("ask", "我叫张三"))
	out2, err := w.Invoke(ctx, ii, sess, nil, nil)
	s.Require().NoError(err)
	wo2, ok := out2.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.Equal(workflowbase.WorkflowExecutionStateCompleted, wo2.State, "恢复后应完成")
	// 下游 end 节点真实执行：模板渲染出抽取字段
	s.Contains(s.resultText(wo2), "collected:张三")
}

// TestQuestioner_直发问答不抽取 question_content 模式：START 渲染发问挂起 →
// 用户回答 → END（Python :425-433/:465-468 纯问答语义）
func (s *QuestionerSuite) TestQuestioner_直发问答不抽取() {
	// Python invoke :811 无条件 initialize_if_needed——question_content 纯问答
	// 模式也需模型配置（否则 101075，与 Python 一致）
	comp := llmcomp.NewQuestionerComponent(&llmcomp.QuestionerConfig{
		QuestionContent:           "请提供您的姓名",
		ExtractFieldsFromResponse: boolPtrQ(false),
	}, llmcomp.WithQuestionerLLM(mockllm.NewMockModelClient()))
	inputsSchema := map[string]any{"query": "${query}"}
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("ask", comp, nil, inputsSchema, nil, nil, nil, nil, 0, -1.0, nil, nil)
	endCfg, err := flow.NewEndConfig("answered:{{user_response}}")
	s.Require().NoError(err)
	end, err := flow.NewEnd(endCfg)
	s.Require().NoError(err)
	w.SetEndComp("end", end, map[string]any{"user_response": "${ask.user_response}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "ask")
	w.AddConnection([]string{"ask"}, "end")

	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	ctx := context.Background()
	out, err := w.Invoke(ctx, map[string]any{"query": ""}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.Require().Equal(workflowbase.WorkflowExecutionStateInputRequired, wo.State)
	s.Contains(s.resultText(wo), "请提供您的姓名")

	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(ii.Update("ask", "李四"))
	out2, err := w.Invoke(ctx, ii, sess, nil, nil)
	s.Require().NoError(err)
	wo2, ok := out2.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.Equal(workflowbase.WorkflowExecutionStateCompleted, wo2.State)
	s.Contains(s.resultText(wo2), "answered:李四")
}

// TestQuestioner_超限报错 max_response 轮后仍缺必填 → 101074（Python :693-697）
func (s *QuestionerSuite) TestQuestioner_超限报错() {
	// 三轮均未抽取（max_response 默认 3：第 4 次检查超限）
	comp, _ := s.newQuestionerComp(`{}`, `{}`, `{}`, `{}`)
	w := s.buildWorkflow(comp)

	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	ctx := context.Background()

	out, err := w.Invoke(ctx, map[string]any{"query": "q1"}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflowbase.WorkflowOutput)
	s.Require().True(ok)
	s.Require().Equal(workflowbase.WorkflowExecutionStateInputRequired, wo.State)

	// response_num 计已收反馈数：3 次恢复后 response_num=3 ≥ max_response=3，
	// 第 3 次恢复抽取仍缺即抛 101074（Python :681-697 语义链）
	for i := 0; i < 2; i++ {
		ii, err := interaction.NewInteractiveInput()
		s.Require().NoError(err)
		s.Require().NoError(ii.Update("ask", fmt.Sprintf("无关回答%d", i)))
		out, err = w.Invoke(ctx, ii, sess, nil, nil)
		s.Require().NoError(err)
		wo, ok = out.(*workflowbase.WorkflowOutput)
		s.Require().True(ok)
	}
	// 第三次恢复：response_num 已达 3，仍缺必填 → 101074
	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(ii.Update("ask", "还是无关"))
	_, err = w.Invoke(ctx, ii, sess, nil, nil)
	s.Require().Error(err, "超限应报 COMPONENT_QUESTIONER_RUNTIME_ERROR 101074")
}
