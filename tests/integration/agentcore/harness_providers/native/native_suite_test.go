//go:build integration

package native

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	nativeprov "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers/native"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// NativeHarnessSuite 15.6 native provider 端到端集成测试。
//
// 驱动一个真 DeepAgent（真 rails / 中断处理器 / 恢复路径）+ 脚本化模型，
// 经 DeepAgentHarness.Start / Send / TurnEvents 走完整 turn → 中断 → 宿主应答 → resume 链路。
//
// 对应 Python: tests/unit_tests/harness_providers/test_native_interrupt_kinds.py
// + tests/unit_tests/fixtures/deep_agent_interrupts.py
type NativeHarnessSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNativeHarnessSuite 套件入口（审计硬要求：每个 _test.go 自带 suite.Run）。
func TestNativeHarnessSuite(t *testing.T) {
	suite.Run(t, new(NativeHarnessSuite))
}

// ──────────────────────────── 测试夹具 ────────────────────────────

// recordingTool 计数包装的服务端工具。
// Python: fixtures/deep_agent_interrupts.py:31-48 CountingTool（invoke_count += 1 观测语义）
type recordingTool struct {
	tool.Tool
	count atomic.Int32
}

func (r *recordingTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	r.count.Add(1)
	return r.Tool.Invoke(ctx, inputs, opts...)
}

// InvokeCount 返回执行次数。
func (r *recordingTool) InvokeCount() int32 { return r.count.Load() }

// newCountingTool 创建 name 计数工具。
// Python: CountingTool(name) 的 {"ran": name, "invoke_count": n} 返回
func newCountingTool(name string) *recordingTool {
	tc := tool.NewToolCardWithID(name, name, name+" test tool", nil, nil)
	inner, _ := tool.NewMapFunction(tc,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{"ran": name}, nil
		}, nil,
	)
	return &recordingTool{Tool: inner}
}

// newClipboardClientTool 创建客户端执行工具（read_clipboard）。
// Python: test_native_interrupt_kinds.py:83-89 ClipboardTool(ClientTool)，
// build_request 返回 {"action": "read_clipboard"}
func newClipboardClientTool() *tool.ClientTool {
	card := tool.NewToolCardWithID("read_clipboard", "read_clipboard", "d", nil, nil)
	card.Execution = tool.ToolExecutionClient
	return tool.NewClientTool(card, func(_ map[string]any, _ *tool.ClientToolContext) (map[string]any, error) {
		return map[string]any{"action": "read_clipboard"}, nil
	}, nil)
}

// askPermissionsConfig 询问 tool_names 权限、其余放行的权限配置。
// Python: fixtures/deep_agent_interrupts.py:104-110 ask_permissions
func askPermissionsConfig(toolNames ...string) map[string]any {
	tools := map[string]any{}
	for _, name := range toolNames {
		tools[name] = "ask"
	}
	return map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"*": "allow"},
		"tools":    tools,
	}
}

// newPermissionRail 创建 ask 模式权限中断护栏。
func newPermissionRail(config map[string]any) *securityrail.PermissionInterruptRail {
	return securityrail.NewPermissionInterruptRail(
		config,
		nil, // engine 从 config 自动创建
		nil, // toolNames
		nil, // llmModel（静态配置不需要 LLM）
		"",
		&harnesssecurity.ToolPermissionHost{
			ResolveWorkspaceDir: func() string { return "/workspace" },
		},
	)
}

// hostAnswers 宿主对每种请求的应答函数。
// Python: test_native_interrupt_kinds.py:59-80 Host.answer
type hostAnswers func(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error)

// recordedHost 记录每个请求并按配置应答的宿主交互处理器。
// Python: test_native_interrupt_kinds.py:59-80 Host（>5 次追问视为应答不满足）
type recordedHost struct {
	answerFn hostAnswers

	mu       sync.Mutex
	requests []hp.HarnessInteractionRequest
}

func newRecordedHost(answerFn hostAnswers) *recordedHost {
	return &recordedHost{answerFn: answerFn}
}

func (h *recordedHost) Handle(_ context.Context, request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
	h.mu.Lock()
	h.requests = append(h.requests, request)
	count := len(h.requests)
	h.mu.Unlock()
	if count > 5 {
		return nil, fmt.Errorf("the agent keeps asking: the answers do not satisfy it")
	}
	return h.answerFn(request)
}

func (h *recordedHost) Cancel(_ context.Context, _ string, _ ...hp.CancelOption) error { return nil }

func (h *recordedHost) all() []hp.HarnessInteractionRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hp.HarnessInteractionRequest{}, h.requests...)
}

// allowApproval 审批请求一律允许；其余请求回 "yes" 文本。
// Python: test_native_interrupt_kinds.py:76-80 allow
func allowApproval(request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
	if req, ok := request.(hp.ToolApprovalRequest); ok {
		return hp.NewToolApprovalResponse(req.RequestID, hp.ToolApprovalDecisionAllow,
			hp.ToolApprovalResponseOptions{})
	}
	req := request.(hp.UserInputRequest)
	return hp.NewUserInputResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
		hp.UserInputResponseOptions{Content: "yes"})
}

// allCapabilities 三个可选宿主能力全开。
// Python: test_native_interrupt_kinds.py:56 ALL_CAPS
func allCapabilities() map[hp.HostCapability]bool {
	return map[hp.HostCapability]bool{
		hp.HostCapabilityUserInput:       true,
		hp.HostCapabilityToolApproval:    true,
		hp.HostCapabilityDynamicToolCall: true,
	}
}

// suiteUniqueID 套件级唯一 id 计数器。
var suiteUniqueID atomic.Int64

// newHarness 装配 DeepAgentHarness（agent 在 start 时构建）。
// Python: DeepAgentHarness(factory)
func newHarness(buildAgent func(ctx context.Context) (*harness.DeepAgent, error)) *nativeprov.DeepAgentHarness {
	return nativeprov.NewDeepAgentHarness(func(ctx context.Context, _ hp.HarnessContext) (*harness.DeepAgent, error) {
		return buildAgent(ctx)
	})
}

// newAgentWithParams 构建真 DeepAgent。
func (s *NativeHarnessSuite) newAgentWithParams(params hconfig.CreateDeepAgentParams) (*harness.DeepAgent, error) {
	params.EnableTaskLoop = false
	if params.MaxIterations == 0 {
		params.MaxIterations = 10
	}
	if params.SystemPrompt == "" {
		params.SystemPrompt = "Follow the scripted model."
	}
	return s.NewDeepAgentForTest(s.Ctx, params)
}

// runTurn 启动 harness、发送输入、收集 turn 事件，返回终态结果。
// Python: test_native_interrupt_kinds.py:101-126 _turn
func (s *NativeHarnessSuite) runTurn(
	h *nativeprov.DeepAgentHarness,
	host *recordedHost,
	capabilities map[hp.HostCapability]bool,
) hp.TurnResult {
	ctx := s.Ctx
	harnessCtx, err := hp.NewHarnessContext("native", "native_agent",
		fmt.Sprintf("native-%d", suiteUniqueID.Add(1)), "", hp.ContextOptions{
			HostCapabilities: capabilities,
			Interactions:     host,
		})
	s.Require().NoError(err)

	s.Require().NoError(h.Start(ctx, harnessCtx), "harness start 失败")
	defer func() { _ = h.Stop(ctx) }()

	input, err := hp.NewHarnessInput("go", hp.JsonObject{})
	s.Require().NoError(err)
	receipt, err := h.Send(ctx, input, hp.DeliveryModeAuto)
	s.Require().NoError(err, "harness send 失败")

	turnID := receipt.TurnID
	cursor := h.TurnEvents(&turnID)
	defer func() { _ = cursor.Close(ctx) }()

	var terminal *hp.TurnLifecycleEvent
	for {
		event, nextErr := cursor.Next(ctx)
		if nextErr != nil {
			if isEventStreamDone(nextErr) {
				break
			}
			s.Require().NoError(nextErr, "event stream error")
		}
		// 事件载荷以值类型存储（构造器返回值而非指针）
		if life, ok := event.Event.(hp.TurnLifecycleEvent); ok {
			s.T().Logf("lifecycle kind=%s hasResult=%v", life.Kind, life.Result != nil)
			if life.Result != nil {
				terminal = &life
			}
		} else {
			if providerEvent, ok := event.Event.(hp.ProviderEvent); ok {
				s.T().Logf("provider event type=%s payload=%v", providerEvent.EventType, providerEvent.Payload)
			} else {
				s.T().Logf("event: %T", event.Event)
			}
		}
	}
	s.Require().NotNil(terminal, "缺终态 TurnLifecycleEvent")
	if terminal.Result.Error != nil {
		s.T().Logf("turn failed: message=%s code=%v provider=%v", terminal.Result.Error.Message,
			terminal.Result.Error.Code, terminal.Result.Error.ProviderData)
	}
	s.T().Logf("stop_reason=%v final_output=%v n_messages=%d",
		terminal.Result.StopReason, terminal.Result.FinalOutput, len(terminal.Result.Messages))
	for _, message := range terminal.Result.Messages {
		if message.Role == hp.MessageRoleTool {
			for _, block := range message.Content {
				s.T().Logf("tool msg block kind=%s content=%v data=%v", block.Kind, block.Content, block.Data)
			}
		}
		if message.Role == hp.MessageRoleAssistant {
			for _, block := range message.Content {
				s.T().Logf("assistant block kind=%s content=%v", block.Kind, block.Content)
			}
		}
	}
	return *terminal.Result
}

// toolResultsSeen 从终态结果提取 [(call_id, content)] 工具消息。
// Python: fixtures/deep_agent_interrupts.py:155-164 tool_results_seen
// （Python 断言模型调用历史里的 tool role 消息；协议层 TurnResult.Messages
// 的 tool 角色消息与之一一对应）
func toolResultsSeen(result hp.TurnResult) [][2]string {
	seen := [][2]string{}
	for _, message := range result.Messages {
		if message.Role != hp.MessageRoleTool {
			continue
		}
		for _, block := range message.Content {
			content := blockContentText(block.Content)
			callID := ""
			if block.Data != nil {
				if id, ok := block.Data["call_id"].(string); ok {
					callID = id
				}
			}
			seen = append(seen, [2]string{callID, content})
		}
	}
	return seen
}

// blockContentText 把 ContentBlock 的冻结 content 转文本。
func blockContentText(content hp.JsonValue) string {
	switch value := content.(type) {
	case string:
		return value
	case map[string]any:
		body, _ := json.Marshal(value)
		return string(body)
	case []any:
		body, _ := json.Marshal(value)
		return string(body)
	default:
		if value == nil {
			return ""
		}
		body, _ := json.Marshal(value)
		return string(body)
	}
}

// toolResultsJoined 工具结果文本拼接（单条断言便捷）。
func toolResultsJoined(result hp.TurnResult) string {
	parts := make([]string, 0)
	for _, item := range toolResultsSeen(result) {
		parts = append(parts, item[1])
	}
	return strings.Join(parts, "\n")
}

// llmToolResultsSeen 从 MockLLM 最后一次模型调用历史提取 [(tool_call_id, content)]。
// Python: fixtures/deep_agent_interrupts.py:155-164 tool_results_seen —— 断言对象是
// 模型收到的 tool role 消息（rendered 文本），不是协议层的结构化 tool_result 块。
func (s *NativeHarnessSuite) llmToolResultsSeen() [][2]string {
	// resume 轮走 _streaming=true → 模型调用进 Stream 记录；最后一轮的 tool
	// 消息在最后一次调用（Invoke 或 Stream 均可能）里
	var messages modelclients.MessagesParam
	if n := s.MockLLM.StreamCallCount(); n > 0 {
		messages = s.MockLLM.GetStreamCall(n - 1).Messages
	} else if n := s.MockLLM.InvokeCallCount(); n > 0 {
		messages = s.MockLLM.GetInvokeCall(n - 1).Messages
	} else {
		return nil
	}
	seen := [][2]string{}
	for _, message := range messages.Messages() {
		toolMsg, ok := message.(*llmschema.ToolMessage)
		if !ok {
			continue
		}
		seen = append(seen, [2]string{toolMsg.ToolCallID, toolMsg.Content.Text()})
	}
	return seen
}

// llmToolResultsJoined 最后一次模型调用的 tool 消息文本拼接。
func (s *NativeHarnessSuite) llmToolResultsJoined() string {
	parts := make([]string, 0)
	for _, item := range s.llmToolResultsSeen() {
		parts = append(parts, item[1])
	}
	return strings.Join(parts, "\n")
}

// isEventStreamDone 判定事件流耗尽。
func isEventStreamDone(err error) bool { return err == hp.ErrEventStreamDone }
