//go:build integration

// 中断恢复全链路集成：server adapter build 层（answers → InteractiveInput）
// → interaction 容器 → 中断 handler 内核（collect / resume / held / payload 重建）。
// 对齐 Python interface.py:2251-2486 + handler.py:76-428。
package interruptresume

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptResumeFullChainSuite 中断恢复全链套件。
type InterruptResumeFullChainSuite struct {
	suite.Suite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInterruptResumeFullChainSuite 套件入口。
func TestInterruptResumeFullChainSuite(t *testing.T) {
	suite.Run(t, new(InterruptResumeFullChainSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildFromAnswers 经 server build 层全路径把 chat.send params 变成 InteractiveInput。
// Python: JiuWenSwarm._build_inputs → _build_interactive_input_from_answers。
func (s *InterruptResumeFullChainSuite) buildFromAnswers(params map[string]any) *interaction.InteractiveInput {
	uc := runtime.NewUapClaw()
	paramsJSON, err := json.Marshal(params)
	s.Require().NoError(err)
	req := schema.NewAgentRequest("itest-req", "web", schema.ReqMethodChatSend, paramsJSON)
	inputs, _, _ := uc.BuildInputs(req)
	ii, ok := inputs["query"].(*interaction.InteractiveInput)
	s.Require().True(ok, "answers 分支应产出 InteractiveInput")
	return ii
}

// fakeAgent 中断 handler 所需的最小 Agent 接口桩。
type fakeAgent struct{}

func (f *fakeAgent) ContextEngine() ceinterface.ContextEngine { return nil }

// ──────────────────────────── 测试用例 ────────────────────────────

// TestFullChain_permission答案经build层路由到中断 build 层按 interrupt_id 回答 →
// handler 的 match_answers 按 interrupt_id 命中该 pending（跨 build/interaction/handler 三层）。
func (s *InterruptResumeFullChainSuite) TestFullChain_permission答案经build层路由到中断() {
	ii := s.buildFromAnswers(map[string]any{
		"source":       "permission_interrupt",
		"request_id":   "tc1",
		"interrupt_id": "intr_perm_1",
		"answers":      []any{map[string]any{"selected_options": []any{"always_allow"}}},
	})
	value, ok := ii.InterruptAnswers["intr_perm_1"]
	s.Require().True(ok, "build 层应按 interrupt_id 回答")
	payload := value.(map[string]any)
	s.Equal(true, payload["approved"])
	s.Equal(true, payload["auto_confirm"])
	s.Equal(true, payload["persist_allow"])

	// 挂起状态：permission rail 在 tc1 上已发号 intr_perm_1
	permReq := &saschema.InterruptRequest{Message: "允许吗", InterruptOwner: "perm.rail", InterruptID: "intr_perm_1"}
	state := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 1},
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{
			"tc1": {
				ToolCall:          &llmschema.ToolCall{ID: "tc1", Name: "bash"},
				PausedCall:        &llmschema.ToolCall{ID: "tc1", Name: "bash"},
				InterruptRequests: map[string]saschema.InterruptRequester{"tc1": permReq},
			},
		},
	}

	// handler 视角：pending 表 + match_answers 应命中该中断
	matched := interrupt.MatchAnswers(ii, interrupt.PendingInterrupts(state))
	s.Require().Len(matched.Unmatched, 0)
	v, ok := matched.Answers["intr_perm_1"]
	s.Require().True(ok, "build 层答案应归属到 pending 的 interrupt_id")
	s.Equal(true, v.(map[string]any)["approved"])
}

// TestFullChain_clientTool恢复保持暂停 client tool 中断：InteractiveInput 只答
// permission 调用时，client tool 调用保持暂停、payload 用 paused_call 重建、
// 中断 id 沿用（对齐 handler.py held 语义 + BuildInputs client_tool 分支）。
func (s *InterruptResumeFullChainSuite) TestFullChain_clientTool恢复保持暂停() {
	// build 层：client_tool_interrupt 恢复携带一条结果 → 按 interrupt_id 回答
	ii := s.buildFromAnswers(map[string]any{
		"source": "client_tool_interrupt",
		"answers": []any{map[string]any{
			"request_id":   "tcA",
			"interrupt_id": "intr_a",
			"result":       map[string]any{"status": "completed", "content": "done"},
		}},
	})
	answerValue, ok := ii.InterruptAnswers["intr_a"]
	s.Require().True(ok)
	// build 层就地 coerce（对齐 Python client_tool_answers），类型为 *tool.ClientToolResult
	coerced, isResult := answerValue.(*tool.ClientToolResult)
	s.Require().True(isResult)
	s.Equal(tool.ClientToolStatusCompleted, coerced.Status)

	// 内核：client tool 调用（无人回答保持暂停）+ permission 调用（已回答重放）
	clientReq := saschema.NewClientToolRequest("client_tool", map[string]any{}, map[string]any{})
	clientReq.InterruptID = "intr_client"
	clientEntry := &saschema.ToolInterruptEntry{
		ToolCall:          &llmschema.ToolCall{ID: "tcA", Name: "client_tool"},
		PausedCall:        &llmschema.ToolCall{ID: "tcA", Name: "client_tool", Arguments: `{"final":1}`},
		InterruptRequests: map[string]saschema.InterruptRequester{"tcA": clientReq},
	}
	permReq := &saschema.InterruptRequest{Message: "允许吗", InterruptOwner: "perm.rail"}
	permReq.InterruptID = "intr_perm"
	permEntry := &saschema.ToolInterruptEntry{
		ToolCall:          &llmschema.ToolCall{ID: "tcB", Name: "bash"},
		PausedCall:        &llmschema.ToolCall{ID: "tcB", Name: "bash"},
		InterruptRequests: map[string]saschema.InterruptRequester{"tcB": permReq},
	}
	state := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 1},
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{
			"tcA": clientEntry,
			"tcB": permEntry,
		},
	}

	agent := &fakeAgent{}
	handler := interrupt.NewToolInterruptHandler(agent)
	sess := session.NewSession()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)

	var executedIDs []string
	executeFn := func(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]saschema.ExecuteResult, error) {
		for _, tc := range toolCalls {
			executedIDs = append(executedIDs, tc.ID)
		}
		return []saschema.ExecuteResult{{Result: map[string]any{"result": "ok"}, ToolMsg: nil}}, nil
	}

	// InteractiveInput 同时回答 permission 中断；client tool 的答案下一轮才给
	resumeInput, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(resumeInput.Answer("intr_perm", map[string]any{"approved": true}))

	result, err := handler.HandleResume(context.Background(), &interrupt.ResumeContext{
		State: state, UserInput: resumeInput, Ctx: cbc, Session: sess,
		InvokeInputs: &agentinterfaces.InvokeInputs{}, ExecuteToolCall: executeFn,
	})
	s.Require().NoError(err)
	// permission 调用重放；client tool 调用不重放
	s.Equal([]string{"tcB"}, executedIDs)
	// 仍有中断（client tool held）→ commit
	s.Require().NotNil(result)
	s.Equal("interrupt", result["result_type"])
	interruptIDs, _ := result["interrupt_ids"].([]any)
	s.Require().Len(interruptIDs, 1)
	s.Equal("tcA", interruptIDs[0])

	// held payload 用 paused_call 重建，interrupt_id 沿用
	stateOutputs, _ := result["state"].([]any)
	s.Require().Len(stateOutputs, 1)
	outputSchema, ok := stateOutputs[0].(*stream.OutputSchema)
	s.Require().True(ok)
	output, ok := outputSchema.Payload.(*interaction.InteractionOutput)
	s.Require().True(ok)
	rebuilt, ok := output.Value.(*saschema.ToolCallInterruptRequest)
	s.Require().True(ok)
	s.Equal(`{"final":1}`, rebuilt.ToolArgs)
	s.Equal("client_tool", rebuilt.ToolName)
	s.Equal("intr_client", rebuilt.Request.GetInterruptID())
}

// TestFullChain_askUser恢复路径 ask_user 答案经 build 层构造 → handler 重放 →
// 无新中断继续循环。
func (s *InterruptResumeFullChainSuite) TestFullChain_askUser恢复路径() {
	// build 层：多选 + Other 剔除 + custom_input 追加
	ii := s.buildFromAnswers(map[string]any{
		"source":     "ask_user_interrupt",
		"request_id": "tc1",
		"answers": []any{map[string]any{
			"question":         "选择方案",
			"selected_options": []any{"A", "Other"},
			"custom_input":     "自定义方案",
		}},
	})
	answers := ii.UserInputs["tc1"].(map[string]any)["answers"].(map[string]any)
	s.Equal([]string{"A", "自定义方案"}, answers["选择方案"])

	// 内核：ask_user 中断被回答 → 重放成功 → 无新中断
	askReq := &saschema.InterruptRequest{Message: "选择方案", InterruptOwner: "ask.rail"}
	askReq.InterruptID = "intr_ask"
	state := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{Iteration: 2},
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{
			"tc1": {
				ToolCall:          &llmschema.ToolCall{ID: "tc1", Name: "ask_user"},
				PausedCall:        &llmschema.ToolCall{ID: "tc1", Name: "ask_user"},
				InterruptRequests: map[string]saschema.InterruptRequester{"tc1": askReq},
			},
		},
	}

	agent := &fakeAgent{}
	handler := interrupt.NewToolInterruptHandler(agent)
	sess := session.NewSession()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)

	result, err := handler.HandleResume(context.Background(), &interrupt.ResumeContext{
		State: state, UserInput: ii, Ctx: cbc, Session: sess,
		InvokeInputs: &agentinterfaces.InvokeInputs{},
		ExecuteToolCall: func(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess sessioninterfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]saschema.ExecuteResult, error) {
			return []saschema.ExecuteResult{{Result: map[string]any{"result": "ok"}, ToolMsg: nil}}, nil
		},
	})
	s.Require().NoError(err)
	s.Nil(result, "无新中断应返回 nil 继续循环")
	v, _ := cbc.GetExtra(interrupt.ResumeStartIterationKey)
	s.Equal(3, v)
}

// TestFullChain_evolution审批答案形状 evolution 分支构造 {"action": ...} payload
// （Python: EVOLUTION_INTERRUPT_METADATA_SOURCES 分支），供技能演化 rail 消费。
func (s *InterruptResumeFullChainSuite) TestFullChain_evolution审批答案形状() {
	ii := s.buildFromAnswers(map[string]any{
		"source":     "evolution_interrupt",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"总是允许"}, "custom_input": "改提示词"}},
	})
	payload, ok := ii.UserInputs["tc1"].(map[string]any)
	s.Require().True(ok)
	s.Equal("allow_always", payload["action"])
	s.Equal("改提示词", payload["feedback"])
}
