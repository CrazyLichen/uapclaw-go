//go:build integration

package native

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	interrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	answpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessionstream "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// TestNativeAdapterDriveSuite HarnessIOAdapter 包真 DeepAgentHarness 的驱动用例。
//
// 对应 Python: test_native_interrupt_kinds.py 的 _adapter_turn 段
// （test_io_adapter_answers_by_tool_call_id_still_resolve +
// test_io_adapter_remember_keeps_its_scope_when_tools_are_asked）。
type TestNativeAdapterDriveSuite struct {
	NativeHarnessSuite
}

func TestNativeAdapterDriveSuiteEntry(t *testing.T) {
	suite.Run(t, new(TestNativeAdapterDriveSuite))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAdapter按toolCallID应答仍解析 按 call_id 键控的应答仍能解析挂起中断。
// Python: test_io_adapter_answers_by_tool_call_id_still_resolve
func (s *TestNativeAdapterDriveSuite) TestAdapter按toolCallID应答仍解析() {
	asked, finalOutput := s.adapterDrive(
		func(chunk sessionstream.OutputSchema) *interaction.InteractiveInput {
			return &interaction.InteractiveInput{
				UserInputs: map[string]any{"call_q": map[string]any{
					"answers": map[string]any{"Which color?": "blue"},
				}},
			}
		},
		func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
			params.Rails = []agentRail{interrail.NewAskUserRail()}
			return params
		},
		harness_providers.HarnessIOAdapterOptions{EmitTurnLifecycle: true},
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "ask_user", ArgsJSON: askArgsJSON, CallID: "call_q"},
		}),
	)
	s.Require().Len(asked, 1, "应恰好一次交互")
	s.Contains(s.llmToolResultsJoined(), `"Which color?"="blue"`)
	s.Equal("done", finalOutput)
}

// TestAdapter记住应答保持其范围 参数化三种应答形状，spy 记录持久化范围。
// Python: test_io_adapter_remember_keeps_its_scope_when_tools_are_asked（3 例）
func (s *TestNativeAdapterDriveSuite) TestAdapter记住应答保持其范围() {
	cases := []struct {
		name      string
		value     map[string]any
		persisted string
	}{
		{"永久记住", map[string]any{"approved": true, "auto_confirm": true}, "permanent"},
		{"永久记住显式persist", map[string]any{"approved": true, "auto_confirm": true, "persist_allow": true}, "permanent"},
		{"会话记住", nil, "session"}, // interrupt_answer(remember=True) 产物，用例内构造
	}
	for _, testCase := range cases {
		s.Run(testCase.name, func() {
			value := testCase.value
			if value == nil {
				// Python: interrupt_answer(InterruptKind.PERMISSION, approved=True, remember=True)
				answer, err := answpkg.InterruptAnswerFor("permission",
					answpkg.InterruptAnswerOptions{Approved: interruptpkgBoolPtr(true), Remember: true})
				s.Require().NoError(err)
				value = answer.(map[string]any)
			}

			scopes := make(chan string, 4)
			// 工具名带子用例唯一后缀：ResourceMgr 按名注册，跨子用例重名会
			// 命中首次注册的实例（invoke 计数落在旧对象上）。
			toolName := fmt.Sprintf("doc_tool_%s", testCase.name)
			permissionRail := newPermissionRail(askPermissionsConfig(toolName))
			permissionRail.SetOverride(&securityrail.PermissionInterruptRailOverride{
				// Python: monkeypatch _persist_allow_always → spy("permanent")
				PersistAllowAlways: func(_ context.Context, _ string, _ map[string]any) (bool, bool) {
					scopes <- "permanent"
					return true, true
				},
				// Python: monkeypatch _persist_session_allow → spy("session")
				PersistSessionAllow: func(_ context.Context, _ string, _ map[string]any, _ string) (bool, bool) {
					scopes <- "session"
					return true, true
				},
			})
			toolInstance := newCountingTool(toolName)

			asked, finalOutput := s.adapterDrive(
				func(chunk sessionstream.OutputSchema) *interaction.InteractiveInput {
					output, ok := chunk.Payload.(interaction.InteractionOutput)
					s.Require().True(ok, "交互块载荷应为 InteractionOutput，实际 %T", chunk.Payload)
					return &interaction.InteractiveInput{
						InterruptAnswers: map[string]any{output.ID: value},
					}
				},
				func(params hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams {
					params.ToolInstances = []tool.Tool{toolInstance}
					params.Rails = []agentRail{permissionRail}
					return params
				},
				harness_providers.HarnessIOAdapterOptions{EmitTurnLifecycle: true, AutoApproveTools: true},
				mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
					{Name: toolName, ArgsJSON: "{}", CallID: "call_p"},
				}),
			)
			s.Require().NotEmpty(asked, "应浮现审批交互")
			s.Equal(int32(1), toolInstance.InvokeCount(), "批准后工具应执行")
			s.Equal("done", finalOutput)

			select {
			case scope := <-scopes:
				s.Equal(testCase.persisted, scope, "持久化范围应匹配")
			case <-time.After(2 * time.Second):
				s.Fail("持久化 spy 未被调用")
			}
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// interruptpkgBoolPtr 布尔转指针（answpkg.InterruptAnswerOptions 需要指针）。
func interruptpkgBoolPtr(value bool) *bool { return &value }

// adapterDrive 经 HarnessIOAdapter 驱动真 harness：发送输入、消费输出、
// 遇交互块用 answer() 应答，直到轮次终态。返回交互块列表与终态输出。
// Python: test_native_interrupt_kinds.py:416-444 _adapter_turn。
func (s *TestNativeAdapterDriveSuite) adapterDrive(
	answer func(chunk sessionstream.OutputSchema) *interaction.InteractiveInput,
	customize func(hconfig.CreateDeepAgentParams) hconfig.CreateDeepAgentParams,
	adapterOpts harness_providers.HarnessIOAdapterOptions,
	firstResponse mockllm.MockResponse,
) ([]sessionstream.OutputSchema, string) {
	s.MockLLM.SetResponses(
		firstResponse,
		mockllm.CreateTextResponse("done"),
	)
	h := newHarness(func(ctx context.Context) (*harness.DeepAgent, error) {
		return s.newAgentWithParams(customize(hconfig.CreateDeepAgentParams{}))
	})
	adapter := harness_providers.NewHarnessIOAdapter(h, adapterOpts)

	harnessCtx, err := hp.NewHarnessContext("native", "native_agent",
		fmt.Sprintf("io-%d", suiteUniqueID.Add(1)), "", hp.ContextOptions{})
	s.Require().NoError(err)

	ctx, cancel := context.WithTimeout(s.Ctx, 30*time.Second)
	defer cancel()
	s.Require().NoError(adapter.Start(ctx, harnessCtx), "adapter start 失败")
	defer func() { _ = adapter.Stop(context.Background()) }()

	input, err := hp.NewHarnessInput("go", hp.JsonObject{})
	s.Require().NoError(err)
	_, err = adapter.Send(ctx, input, false)
	s.Require().NoError(err, "adapter send 失败")

	asked := []sessionstream.OutputSchema{}
	finalOutput := ""
	iterator := adapter.Outputs()
	for {
		chunk, hasMore := iterator.Next(ctx)
		if !hasMore {
			break
		}
		schema, ok := chunk.(sessionstream.OutputSchema)
		if !ok {
			continue
		}
		switch schema.Type {
		case interaction.InteractionType:
			asked = append(asked, schema)
			s.Require().LessOrEqual(len(asked), 2, "the agent keeps asking")
			_, sendErr := adapter.Send(ctx, answer(schema), false)
			s.Require().NoError(sendErr, "应答应命中挂起交互")
		case harness_providers.TurnLifecycleChunkType:
			// Python: TURN_LIFECYCLE 且 kind != "started" → return
			if payload, ok := schema.Payload.(map[string]any); ok {
				if kind, _ := payload["kind"].(string); kind != "started" {
					if text, hasText := payload["final_output"].(string); hasText {
						finalOutput = text
					}
					return asked, finalOutput
				}
			}
		}
	}
	return asked, finalOutput
}
