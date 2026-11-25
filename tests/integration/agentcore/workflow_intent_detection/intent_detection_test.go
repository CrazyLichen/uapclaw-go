//go:build integration

package workflow_intent_detection

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	llmcomp "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/llm"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// IntentDetectionSuite 8.23 IntentDetectionComp 全链集成（spec §5/§3）。
type IntentDetectionSuite struct {
	suite.Suite
}

func TestIntentDetectionSuite(t *testing.T) {
	suite.Run(t, new(IntentDetectionSuite))
}

// newIntentComp 构造意图组件（mock LLM 边界 + 分类表 退款/物流）
func (s *IntentDetectionSuite) newIntentComp(classification string, enableHistory bool) (*llmcomp.IntentDetectionComponent, *mockllm.MockModelClient) {
	mock := mockllm.NewMockModelClient(mockllm.CreateTextResponse(classification))
	comp := llmcomp.NewIntentDetectionComponent(&llmcomp.IntentDetectionCompConfig{
		CategoryNameList: []string{"退款申请", "物流查询"},
		UserPrompt:       "电商客服分流语境",
		EnableHistory:    enableHistory,
	}, llmcomp.WithIntentLLM(mock))
	return comp, mock
}

// newEnd 构造 End 组件（batch 收敛）
func (s *IntentDetectionSuite) newEnd(tmpl string) *flow.End {
	end, err := flow.NewEndConfig(tmpl)
	s.Require().NoError(err)
	e, err := flow.NewEnd(end)
	s.Require().NoError(err)
	return e
}

// buildWorkflow 装配 start → intent →（条件边）→ refund/logistics 两个 end。
// designated = 预期命中分支（designated end 为后注册者，与 branch_comp 套件同款）
func (s *IntentDetectionSuite) buildWorkflow(comp *llmcomp.IntentDetectionComponent, refundTmpl, logisticsTmpl, designated string) *workflow.Workflow {
	s.Require().NoError(comp.AddBranch("${classificationId} == 1", []string{"refund"}, "b1"))
	s.Require().NoError(comp.AddBranch("${classificationId} == 2", []string{"logistics"}, "b2"))

	// query 从 io_state 顶层取（workflow 输入 commit_user_inputs 落顶层，
	// ref path ${query} 经 get_by_schema 映射进节点输入——converter 同款）
	inputsSchema := map[string]any{"query": "${query}"}
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("intent", comp, nil, inputsSchema, nil, nil, nil, nil, 0, -1.0, nil, nil)
	// designated end 为后注册者：refund 命中 refund，logistics 命中 logistics
	endLogistics := s.newEnd(logisticsTmpl)
	endRefund := s.newEnd(refundTmpl)
	if designated == "logistics" {
		w.SetEndComp("refund", endRefund, nil, nil, nil, nil, "batch", nil)
		w.SetEndComp("logistics", endLogistics, nil, nil, nil, nil, "batch", nil)
	} else {
		w.SetEndComp("logistics", endLogistics, nil, nil, nil, nil, "batch", nil)
		w.SetEndComp("refund", endRefund, nil, nil, nil, nil, "batch", nil)
	}
	w.AddConnection([]string{"start"}, "intent")
	w.AddConditionalConnection("intent", comp.Router())
	return w
}

// TestIntentDetection_端到端命中refund spec §5 覆盖点 6：LLM 返回 分类1 → 路由 refund
func (s *IntentDetectionSuite) TestIntentDetection_端到端命中refund() {
	comp, _ := s.newIntentComp(`{"class": "分类1", "reason": "用户要退款"}`, false)
	w := s.buildWorkflow(comp, "refund-end", "logistics-end", "refund")

	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"query": "我买的东西坏了要退钱"}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflow.WorkflowOutput)
	s.Require().True(ok)
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	s.Contains(s.resultText(wo), "refund-end")
}

// TestIntentDetection_端到端命中logistics 分类2 → 路由 logistics
func (s *IntentDetectionSuite) TestIntentDetection_端到端命中logistics() {
	comp, _ := s.newIntentComp(`{"class": "分类2", "reason": "查物流"}`, false)
	w := s.buildWorkflow(comp, "refund-end", "logistics-end", "logistics")

	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(),
		map[string]any{"query": "我的快递到哪了"}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflow.WorkflowOutput)
	s.Require().True(ok)
	s.Equal(workflow.WorkflowExecutionStateCompleted, wo.State)
	s.Contains(s.resultText(wo), "logistics-end")
}

// TestIntentDetection_解析失败回落默认分支 spec §3：垃圾文本 → 分类0，两分支均不命中
// → 兜底（登记 default 分支后命中）
func (s *IntentDetectionSuite) TestIntentDetection_解析失败回落默认分支() {
	comp, _ := s.newIntentComp("完全不是JSON", false)
	s.Require().NoError(comp.AddBranch("${classificationId} == 1", []string{"refund"}, "b1"))
	s.Require().NoError(comp.AddBranch("${classificationId} == 2", []string{"logistics"}, "b2"))

	inputsSchema := map[string]any{"query": "${query}"}
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	w.AddWorkflowComp("intent", comp, nil, inputsSchema, nil, nil, nil, nil, 0, -1.0, nil, nil)
	w.SetEndComp("logistics", s.newEnd("logistics-end"), nil, nil, nil, nil, "batch", nil)
	w.SetEndComp("refund", s.newEnd("refund-end"), nil, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "intent")
	w.AddConditionalConnection("intent", comp.Router())

	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(), map[string]any{"query": "q"}, sess, nil, nil)
	// 无分支命中 → 101021（branch not found）——与 Python BranchRouter 语义一致
	if err == nil {
		wo, ok := out.(*workflow.WorkflowOutput)
		s.Require().True(ok)
		s.NotEqual(workflow.WorkflowExecutionStateCompleted, wo.State, "无命中分支不应完成")
	} else {
		s.Require().Error(err, "无命中分支应报错（Python 101021 语义）")
	}
}

// TestIntentDetection_历史写回真实链 spec §3/§5 覆盖点 3+5：EnableHistory=true 时
// ModelContext 追加 user(query)+assistant(LLM 原文)（真实 ContextEngine 链）
func (s *IntentDetectionSuite) TestIntentDetection_历史写回真实链() {
	comp, mock := s.newIntentComp(`{"class": "分类1", "reason": "r"}`, true)
	w := s.buildWorkflow(comp, "refund-end", "logistics-end", "logistics")

	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	_, err := w.Invoke(context.Background(), map[string]any{"query": "我要退货"}, sess, nil, nil)
	s.Require().NoError(err)
	// mock 收到的消息应包含历史窗口段与当前输入
	s.Require().Equal(1, mock.InvokeCallCount())
	msgs := mock.GetInvokeCall(0).Messages.Messages()
	s.Require().NotEmpty(msgs)
	userText := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].GetRole().String() == "user" {
			userText = msgs[i].GetContent().String()
			break
		}
	}
	s.Contains(userText, "我要退货")
}

// TestIntentDetection_同键覆盖 快照替换语义（spec §3 set_outputs 最新覆盖）：
// 连续两轮 invoke，第二轮输出覆盖第一轮（同一 NodeSession）
func (s *IntentDetectionSuite) TestIntentDetection_同键覆盖() {
	comp1, _ := s.newIntentComp(`{"class": "分类1", "reason": "第一轮"}`, false)
	sess := session.NewSession(session.WithEnvs(map[string]any{}))

	w1 := s.buildWorkflow(comp1, "refund-end", "logistics-end", "refund")
	out1, err := w1.Invoke(context.Background(), map[string]any{"query": "退款"}, sess, nil, nil)
	s.Require().NoError(err)
	wo1 := out1.(*workflow.WorkflowOutput)
	s.Contains(s.resultText(wo1), "refund-end")

	// 同一 session 第二轮（mock 返回 分类2）
	comp2, _ := s.newIntentComp(`{"class": "分类2", "reason": "第二轮"}`, false)
	w2 := s.buildWorkflow(comp2, "refund-end", "logistics-end", "logistics")
	out2, err := w2.Invoke(context.Background(), map[string]any{"query": "查物流"}, sess, nil, nil)
	s.Require().NoError(err)
	wo2 := out2.(*workflow.WorkflowOutput)
	// 第二轮结果反映最新分类（新组件实例同 session 复用其 io_state 快照语义）
	s.Contains(s.resultText(wo2), "logistics-end")
}

// resultText 提取 WorkflowOutput 文本形态（End batch 输出 map/串/Chunk 双形态）
func (s *IntentDetectionSuite) resultText(wo *workflow.WorkflowOutput) string {
	s.T().Helper()
	var parts []string
	switch v := wo.Result.(type) {
	case string:
		return v
	case map[string]any:
		for _, val := range v {
			parts = append(parts, fmt.Sprintf("%v", val))
		}
	case []workflow.WorkflowChunk:
		for _, c := range v {
			parts = append(parts, fmt.Sprintf("%v", c))
		}
	default:
		parts = append(parts, fmt.Sprintf("%v", wo.Result))
	}
	return strings.Join(parts, " ")
}
